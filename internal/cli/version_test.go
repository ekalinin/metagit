package cli

import (
	"strings"
	"testing"
)

func TestFormatVersion(t *testing.T) {
	for _, tc := range []struct {
		version, revision string
		modified          bool
		want              string
	}{
		{"0.1.0", "3dc143a6f1b2c3d4e5f60718293a4b5c6d7e8f90", false, "0.1.0 (3dc143a)"},
		{"dev", "3dc143a6f1b2c3d4e5f60718293a4b5c6d7e8f90", true, "dev (3dc143a, modified)"},
		{"dev", "", false, "dev"},
	} {
		if got := formatVersion(tc.version, tc.revision, tc.modified); got != tc.want {
			t.Errorf("formatVersion(%q, %q, %v) = %q, want %q", tc.version, tc.revision, tc.modified, got, tc.want)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI(t, "", "version")
	if code != 0 || !strings.HasPrefix(out, "metagit dev") || strings.Count(out, "\n") != 1 {
		t.Fatalf("version: code %d, out %q", code, out)
	}
	code, flagOut, _ := runCLI(t, "", "--version")
	if code != 0 || flagOut != out {
		t.Fatalf("--version: code %d, out %q, want %q", code, flagOut, out)
	}
}
