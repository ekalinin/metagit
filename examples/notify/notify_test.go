// Package notify checks the notification command examples.
package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const event = `{"event":"delivery_error","time":"2026-10-21T11:38:01Z","project":"app","target":"github",` +
	`"ref":"","archive_ref":"","refs":[],"message":"delivery error: denied"}`

var eventEnv = []string{
	"METAGIT_EVENT=delivery_error",
	"METAGIT_TIME=2026-10-21T11:38:01Z",
	"METAGIT_PROJECT=app",
	"METAGIT_TARGET=github",
	"METAGIT_REF=",
	"METAGIT_ARCHIVE_REF=",
	"METAGIT_REFS=",
	"METAGIT_MESSAGE=delivery error: denied",
}

// run runs a script with the event on stdin and in the environment, and with
// bin first in PATH.
func run(t *testing.T, script, bin string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("./" + script)
	cmd.Stdin = strings.NewReader(event)
	cmd.Env = append(append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}, eventEnv...), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// fake writes a command into bin that saves its arguments (one per line)
// and stdin to files.
func fake(t *testing.T, bin, name string) (args, stdin string) {
	t.Helper()
	args, stdin = filepath.Join(bin, name+".args"), filepath.Join(bin, name+".stdin")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + args + "'\ncat > '" + stdin + "'\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return args, stdin
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestScriptsParse(t *testing.T) {
	for _, script := range []string{"webhook.sh", "telegram.sh", "email.sh"} {
		if out, err := exec.Command("sh", "-n", script).CombinedOutput(); err != nil {
			t.Errorf("sh -n %s: %v\n%s", script, err, out)
		}
	}
}

func TestWebhook(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}
	var body, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body, contentType = string(b), r.Header.Get("Content-Type")
	}))
	defer srv.Close()

	if out, err := run(t, "webhook.sh", t.TempDir(), "NOTIFY_WEBHOOK_URL="+srv.URL+"/hook"); err != nil {
		t.Fatalf("webhook.sh: %v\n%s", err, out)
	}
	if body != event || contentType != "application/json" {
		t.Fatalf("webhook got %q (%s)", body, contentType)
	}
	if out, err := run(t, "webhook.sh", t.TempDir(), "NOTIFY_WEBHOOK_URL="+srv.URL+"/missing"); err == nil {
		t.Fatalf("webhook.sh succeeds on 404:\n%s", out)
	}
}

func TestTelegram(t *testing.T) {
	bin := t.TempDir()
	args, _ := fake(t, bin, "curl")
	if out, err := run(t, "telegram.sh", bin, "TELEGRAM_BOT_TOKEN=123:abc", "TELEGRAM_CHAT_ID=42"); err != nil {
		t.Fatalf("telegram.sh: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(read(t, args), "\n"), "\n")
	for _, want := range []string{"chat_id=42", "text=metagit app (github): delivery error: denied", "https://api.telegram.org/bot123:abc/sendMessage"} {
		if !slices.Contains(got, want) {
			t.Errorf("curl arguments %q have no %q", got, want)
		}
	}
}

func TestEmail(t *testing.T) {
	bin := t.TempDir()
	args, stdin := fake(t, bin, "sendmail")
	if out, err := run(t, "email.sh", bin, "NOTIFY_EMAIL_TO=owner@example.com"); err != nil {
		t.Fatalf("email.sh: %v\n%s", err, out)
	}
	if got := read(t, args); got != "-t\n" {
		t.Errorf("sendmail arguments: %q", got)
	}
	mail := read(t, stdin)
	for _, want := range []string{
		"To: owner@example.com\n",
		"Subject: metagit: delivery_error in app\n",
		"\n\ndelivery error: denied\n",
		"Target: github\n",
	} {
		if !strings.Contains(mail, want) {
			t.Errorf("mail has no %q:\n%s", want, mail)
		}
	}
	if strings.Contains(mail, "From:") {
		t.Errorf("From without NOTIFY_EMAIL_FROM:\n%s", mail)
	}
}
