// Package notify runs the owner's notification command on delivery and
// archive events. Events are queued in memory and passed to the command one
// at a time; a failed or lost notification is logged and not retried.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Event kinds. delivery_error, diverged and archive_push are also kinds of
// the project event log (store.Event).
const (
	KindDeliveryError = "delivery_error"
	KindRecovered     = "recovered"
	KindDelivered     = "delivered"
	KindDiverged      = "diverged"
	KindArchivePush   = "archive_push"
)

const (
	// QueueSize is the number of events waiting for the command; newer
	// events are dropped when the queue is full.
	QueueSize = 1000
	// Timeout limits one run of the command.
	Timeout = 30 * time.Second
	// StopWait is how long Stop lets queued events finish.
	StopWait = 5 * time.Second

	// maxEnvValue keeps every event variable below the Linux limit of
	// one environment string (128 KiB); JSON has the full values.
	maxEnvValue = 64 << 10
	maxStderr   = 4 << 10
)

// Event is passed to the notification command.
type Event struct {
	Kind       string
	Time       time.Time
	Project    string
	Target     string // empty for archive_push
	Ref        string // diverged and archive_push
	ArchiveRef string // diverged and archive_push
	Refs       []string
	Message    string
}

// payload is the JSON object on the command's stdin.
type payload struct {
	Event      string   `json:"event"`
	Time       string   `json:"time"`
	Project    string   `json:"project"`
	Target     string   `json:"target"`
	Ref        string   `json:"ref"`
	ArchiveRef string   `json:"archive_ref"`
	Refs       []string `json:"refs"`
	Message    string   `json:"message"`
}

func (e Event) json() ([]byte, error) {
	refs := e.Refs
	if refs == nil {
		refs = []string{}
	}
	return json.Marshal(payload{
		Event:      e.Kind,
		Time:       e.Time.UTC().Format(time.RFC3339),
		Project:    e.Project,
		Target:     e.Target,
		Ref:        e.Ref,
		ArchiveRef: e.ArchiveRef,
		Refs:       refs,
		Message:    e.Message,
	})
}

// commandEnv returns base without METAGIT_SECRET_KEY plus the event
// variables.
func commandEnv(base []string, e Event) []string {
	env := make([]string, 0, len(base)+8)
	for _, kv := range base {
		if !strings.HasPrefix(kv, "METAGIT_SECRET_KEY=") {
			env = append(env, kv)
		}
	}
	return append(env,
		"METAGIT_EVENT="+envValue(e.Kind),
		"METAGIT_TIME="+e.Time.UTC().Format(time.RFC3339),
		"METAGIT_PROJECT="+envValue(e.Project),
		"METAGIT_TARGET="+envValue(e.Target),
		"METAGIT_REF="+envValue(e.Ref),
		"METAGIT_ARCHIVE_REF="+envValue(e.ArchiveRef),
		"METAGIT_REFS="+joinRefs(e.Refs),
		"METAGIT_MESSAGE="+envValue(e.Message),
	)
}

// envValue cuts s to maxEnvValue bytes without splitting a UTF-8 sequence.
// NUL bytes cannot be passed in the environment and are removed.
func envValue(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if len(s) <= maxEnvValue {
		return s
	}
	cut := maxEnvValue
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// joinRefs joins refs with spaces, keeping only whole refs that fit in
// maxEnvValue bytes.
func joinRefs(refs []string) string {
	var b strings.Builder
	for _, ref := range refs {
		n := len(ref)
		if b.Len() > 0 {
			n++
		}
		if b.Len()+n > maxEnvValue {
			break
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(ref)
	}
	return b.String()
}

// Check makes sure command is an executable file and returns its absolute
// path.
func Check(command string) (string, error) {
	path, err := filepath.Abs(command)
	if err != nil {
		return "", fmt.Errorf("--notify-command: %w", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("--notify-command: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("--notify-command: %s is not an executable file", path)
	}
	return path, nil
}

// Notifier passes events to the notification command. A nil *Notifier is
// valid and does nothing, so callers need no checks when notifications are
// off.
type Notifier struct {
	command string
	log     *slog.Logger
	timeout time.Duration

	mu      sync.Mutex
	started bool
	closed  bool
	queue   chan Event

	ctx    context.Context // canceled when Stop stops waiting
	cancel context.CancelFunc
	done   chan struct{}
	unsent int // written by the loop, read by Stop after done
}

// New creates a Notifier for command. Call Start to run it.
func New(command string, log *slog.Logger) *Notifier {
	return newNotifier(command, log, QueueSize, Timeout)
}

func newNotifier(command string, log *slog.Logger, queueSize int, timeout time.Duration) *Notifier {
	ctx, cancel := context.WithCancel(context.Background())
	return &Notifier{
		command: command,
		log:     log,
		timeout: timeout,
		queue:   make(chan Event, queueSize),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
}

// Start runs the loop that passes queued events to the command.
func (n *Notifier) Start() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.started || n.closed {
		return
	}
	n.started = true
	go n.loop()
}

// Notify queues an event without waiting for the command. The event is
// dropped with a warning when the queue is full or the Notifier is stopped.
func (n *Notifier) Notify(e Event) {
	if n == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	reason := "service is stopping"
	if !n.closed {
		select {
		case n.queue <- e:
			return
		default:
			reason = "queue is full"
		}
	}
	n.log.Warn("notification dropped", "event", e.Kind, "project", e.Project, "target", e.Target, "reason", reason)
}

// Stop stops accepting events and lets queued ones finish for up to wait.
// Then it interrupts the running command and logs how many events were not
// sent.
func (n *Notifier) Stop(wait time.Duration) {
	if n == nil {
		return
	}
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}
	n.closed = true
	close(n.queue)
	started := n.started
	n.mu.Unlock()
	if !started {
		n.cancel()
		return
	}

	select {
	case <-n.done:
	case <-time.After(wait):
		n.cancel()
		<-n.done
	}
	n.cancel()
	if n.unsent > 0 {
		n.log.Warn("notifications not sent: service is stopping", "count", n.unsent)
	}
}

func (n *Notifier) loop() {
	defer close(n.done)
	for e := range n.queue {
		if n.ctx.Err() != nil {
			n.unsent++
			continue
		}
		n.run(e)
	}
}

func (n *Notifier) run(e Event) {
	attrs := []any{"event", e.Kind, "project", e.Project, "target", e.Target}
	body, err := e.json()
	if err != nil {
		n.log.Warn("notification command failed", append(attrs, "err", err)...)
		return
	}
	ctx, cancel := context.WithTimeout(n.ctx, n.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, n.command)
	cmd.Stdin = bytes.NewReader(body)
	cmd.Env = commandEnv(os.Environ(), e)
	stderr := &limitedBuffer{max: maxStderr}
	cmd.Stderr = stderr
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	switch {
	case err == nil:
		n.log.Debug("notification sent", attrs...)
	case n.ctx.Err() != nil:
		n.unsent++
	default:
		if ctx.Err() != nil {
			err = fmt.Errorf("killed after %s", n.timeout)
		}
		n.log.Warn("notification command failed", append(attrs, "err", err, "stderr", firstLine(stderr.String()))...)
	}
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// limitedBuffer keeps the first max bytes written to it.
type limitedBuffer struct {
	max int
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }
