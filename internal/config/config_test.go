package config

import (
	"io"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(nil, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != DefaultDataDir || cfg.HTTPAddr != DefaultHTTPAddr || cfg.SSHAddr != DefaultSSHAddr {
		t.Errorf("unexpected addresses: %+v", cfg)
	}
	if cfg.PushStatusTimeout != 5*time.Second {
		t.Errorf("push status timeout = %v", cfg.PushStatusTimeout)
	}
	if cfg.MaxParallelDeliveries != 4 {
		t.Errorf("max parallel deliveries = %d", cfg.MaxParallelDeliveries)
	}
	if cfg.GitTimeout != 10*time.Minute {
		t.Errorf("git timeout = %v", cfg.GitTimeout)
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("base url = %q", cfg.BaseURL)
	}
	if cfg.SSHURL != "ssh://git@localhost:2222" {
		t.Errorf("ssh url = %q", cfg.SSHURL)
	}
}

func TestEnvironmentOverridesDefault(t *testing.T) {
	cfg, err := Load(nil, env(map[string]string{
		"METAGIT_DATA_DIR":                "/srv/metagit",
		"METAGIT_PUSH_STATUS_TIMEOUT":     "2s",
		"METAGIT_MAX_PARALLEL_DELIVERIES": "8",
	}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/srv/metagit" || cfg.PushStatusTimeout != 2*time.Second || cfg.MaxParallelDeliveries != 8 {
		t.Errorf("environment ignored: %+v", cfg)
	}
}

func TestFlagOverridesEnvironment(t *testing.T) {
	cfg, err := Load(
		[]string{"--data-dir", "/flag", "--push-status-timeout", "1s", "--http-addr", "127.0.0.1:9000"},
		env(map[string]string{"METAGIT_DATA_DIR": "/env", "METAGIT_PUSH_STATUS_TIMEOUT": "2s"}),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/flag" || cfg.PushStatusTimeout != time.Second {
		t.Errorf("flag did not win: %+v", cfg)
	}
	if cfg.BaseURL != "http://127.0.0.1:9000" {
		t.Errorf("base url = %q", cfg.BaseURL)
	}
}

func TestInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  map[string]string
	}{
		{"bad env duration", nil, map[string]string{"METAGIT_PUSH_STATUS_TIMEOUT": "soon"}},
		{"bad env int", nil, map[string]string{"METAGIT_MAX_PARALLEL_DELIVERIES": "many"}},
		{"zero parallel", []string{"--max-parallel-deliveries", "0"}, nil},
		{"tls cert without key", []string{"--tls-cert", "c.pem"}, nil},
		{"extra args", []string{"extra"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(tc.args, env(tc.env), io.Discard); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTLSChangesDefaultScheme(t *testing.T) {
	cfg, err := Load([]string{"--tls-cert", "c.pem", "--tls-key", "k.pem", "--http-addr", "0.0.0.0:443"}, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://localhost:443" {
		t.Errorf("base url = %q", cfg.BaseURL)
	}
}
