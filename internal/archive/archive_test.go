package archive

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ekalinin/metagit/internal/gitcmd"
	"github.com/ekalinin/metagit/internal/hooks"
	"github.com/ekalinin/metagit/internal/repo"
	"github.com/ekalinin/metagit/internal/store"
	"github.com/ekalinin/metagit/internal/testutil"
)

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, ref string
		want         bool
	}{
		{"refs/heads/main", "refs/heads/main", true},
		{"refs/heads/main", "refs/heads/main2", false},
		{"refs/tags/*", "refs/tags/v1.0", true},
		{"refs/tags/*", "refs/tags/release/v2", true},
		{"refs/heads/release/*", "refs/heads/release/1.x", true},
		{"refs/heads/release/*", "refs/heads/main", false},
		{"refs/heads/v1.?", "refs/heads/v1.x", false},
	}
	for _, tc := range cases {
		if got := MatchPattern(tc.pattern, tc.ref); got != tc.want {
			t.Errorf("MatchPattern(%q, %q) = %v", tc.pattern, tc.ref, got)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	for _, ok := range []string{"refs/heads/main", "refs/tags/*"} {
		if err := ValidatePattern(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "main", "refs/", "refs/metagit/archive/*", "refs/heads/a b"} {
		if err := ValidatePattern(bad); err == nil {
			t.Errorf("%q must be invalid", bad)
		}
	}
}

func TestCheck(t *testing.T) {
	updates := []hooks.Update{
		{Ref: "refs/heads/feature", Kind: hooks.KindNonFastForward},
		{Ref: "refs/heads/main", Kind: hooks.KindFastForward},
	}
	if msgs := Check(updates, []string{"refs/heads/main"}); msgs != nil {
		t.Fatalf("allowed push rejected: %v", msgs)
	}
	updates = append(updates, hooks.Update{Ref: "refs/heads/main", Kind: hooks.KindDelete})
	msgs := Check(updates, []string{"refs/heads/main"})
	if len(msgs) != 2 || !strings.Contains(msgs[0], "refs/heads/main is protected") {
		t.Fatalf("msgs = %v", msgs)
	}
	msgs = Check([]hooks.Update{{Ref: "refs/metagit/archive/x", Kind: hooks.KindCreate}}, nil)
	if len(msgs) == 0 || !strings.Contains(msgs[0], "reserved namespace") {
		t.Fatalf("msgs = %v", msgs)
	}
}

func TestNames(t *testing.T) {
	ts := time.Date(2026, 10, 14, 12, 0, 1, 0, time.FixedZone("MSK", 3*3600))
	if got := Name(store.SourcePush, 0, "refs/heads/feature/x", ts); got != "refs/metagit/archive/push/heads/feature/x/20261014T090001Z" {
		t.Errorf("push name = %q", got)
	}
	if got := Name(store.SourceTarget, 3, "refs/tags/v1", ts); got != "refs/metagit/archive/targets/3/tags/v1/20261014T090001Z" {
		t.Errorf("target name = %q", got)
	}
}

func TestSaveTargetStateUsesUniqueNames(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "metagit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repos := &repo.Manager{ReposDir: filepath.Join(dir, "repos"), HooksDir: filepath.Join(dir, "hooks"), Git: &gitcmd.Runner{}}
	p, _ := st.CreateProject(ctx, "foo")
	if err := repos.Create(ctx, "foo"); err != nil {
		t.Fatal(err)
	}
	work := testutil.InitWork(t)
	a := testutil.Commit(t, work, "a")
	b := testutil.Commit(t, work, "b")
	testutil.Git(t, work, "push", "-q", repos.Path("foo"), "main")

	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	s := &Service{Store: st, Repos: repos, Git: &gitcmd.Runner{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return now }}
	target := &store.Target{ID: 5, Name: "gh"}
	n1, err := s.SaveTargetState(ctx, &p, target, "refs/heads/main", a)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := s.SaveTargetState(ctx, &p, target, "refs/heads/main", b)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != "refs/metagit/archive/targets/5/heads/main/20261014T120000Z" || n2 != n1+"-2" {
		t.Fatalf("names %q %q", n1, n2)
	}
	// The same object under the same time reuses the name (idempotent retry).
	n3, err := s.SaveTargetState(ctx, &p, target, "refs/heads/main", a)
	if err != nil || n3 != n1 {
		t.Fatalf("retry name %q %v", n3, err)
	}
	if testutil.RefOf(t, repos.Path("foo"), n1) != a || testutil.RefOf(t, repos.Path("foo"), n2) != b {
		t.Fatal("archive refs point to wrong objects")
	}
}
