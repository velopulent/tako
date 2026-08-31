package logging

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewRejectsInvalidLevel(t *testing.T) {
	t.Setenv("TAKO_LOG_LEVEL", "verbose")
	if _, err := New("serve"); err == nil {
		t.Fatal("invalid log level accepted")
	}
}

func TestNewAcceptsDebugLevel(t *testing.T) {
	t.Setenv("TAKO_LOG_LEVEL", "debug")
	logger, err := New("serve")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logger.Sync() }()
	if !logger.Core().Enabled(-1) {
		t.Fatal("debug logging is not enabled")
	}
}

func TestNewWithOutputWritesJournalFriendlyConsoleLogs(t *testing.T) {
	t.Setenv("TAKO_LOG_LEVEL", "info")
	var output bytes.Buffer
	logger, err := NewWithOutput("serve", &output)
	if err != nil {
		t.Fatal(err)
	}

	logger.Named("gateway").Info("HTTP request",
		zap.String("request_id", "spirit/000264"),
		zap.String("method", "GET"),
		zap.String("path", "/api/v1/services"),
		zap.Int("status", 503),
		zap.Int("bytes", 165),
		zap.String("remote_ip", "192.168.0.119:57798"),
		zap.Duration("duration", 435456*time.Nanosecond),
	)
	line := output.String()

	for _, want := range []string{
		"INFO", "gateway", "HTTP request", `"mode": "serve"`,
		`"request_id": "spirit/000264"`, `"method": "GET"`,
		`"path": "/api/v1/services"`, `"status": 503`, `"bytes": 165`,
		`"remote_ip": "192.168.0.119:57798"`, `"duration": "435.456µs"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q does not contain %q", line, want)
		}
	}
	for _, unwanted := range []string{`"ts"`, `"service"`, `"caller"`, "logging/logging_test.go:"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("log line %q contains redundant field %q", line, unwanted)
		}
	}
	if !strings.HasPrefix(line, "INFO") {
		t.Errorf("log line includes content before level: %q", line)
	}
}

func TestNewWithOutputPreservesErrorFields(t *testing.T) {
	t.Setenv("TAKO_LOG_LEVEL", "info")
	var output bytes.Buffer
	logger, err := NewWithOutput("serve", &output)
	if err != nil {
		t.Fatal(err)
	}

	logger.Error("module unavailable",
		zap.String("module", "services"),
		zap.Error(errors.New("permission denied")),
	)
	line := output.String()
	for _, want := range []string{"ERROR", "module unavailable", `"module": "services"`, `"error": "permission denied"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q does not contain %q", line, want)
		}
	}
}

func TestNewWithOutputIncludesCallerAtDebugLevel(t *testing.T) {
	t.Setenv("TAKO_LOG_LEVEL", "debug")
	var output bytes.Buffer
	logger, err := NewWithOutput("serve", &output)
	if err != nil {
		t.Fatal(err)
	}

	logger.Debug("diagnostic")
	if line := output.String(); !strings.Contains(line, "logging/logging_test.go:") {
		t.Errorf("debug log line does not contain caller: %q", line)
	}
}
