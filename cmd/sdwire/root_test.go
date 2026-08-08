package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestVersionFormat(t *testing.T) {
	orig := version
	version = "1.2.3"
	t.Cleanup(func() { version = orig })

	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "sdwire, version 1.2.3\n"
	if buf.String() != want {
		t.Errorf("--version output = %q, want %q", buf.String(), want)
	}
}

func TestOpErrorDistinguishesExitCodes(t *testing.T) {
	plain := errors.New("plain")
	wrapped := opErrf("wrapped: %w", plain)

	var oe *opError
	if errors.As(plain, &oe) {
		t.Error("a plain error should not be treated as an opError")
	}
	if !errors.As(wrapped, &oe) {
		t.Error("opErrf's result should be an opError")
	}
	if !errors.Is(wrapped, plain) {
		t.Error("opErrf should preserve %w wrapping for errors.Is")
	}
}

func TestExecuteUnknownCommandIsUsageError(t *testing.T) {
	if code := Execute([]string{"not-a-real-command"}); code != 2 {
		t.Errorf("Execute([not-a-real-command]) = %d, want 2", code)
	}
}

func TestExecuteHelpSucceeds(t *testing.T) {
	if code := Execute([]string{"--help"}); code != 0 {
		t.Errorf("Execute([--help]) = %d, want 0", code)
	}
}
