package replicate

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestValidateURL(t *testing.T) {
	for _, ok := range []string{
		"https://github.com/u/foo.git",
		"https://bitbucket.org/u/foo",
		"ssh://git@github.com/u/foo.git",
		"ssh://git@host:2222/foo.git",
		"git@github.com:u/foo.git",
		"git@codeberg.org:u/foo.git",
	} {
		if err := ValidateURL(ok); err != nil {
			t.Errorf("%q must be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"",
		"http://github.com/u/foo.git",
		"file:///tmp/foo.git",
		"/tmp/foo.git",
		"git://github.com/u/foo.git",
		"https://github.com",
		"-oProxyCommand=evil",
		"github.com:u/foo.git",
		"https://github.com/u/foo .git",
	} {
		if err := ValidateURL(bad); err == nil {
			t.Errorf("%q must be invalid", bad)
		}
	}
	for _, secret := range []string{
		"https://user:token@github.com/u/foo.git",
		"https://ghp_token@github.com/u/foo.git",
		"ssh://git:password@host/foo.git",
	} {
		if err := ValidateURL(secret); !errors.Is(err, ErrSecretInURL) {
			t.Errorf("%q: expected ErrSecretInURL, got %v", secret, err)
		}
	}
}

func TestParsePorcelain(t *testing.T) {
	out := []byte("To https://example.com/foo.git\n" +
		" \t1111:refs/heads/main\taaa..bbb\n" +
		"+\t2222:refs/heads/feature\taaa...bbb (forced update)\n" +
		"*\t3333:refs/tags/v1\t[new tag]\n" +
		"-\t:refs/heads/old\t[deleted]\n" +
		"=\t4444:refs/heads/same\t[up to date]\n" +
		"!\t5555:refs/heads/stale\t[rejected] (stale info)\n" +
		"!\t(delete):refs/heads/gone\t[rejected] (stale info)\n" +
		"!\t6666:refs/heads/hooked\t[remote rejected] (pre-receive hook declined)\n" +
		"Done\n")
	got := parsePorcelain(out)
	want := map[string]refStatus{
		"refs/heads/main":    refOK,
		"refs/heads/feature": refOK,
		"refs/tags/v1":       refOK,
		"refs/heads/old":     refOK,
		"refs/heads/same":    refOK,
		"refs/heads/stale":   refStale,
		"refs/heads/gone":    refStale,
		"refs/heads/hooked":  refFailed,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results: %+v", len(got), got)
	}
	for ref, status := range want {
		if got[ref].status != status {
			t.Errorf("%s: status %d, want %d (%+v)", ref, got[ref].status, status, got[ref])
		}
	}
	if got["refs/heads/hooked"].summary != "[remote rejected] (pre-receive hook declined)" {
		t.Errorf("summary = %q", got["refs/heads/hooked"].summary)
	}
}

func TestAskpass(t *testing.T) {
	env := map[string]string{EnvAskpassUsername: "user", EnvAskpassPassword: "secret"}
	getenv := func(k string) string { return env[k] }
	var out bytes.Buffer
	if code := Askpass("Username for 'https://github.com': ", getenv, &out); code != 0 || out.String() != "user\n" {
		t.Fatalf("username: %d %q", code, out.String())
	}
	out.Reset()
	if code := Askpass("Password for 'https://user@github.com': ", getenv, &out); code != 0 || out.String() != "secret\n" {
		t.Fatalf("password: %d %q", code, out.String())
	}
	if code := Askpass("Something else", getenv, &out); code == 0 {
		t.Fatal("unknown prompt must fail")
	}
	if code := Askpass("Password: ", func(string) string { return "" }, &out); code == 0 {
		t.Fatal("missing password must fail")
	}
}

func TestBackoff(t *testing.T) {
	r := &Replicator{opts: Options{RetryMin: 15 * time.Second, RetryMax: 30 * time.Minute}}
	cases := []struct {
		attempt int
		base    time.Duration
	}{
		{1, 15 * time.Second},
		{2, 30 * time.Second},
		{3, time.Minute},
		{10, 30 * time.Minute},
		{100, 30 * time.Minute},
	}
	for _, tc := range cases {
		for range 20 {
			d := r.backoff(tc.attempt)
			lo, hi := time.Duration(float64(tc.base)*0.8), time.Duration(float64(tc.base)*1.2)
			if d < lo || d > hi {
				t.Fatalf("attempt %d: %v not in [%v, %v]", tc.attempt, d, lo, hi)
			}
		}
	}
}

func TestRemoteDetails(t *testing.T) {
	stderr := "error: failed to push\nremote: error: GH006: Protected branch update failed\nremote:\nTo https://x\n"
	if got := remoteDetails(stderr); got != "remote: error: GH006: Protected branch update failed" {
		t.Fatalf("details = %q", got)
	}
}
