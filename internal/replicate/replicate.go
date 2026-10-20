// Package replicate delivers branches and tags of metagit projects to targets.
//
// Delivery is reconciliation, not event replay: every run compares the refs
// of the metagit repository with what metagit last delivered to the target
// (store.TargetRefs) and pushes the difference with --force-with-lease.
package replicate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ekalinin/metagit/internal/archive"
	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/secrets"
	"github.com/ekalinin/metagit/internal/store"
)

// Status is the outcome of one delivery run.
type Status string

// Delivery outcomes.
const (
	StatusDelivered Status = "delivered"
	StatusDiverged  Status = "diverged"
	StatusError     Status = "error"
	StatusDisabled  Status = "disabled"
	StatusRemoved   Status = "removed"
)

// Result describes a finished delivery run.
type Result struct {
	TargetID   int64
	TargetName string
	Status     Status
	// Archives are archive refs created for diverged refs of the target.
	Archives []string
	Error    string
	// NextAttempt is set when the run failed.
	NextAttempt time.Time
}

// Options configure a Replicator.
type Options struct {
	MaxParallel int
	GitTimeout  time.Duration
	RetryMin    time.Duration
	RetryMax    time.Duration
	// Askpass is the script git runs to get HTTPS credentials.
	Askpass string
	// TmpDir keeps temporary SSH key files.
	TmpDir string
	// KnownHosts is the known_hosts file for SSH targets.
	KnownHosts string
	// ExtraEnv is added to the environment of git commands (tests).
	ExtraEnv []string
	// MaxRounds limits push attempts per run when refs keep changing.
	MaxRounds int
	// BatchSize limits refs per git push command.
	BatchSize int
}

// Replicator runs one worker per target.
type Replicator struct {
	store   *store.Store
	repos   *repo.Manager
	box     *secrets.Box
	archive *archive.Service
	opts    Options
	log     *slog.Logger
	git     *gitcmd.Runner

	sem chan struct{}

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	workers map[int64]*worker
	wg      sync.WaitGroup
}

// New creates a Replicator. Call Start to run it.
func New(st *store.Store, repos *repo.Manager, box *secrets.Box, arch *archive.Service, opts Options, log *slog.Logger) *Replicator {
	opts.MaxParallel = max(opts.MaxParallel, 1)
	opts.GitTimeout = cmp.Or(opts.GitTimeout, 10*time.Minute)
	opts.RetryMin = cmp.Or(opts.RetryMin, 15*time.Second)
	opts.RetryMax = cmp.Or(opts.RetryMax, 30*time.Minute)
	opts.MaxRounds = cmp.Or(opts.MaxRounds, 3)
	opts.BatchSize = cmp.Or(opts.BatchSize, 200)
	ctx, cancel := context.WithCancel(context.Background())
	return &Replicator{
		ctx:     ctx,
		cancel:  cancel,
		store:   st,
		repos:   repos,
		box:     box,
		archive: arch,
		opts:    opts,
		log:     log,
		git:     &gitcmd.Runner{Timeout: opts.GitTimeout, Env: append(gitcmd.CleanEnv(), opts.ExtraEnv...)},
		sem:     make(chan struct{}, opts.MaxParallel),
		workers: map[int64]*worker{},
	}
}

// Start creates workers for all targets. Pending targets are delivered at
// once, failed ones at their next attempt time.
func (r *Replicator) Start(ctx context.Context) error {
	targets, err := r.store.ListAllTargets(ctx)
	if err != nil {
		return err
	}
	for _, t := range targets {
		w := r.ensureWorker(t.ID)
		if !t.Enabled {
			continue
		}
		switch t.State {
		case store.StateError:
			w.retryLater(t.NextAttemptAt)
		case store.StatePending:
			w.trigger()
		}
	}
	return nil
}

// Stop cancels running deliveries and waits for workers to exit.
func (r *Replicator) Stop() {
	r.cancel()
	r.wg.Wait()
}

func (r *Replicator) ensureWorker(id int64) *worker {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w, ok := r.workers[id]; ok {
		return w
	}
	ctx, cancel := context.WithCancel(r.ctx)
	w := &worker{id: id, r: r, wake: make(chan struct{}, 1), changed: make(chan struct{}), cancel: cancel}
	r.workers[id] = w
	r.wg.Go(func() { w.loop(ctx) })
	return w
}

// Trigger asks for a delivery to the target and returns a ticket for Wait.
func (r *Replicator) Trigger(ctx context.Context, id int64) uint64 {
	_ = r.store.MarkTargetPending(ctx, id)
	return r.ensureWorker(id).trigger()
}

// TriggerProject triggers all enabled targets of a project.
func (r *Replicator) TriggerProject(ctx context.Context, projectID int64) (map[int64]uint64, error) {
	targets, err := r.store.ListTargets(ctx, projectID)
	if err != nil {
		return nil, err
	}
	tickets := map[int64]uint64{}
	for _, t := range targets {
		if t.Enabled {
			tickets[t.ID] = r.Trigger(ctx, t.ID)
		}
	}
	return tickets, nil
}

// Wait waits for a run that started after the trigger with the ticket. It
// returns false when ctx ends first.
func (r *Replicator) Wait(ctx context.Context, id int64, ticket uint64) (Result, bool) {
	r.mu.Lock()
	w, ok := r.workers[id]
	r.mu.Unlock()
	if !ok {
		return Result{}, false
	}
	return w.wait(ctx, ticket)
}

// RemoveTarget stops the worker of a deleted target.
func (r *Replicator) RemoveTarget(id int64) {
	r.mu.Lock()
	w, ok := r.workers[id]
	delete(r.workers, id)
	r.mu.Unlock()
	if ok {
		w.cancel()
	}
}

type worker struct {
	id     int64
	r      *Replicator
	wake   chan struct{}
	cancel context.CancelFunc

	mu      sync.Mutex
	want    uint64
	done    uint64
	result  Result
	changed chan struct{}
	retryAt time.Time
}

func (w *worker) trigger() uint64 {
	w.mu.Lock()
	w.want++
	ticket := w.want
	w.retryAt = time.Time{}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return ticket
}

func (w *worker) retryLater(at time.Time) {
	w.mu.Lock()
	w.retryAt = at
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *worker) wait(ctx context.Context, ticket uint64) (Result, bool) {
	for {
		w.mu.Lock()
		if w.done >= ticket {
			res := w.result
			w.mu.Unlock()
			return res, true
		}
		ch := w.changed
		w.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return Result{}, false
		}
	}
}

func (w *worker) loop(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		w.mu.Lock()
		pending := w.want > w.done
		retryAt := w.retryAt
		ticket := w.want
		w.mu.Unlock()

		if !pending {
			var timerC <-chan time.Time
			if !retryAt.IsZero() {
				timer.Reset(max(time.Until(retryAt), 0))
				timerC = timer.C
			}
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-w.wake:
				timer.Stop()
			case <-timerC:
				w.trigger()
			}
			continue
		}

		select {
		case w.r.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		res := w.r.deliver(ctx, w.id)
		<-w.r.sem

		w.mu.Lock()
		w.done = ticket
		w.result = res
		close(w.changed)
		w.changed = make(chan struct{})
		if w.want == ticket {
			w.retryAt = res.NextAttempt
		}
		w.mu.Unlock()
		if res.Status == StatusRemoved {
			return
		}
	}
}

// deliver runs one delivery to a target and saves its status.
func (r *Replicator) deliver(ctx context.Context, id int64) Result {
	t, err := r.store.TargetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return Result{TargetID: id, Status: StatusRemoved}
	}
	res := Result{TargetID: id}
	if err != nil {
		res.Status, res.Error = StatusError, err.Error()
		return res
	}
	res.TargetName = t.Name
	if !t.Enabled {
		res.Status = StatusDisabled
		return res
	}
	project, err := r.store.ProjectByID(ctx, t.ProjectID)
	if err != nil {
		res.Status, res.Error = StatusError, err.Error()
		return res
	}

	run := &run{r: r, project: project, target: t, gitDir: r.repos.Path(project.Name)}
	err = run.prepare()
	if err == nil {
		defer run.cleanup()
		err = run.reconcile(ctx)
	}
	if ctx.Err() != nil {
		// The service is stopping; keep the state for the next start.
		res.Status, res.Error = StatusError, "delivery interrupted"
		return res
	}
	res.Archives = run.archives
	now := time.Now()
	if err != nil {
		msg := run.mask(err.Error())
		res.Status, res.Error = StatusError, msg
		attempt := t.Attempt + 1
		res.NextAttempt = now.Add(r.backoff(attempt))
		st := store.TargetStatus{State: store.StateError, LastError: msg, LastSuccessAt: t.LastSuccessAt, NextAttemptAt: res.NextAttempt, Attempt: attempt}
		if err := r.store.SetTargetStatus(ctx, t.ID, st); err != nil {
			r.log.Error("cannot save target status", "target", t.ID, "err", err)
		}
		if t.State != store.StateError || t.LastError != msg {
			_ = r.store.AddEvent(ctx, &store.Event{ProjectID: project.ID, TargetID: t.ID, TargetName: t.Name,
				Kind: store.EventDeliveryError, Message: "delivery error: " + msg})
		}
		r.log.Warn("delivery failed", "project", project.Name, "target", t.Name, "attempt", attempt, "err", msg)
		return res
	}
	res.Status = StatusDelivered
	if len(run.archives) > 0 {
		res.Status = StatusDiverged
	}
	st := store.TargetStatus{State: store.StateSynced, LastSuccessAt: now}
	if err := r.store.SetTargetStatus(ctx, t.ID, st); err != nil {
		r.log.Error("cannot save target status", "target", t.ID, "err", err)
	}
	return res
}

func (r *Replicator) backoff(attempt int) time.Duration {
	d := r.opts.RetryMin
	for i := 1; i < attempt && d < r.opts.RetryMax; i++ {
		d *= 2
	}
	d = min(d, r.opts.RetryMax)
	// +-20% jitter so that many failed targets do not retry at once.
	jitter := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(d) * jitter)
}

// run is the state of one delivery.
type run struct {
	r       *Replicator
	project *store.Project
	target  *store.Target
	gitDir  string

	env      []string
	secrets  []string
	keyFile  string
	archives []string
}

func (u *run) mask(s string) string {
	return secrets.Mask(s, u.secrets...)
}

// prepare sets up credentials for git.
func (u *run) prepare() error {
	sshCmd := []string{"ssh", "-F", os.DevNull, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new"}
	if u.r.opts.KnownHosts != "" {
		sshCmd = append(sshCmd, "-o", "UserKnownHostsFile="+repo.ShellQuote(u.r.opts.KnownHosts))
	}
	secret, err := u.r.box.Open(u.target.Secret)
	if err != nil {
		return err
	}
	switch u.target.AuthKind {
	case store.AuthHTTPS:
		if len(secret) == 0 {
			return errors.New("token is not set")
		}
		u.secrets = append(u.secrets, string(secret))
		u.env = append(u.env,
			"GIT_ASKPASS="+u.r.opts.Askpass,
			EnvAskpassUsername+"="+cmp.Or(u.target.Username, "git"),
			EnvAskpassPassword+"="+string(secret),
		)
	case store.AuthSSH:
		if len(secret) == 0 {
			return errors.New("SSH key is not set")
		}
		f, err := os.CreateTemp(u.r.opts.TmpDir, "key-*")
		if err != nil {
			return err
		}
		u.keyFile = f.Name()
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return err
		}
		key := string(secret)
		if !strings.HasSuffix(key, "\n") {
			key += "\n"
		}
		if _, err := f.WriteString(key); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		sshCmd = append(sshCmd, "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none", "-i", repo.ShellQuote(u.keyFile))
	}
	u.env = append(u.env, "GIT_SSH_COMMAND="+strings.Join(sshCmd, " "))
	return nil
}

func (u *run) cleanup() {
	if u.keyFile != "" {
		_ = os.Remove(u.keyFile)
	}
}

// remoteGit runs a git command that talks to the target.
func (u *run) remoteGit(ctx context.Context, args ...string) (*gitcmd.Result, error) {
	args = append([]string{"-c", "credential.helper=", "-c", "gc.auto=0"}, args...)
	return u.r.git.Run(ctx, gitcmd.Opts{Dir: u.gitDir, Env: u.env}, args...)
}

// op is a ref change to apply on the target.
type op struct {
	ref    string
	object string // "" deletes the ref
	expect string // value the target must have; "" means "must not exist"
}

func (u *run) reconcile(ctx context.Context) error {
	refs, err := u.r.git.ForEachRef(ctx, u.gitDir, "refs/heads/", "refs/tags/")
	if err != nil {
		return err
	}
	desired := make(map[string]string, len(refs))
	for _, ref := range refs {
		desired[ref.Name] = ref.Object
	}
	known, err := u.r.store.TargetRefs(ctx, u.target.ID)
	if err != nil {
		return err
	}
	var ops []op
	for _, ref := range slices.Sorted(maps.Keys(desired)) {
		if known[ref] != desired[ref] {
			ops = append(ops, op{ref: ref, object: desired[ref], expect: known[ref]})
		}
	}
	for _, ref := range slices.Sorted(maps.Keys(known)) {
		if _, ok := desired[ref]; !ok {
			ops = append(ops, op{ref: ref, expect: known[ref]})
		}
	}
	if len(ops) == 0 {
		// Nothing to push: wrong credentials must not look synced.
		return u.checkAccess(ctx)
	}

	var failures []string
	var details string
	for round := 0; len(ops) > 0; round++ {
		if round >= u.r.opts.MaxRounds {
			names := make([]string, len(ops))
			for i, o := range ops {
				names[i] = o.ref
			}
			failures = append(failures, "refs on the hosting keep changing: "+strings.Join(names, ", "))
			break
		}
		var retry []op
		for batch := range slices.Chunk(ops, u.r.opts.BatchSize) {
			results, stderr, err := u.push(ctx, batch)
			if err != nil {
				return err
			}
			for _, o := range batch {
				res, ok := results[o.ref]
				switch {
				case !ok:
					failures = append(failures, o.ref+": no response from the hosting")
				case res.status == refOK:
					if err := u.record(ctx, o.ref, o.object); err != nil {
						return err
					}
				case res.status == refStale:
					next, err := u.diverged(ctx, o)
					if err != nil {
						return err
					}
					if next != nil {
						retry = append(retry, *next)
					}
				default:
					failures = append(failures, o.ref+": "+res.summary)
					details = remoteDetails(stderr)
				}
			}
		}
		ops = retry
	}
	if len(failures) > 0 {
		msg := strings.Join(failures, "; ")
		if details != "" {
			msg += "\n" + details
		}
		return errors.New(msg)
	}
	return nil
}

// remoteDetails keeps the messages of the hosting ("remote: ...") that
// explain why it rejected a ref.
func remoteDetails(stderr string) string {
	var lines []string
	for line := range strings.SplitSeq(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "remote:") && strings.TrimSpace(strings.TrimPrefix(line, "remote:")) != "" {
			lines = append(lines, line)
		}
		if len(lines) == 20 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func (u *run) record(ctx context.Context, ref, object string) error {
	if object == "" {
		return u.r.store.DeleteTargetRef(ctx, u.target.ID, ref)
	}
	return u.r.store.SetTargetRef(ctx, u.target.ID, ref, object)
}

func (u *run) push(ctx context.Context, ops []op) (map[string]refResult, string, error) {
	args := []string{"push", "--porcelain"}
	var specs []string
	for _, o := range ops {
		args = append(args, "--force-with-lease="+o.ref+":"+o.expect)
		specs = append(specs, o.object+":"+o.ref)
	}
	args = append(args, "--", u.target.URL)
	args = append(args, specs...)
	res, err := u.remoteGit(ctx, args...)
	results := parsePorcelain(res.Stdout)
	if err != nil && len(results) == 0 {
		return nil, "", gitError(err)
	}
	return results, string(res.Stderr), nil
}

// checkRefspec matches no local ref (metagit never creates refs under
// refs/metagit/check/), so a push with it sends nothing.
const checkRefspec = "refs/metagit/check/*:refs/metagit/check/*"

// checkAccess connects to the push service of the target (receive-pack)
// without changing anything there, so it fails when the credentials do not
// allow pushing. "git ls-remote" is not enough: it talks to upload-pack,
// which a public repository or a read-only key also answers.
func (u *run) checkAccess(ctx context.Context) error {
	res, err := u.remoteGit(ctx, "push", "--dry-run", "--", u.target.URL, checkRefspec)
	// When the repository on the hosting is empty, git closes an SSH
	// connection without the final packet and reports a failure, although
	// the hosting has already accepted the key and sent its (empty) refs.
	if err != nil && !strings.Contains(string(res.Stderr), "No refs in common") {
		return gitError(err)
	}
	return nil
}

// diverged handles a ref whose value on the target is not what metagit
// expected. It returns the operation to retry, or nil when nothing is left.
func (u *run) diverged(ctx context.Context, o op) (*op, error) {
	current, err := u.lsRemote(ctx, o.ref)
	if err != nil {
		return nil, err
	}
	switch {
	case current == o.object:
		// Already there (or already deleted).
		return nil, u.record(ctx, o.ref, o.object)
	case current == "":
		// Deleted on the hosting: nothing to save, just write again.
		return &op{ref: o.ref, object: o.object, expect: ""}, nil
	}
	if o.object != "" && strings.HasPrefix(o.ref, "refs/heads/") && u.r.git.HasObject(ctx, u.gitDir, current) {
		ok, err := u.r.git.IsAncestor(ctx, gitcmd.Opts{Dir: u.gitDir}, current, o.object)
		if err != nil {
			return nil, err
		}
		if ok {
			// The hosting is behind: a fast-forward loses nothing.
			return &op{ref: o.ref, object: o.object, expect: current}, nil
		}
	}

	fetched, err := u.fetch(ctx, o.ref)
	if err != nil {
		return nil, err
	}
	name, err := u.r.archive.SaveTargetState(ctx, u.project, u.target, o.ref, fetched)
	if err != nil {
		return nil, err
	}
	u.archives = append(u.archives, name)
	what := "branch"
	if strings.HasPrefix(o.ref, "refs/tags/") {
		what = "tag"
	}
	action := "is overwritten with the value from metagit"
	if o.object == "" {
		action = "is deleted, as in metagit"
	}
	err = u.r.store.AddEvent(ctx, &store.Event{
		ProjectID: u.project.ID, TargetID: u.target.ID, TargetName: u.target.Name,
		Kind: store.EventDiverged, Ref: o.ref, ArchiveRef: name,
		Message: fmt.Sprintf("%s %s on target %s diverged from metagit: hosting state %s saved in %s, ref %s",
			what, o.ref, u.target.Name, short(fetched), name, action),
	})
	if err != nil {
		return nil, err
	}
	return &op{ref: o.ref, object: o.object, expect: fetched}, nil
}

// lsRemote returns the object of ref on the target, or "".
func (u *run) lsRemote(ctx context.Context, ref string) (string, error) {
	res, err := u.remoteGit(ctx, "ls-remote", "--", u.target.URL, ref)
	if err != nil {
		return "", gitError(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(res.Stdout)), "\n") {
		oid, name, ok := strings.Cut(line, "\t")
		if ok && name == ref {
			return oid, nil
		}
	}
	return "", nil
}

// fetch copies the target's ref into the repository and returns its object.
func (u *run) fetch(ctx context.Context, ref string) (string, error) {
	tmp := archive.TmpRef(u.target.ID)
	if _, err := u.remoteGit(ctx, "fetch", "--no-tags", "--no-write-fetch-head", "--", u.target.URL, "+"+ref+":"+tmp); err != nil {
		return "", gitError(err)
	}
	defer func() {
		_, _ = u.r.git.Run(context.WithoutCancel(ctx), gitcmd.Opts{Dir: u.gitDir}, "update-ref", "-d", tmp)
	}()
	oid, err := u.r.git.ResolveRef(ctx, u.gitDir, tmp)
	if err != nil {
		return "", err
	}
	if oid == "" {
		return "", fmt.Errorf("%s not found on the hosting when fetching", ref)
	}
	return oid, nil
}

// gitError keeps the useful part of a git failure.
func gitError(err error) error {
	if gerr, ok := errors.AsType[*gitcmd.Error](err); ok {
		if errors.Is(gerr.Err, context.DeadlineExceeded) {
			return errors.New("timed out waiting for the hosting")
		}
		msg := strings.TrimSpace(gerr.Stderr)
		if msg == "" {
			msg = gerr.Error()
		}
		return errors.New(msg)
	}
	return err
}

func short(oid string) string {
	if len(oid) > 12 {
		return oid[:12]
	}
	return oid
}
