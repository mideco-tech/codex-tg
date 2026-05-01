package main

import (
	"io"
	"log"
	"testing"

	"github.com/mideco-tech/codex-tg/internal/config"
)

func TestDaemonLogOutputCanBeDisabled(t *testing.T) {
	if got := daemonLogOutput(config.Config{LogEnabled: false}); got != io.Discard {
		t.Fatalf("daemonLogOutput(false) = %#v, want io.Discard", got)
	}
	if got := daemonLogOutput(config.Config{LogEnabled: true}); got == io.Discard {
		t.Fatal("daemonLogOutput(true) = io.Discard, want stdout logger")
	}
}

func TestDiagnosticLoggerHonorsFlags(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	if got := diagnosticLogger(config.Config{LogEnabled: true, DiagnosticLogs: true}, logger); got != logger {
		t.Fatal("diagnosticLogger(enabled, enabled) did not return logger")
	}
	if got := diagnosticLogger(config.Config{LogEnabled: false, DiagnosticLogs: true}, logger); got != nil {
		t.Fatal("diagnosticLogger(log disabled) returned logger, want nil")
	}
	if got := diagnosticLogger(config.Config{LogEnabled: true, DiagnosticLogs: false}, logger); got != nil {
		t.Fatal("diagnosticLogger(diagnostics disabled) returned logger, want nil")
	}
}
