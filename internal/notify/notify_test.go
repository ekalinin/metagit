package notify

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

var testTime = time.Date(2026, 10, 21, 14, 38, 1, 0, time.FixedZone("MSK", 3*60*60))

func TestEventJSON(t *testing.T) {
	tests := map[string]struct {
		event Event
		want  string
	}{
		"delivered": {
			Event{Kind: KindDelivered, Time: testTime, Project: "app", Target: "github",
				Refs: []string{"refs/heads/dev", "refs/heads/main"}, Message: "delivered 2 refs to github"},
			`{"event":"delivered","time":"2026-10-21T11:38:01Z","project":"app","target":"github","ref":"","archive_ref":"",` +
				`"refs":["refs/heads/dev","refs/heads/main"],"message":"delivered 2 refs to github"}`,
		},
		"archive push without target and refs": {
			Event{Kind: KindArchivePush, Time: testTime, Project: "app", Ref: "refs/heads/main",
				ArchiveRef: "refs/metagit/archive/push/x", Message: "saved"},
			`{"event":"archive_push","time":"2026-10-21T11:38:01Z","project":"app","target":"","ref":"refs/heads/main",` +
				`"archive_ref":"refs/metagit/archive/push/x","refs":[],"message":"saved"}`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tt.event.json()
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("json:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestCommandEnv(t *testing.T) {
	base := []string{"PATH=/bin", "METAGIT_SECRET_KEY=secret", "TELEGRAM_TOKEN=tg", "METAGIT_DATA_DIR=/data"}
	e := Event{Kind: KindDiverged, Time: testTime, Project: "app", Target: "github",
		Ref: "refs/heads/main", ArchiveRef: "refs/metagit/archive/targets/1/x", Message: "line 1\nline 2"}
	got := commandEnv(base, e)
	want := []string{
		"PATH=/bin", "TELEGRAM_TOKEN=tg", "METAGIT_DATA_DIR=/data",
		"METAGIT_EVENT=diverged",
		"METAGIT_TIME=2026-10-21T11:38:01Z",
		"METAGIT_PROJECT=app",
		"METAGIT_TARGET=github",
		"METAGIT_REF=refs/heads/main",
		"METAGIT_ARCHIVE_REF=refs/metagit/archive/targets/1/x",
		"METAGIT_REFS=",
		"METAGIT_MESSAGE=line 1\nline 2",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("env:\n%q\nwant:\n%q", got, want)
	}
}

func TestLongValuesAreCut(t *testing.T) {
	// "ы" is 2 bytes, so the cut at an odd byte count falls inside a rune.
	msg := "x" + strings.Repeat("ы", maxEnvValue)
	var refs []string
	for i := range 5000 {
		refs = append(refs, "refs/tags/v"+strings.Repeat("0", 20)+string(rune('a'+i%26)))
	}
	e := Event{Kind: KindDelivered, Time: testTime, Refs: refs, Message: msg}
	env := envMap(commandEnv(nil, e))

	gotMsg := env["METAGIT_MESSAGE"]
	if len(gotMsg) > maxEnvValue || len(gotMsg) < maxEnvValue-1 || !strings.HasPrefix(msg, gotMsg) || !utf8.ValidString(gotMsg) {
		t.Fatalf("message: %d bytes, valid %v", len(gotMsg), utf8.ValidString(gotMsg))
	}
	gotRefs := strings.Fields(env["METAGIT_REFS"])
	if len(env["METAGIT_REFS"]) > maxEnvValue || len(gotRefs) == 0 || len(gotRefs) == len(refs) || !slices.Equal(gotRefs, refs[:len(gotRefs)]) {
		t.Fatalf("refs: %d bytes, %d of %d refs", len(env["METAGIT_REFS"]), len(gotRefs), len(refs))
	}

	body, err := e.json()
	if err != nil {
		t.Fatal(err)
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	if p.Message != msg || len(p.Refs) != len(refs) {
		t.Fatalf("json is cut: message %d bytes, %d refs", len(p.Message), len(p.Refs))
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	exe := writeScript(t, dir, "notify.sh", "exit 0")
	plain := filepath.Join(dir, "plain.sh")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.sh")

	got, err := Check(exe)
	if err != nil || got != exe {
		t.Fatalf("Check(%s) = %q, %v", exe, got, err)
	}
	for _, path := range []string{missing, dir, plain} {
		_, err := Check(path)
		if err == nil || !strings.Contains(err.Error(), "--notify-command") || !strings.Contains(err.Error(), path) {
			t.Errorf("Check(%s): %v", path, err)
		}
	}

	t.Chdir(dir)
	got, err = Check("notify.sh")
	if err != nil || got != exe {
		t.Fatalf("relative path: %q, %v", got, err)
	}
}

func TestNotifierPassesEventsInOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OUT", dir)
	t.Setenv("METAGIT_SECRET_KEY", "secret")
	t.Setenv("TELEGRAM_TOKEN", "tg")
	cmd := writeScript(t, dir, "notify.sh", `
n=$(ls "$OUT" | grep -c '\.json$' || true)
cat > "$OUT/$n.json"
env > "$OUT/$n.env"
echo "$METAGIT_EVENT $METAGIT_TARGET" >> "$OUT/order"`)
	n, logs := newTestNotifier(t, cmd, QueueSize, Timeout)
	n.Start()
	n.Notify(Event{Kind: KindDeliveryError, Project: "app", Target: "github", Message: "delivery error: denied"})
	n.Notify(Event{Kind: KindRecovered, Project: "app", Target: "github"})
	n.Notify(Event{Kind: KindDelivered, Project: "app", Target: "github", Refs: []string{"refs/heads/main"}})
	n.Stop(10 * time.Second)

	if got := readFile(t, filepath.Join(dir, "order")); got != "delivery_error github\nrecovered github\ndelivered github\n" {
		t.Fatalf("order:\n%s\nlog:\n%s", got, logs)
	}
	var p payload
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "2.json"))), &p); err != nil {
		t.Fatal(err)
	}
	if p.Event != KindDelivered || p.Project != "app" || !slices.Equal(p.Refs, []string{"refs/heads/main"}) || p.Time == "" {
		t.Fatalf("payload: %+v", p)
	}
	env := envMap(strings.Split(readFile(t, filepath.Join(dir, "0.env")), "\n"))
	if env["METAGIT_EVENT"] != KindDeliveryError || env["METAGIT_MESSAGE"] != "delivery error: denied" || env["TELEGRAM_TOKEN"] != "tg" {
		t.Fatalf("env: %v", env)
	}
	if _, ok := env["METAGIT_SECRET_KEY"]; ok {
		t.Fatal("the command sees METAGIT_SECRET_KEY")
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("warnings:\n%s", logs)
	}
}

func TestNotifierCommandFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OUT", dir)
	cmd := writeScript(t, dir, "notify.sh", `
echo "$METAGIT_PROJECT" >> "$OUT/runs"
if [ "$METAGIT_PROJECT" = bad ]; then
	echo >&2
	echo "webhook is not reachable" >&2
	echo "second line" >&2
	exit 1
fi`)
	n, logs := newTestNotifier(t, cmd, QueueSize, Timeout)
	n.Start()
	n.Notify(Event{Kind: KindDeliveryError, Project: "bad", Target: "github"})
	n.Notify(Event{Kind: KindDelivered, Project: "good", Target: "github"})
	n.Stop(10 * time.Second)

	if got := readFile(t, filepath.Join(dir, "runs")); got != "bad\ngood\n" {
		t.Fatalf("runs:\n%s", got)
	}
	warn := logLine(t, logs.String(), "notification command failed")
	for _, s := range []string{"event=delivery_error", "project=bad", "target=github", `stderr="webhook is not reachable"`, "exit status 1"} {
		if !strings.Contains(warn, s) {
			t.Errorf("no %s in %q", s, warn)
		}
	}
}

func TestNotifierKillsSlowCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OUT", dir)
	cmd := writeScript(t, dir, "notify.sh", `
case "$METAGIT_PROJECT" in
warmup) exit 0 ;;
slow) exec sleep 30 ;;
esac
echo "$METAGIT_PROJECT" >> "$OUT/runs"`)
	// The first run of a new executable can take hundreds of milliseconds on
	// macOS under load, longer than the short timeout below.
	warmup := exec.Command(cmd)
	warmup.Env = append(os.Environ(), "METAGIT_PROJECT=warmup")
	if out, err := warmup.CombinedOutput(); err != nil {
		t.Fatalf("warm-up run: %v\n%s", err, out)
	}
	n, logs := newTestNotifier(t, cmd, QueueSize, 300*time.Millisecond)
	n.Start()
	start := time.Now()
	n.Notify(Event{Kind: KindDelivered, Project: "slow", Target: "github"})
	n.Notify(Event{Kind: KindDelivered, Project: "next", Target: "github"})
	n.Stop(10 * time.Second)

	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %s", d)
	}
	runs, _ := os.ReadFile(filepath.Join(dir, "runs"))
	if string(runs) != "next\n" {
		t.Fatalf("runs:\n%s\nlog:\n%s", runs, logs)
	}
	warn := logLine(t, logs.String(), "notification command failed")
	if !strings.Contains(warn, "project=slow") || !strings.Contains(warn, "killed after 300ms") {
		t.Fatalf("warning: %q", warn)
	}
}

func TestNotifierQueueAndStop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OUT", dir)
	cmd := writeScript(t, dir, "notify.sh", `
touch "$OUT/started"
exec sleep 30`)
	n, logs := newTestNotifier(t, cmd, 2, Timeout)
	n.Start()
	n.Notify(Event{Kind: KindDelivered, Project: "p1"})
	waitFile(t, filepath.Join(dir, "started"))
	n.Notify(Event{Kind: KindDelivered, Project: "p2"})
	n.Notify(Event{Kind: KindDelivered, Project: "p3"})
	n.Notify(Event{Kind: KindDelivered, Project: "p4"})

	start := time.Now()
	n.Stop(200 * time.Millisecond)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("Stop took %s", d)
	}
	n.Notify(Event{Kind: KindDelivered, Project: "p5"})

	out := logs.String()
	if line := logLine(t, out, "notification dropped"); !strings.Contains(line, "project=p4") || !strings.Contains(line, `reason="queue is full"`) {
		t.Errorf("dropped: %q", line)
	}
	if !strings.Contains(out, `project=p5 target="" reason="service is stopping"`) {
		t.Errorf("no warning for an event after Stop:\n%s", out)
	}
	// p1 is interrupted, p2 and p3 never start.
	if line := logLine(t, out, "notifications not sent"); !strings.Contains(line, "count=3") {
		t.Errorf("not sent: %q", line)
	}
	if strings.Contains(out, "notification command failed") {
		t.Errorf("an interrupted command is reported as failed:\n%s", out)
	}
}

func TestNilNotifier(t *testing.T) {
	var n *Notifier
	n.Start()
	n.Notify(Event{Kind: KindDelivered})
	n.Stop(time.Second)
}

func TestStopWithoutStart(t *testing.T) {
	n, _ := newTestNotifier(t, "/bin/true", QueueSize, Timeout)
	n.Notify(Event{Kind: KindDelivered})
	n.Stop(time.Second)
}

func newTestNotifier(t *testing.T, cmd string, queueSize int, timeout time.Duration) (*Notifier, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	n := newNotifier(cmd, log, queueSize, timeout)
	t.Cleanup(func() { n.Stop(0) })
	return n, logs
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func waitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func logLine(t *testing.T, logs, msg string) string {
	t.Helper()
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, `msg="`+msg) {
			return line
		}
	}
	t.Fatalf("no warning %q in:\n%s", msg, logs)
	return ""
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
