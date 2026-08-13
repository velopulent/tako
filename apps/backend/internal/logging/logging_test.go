package logging

import "testing"

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
