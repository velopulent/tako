//go:build linux && integration

package auth

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestPAMPasswordLifecycle is opt-in for a disposable VM. The harness must
// provide a fixture account and two temporary passwords. Values are consumed
// in memory, removed from the environment, and are never written to output.
func TestPAMPasswordLifecycle(t *testing.T) {
	username := os.Getenv("TAKO_TEST_PASSWORD_USER")
	oldPassword := os.Getenv("TAKO_TEST_PASSWORD_OLD")
	newPassword := os.Getenv("TAKO_TEST_PASSWORD_NEW")
	service := os.Getenv("TAKO_TEST_PASSWORD_SERVICE")
	os.Unsetenv("TAKO_TEST_PASSWORD_USER")
	os.Unsetenv("TAKO_TEST_PASSWORD_OLD")
	os.Unsetenv("TAKO_TEST_PASSWORD_NEW")
	os.Unsetenv("TAKO_TEST_PASSWORD_SERVICE")
	if username == "" || oldPassword == "" || newPassword == "" {
		t.Skip("set disposable VM password fixture environment to run PAM password integration")
	}
	if service == "" {
		service = "passwd"
	}
	authenticator := PAMAuthenticator{Service: service, PasswordService: service}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := authenticator.ChangePassword(ctx, username, oldPassword, newPassword); err != nil {
		t.Fatal("PAM password change failed")
	}
	if err := authenticator.ChangePassword(ctx, username, newPassword, oldPassword); err != nil {
		t.Fatal("PAM password restore failed")
	}
}
