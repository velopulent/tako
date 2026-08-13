//go:build linux && integration

package auth

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// TestPAMUserSessionLifecycle is the disposable-VM seam. The VM provisions a
// dedicated PAM service and account, then supplies prompt responses as a JSON
// array through TAKO_TEST_PAM_RESPONSES. Secrets are consumed in memory and are
// never logged or written by the test.
func TestPAMUserSessionLifecycle(t *testing.T) {
	username := os.Getenv("TAKO_TEST_PAM_USER")
	encodedResponses := os.Getenv("TAKO_TEST_PAM_RESPONSES")
	if username == "" || encodedResponses == "" {
		t.Skip("set TAKO_TEST_PAM_USER and TAKO_TEST_PAM_RESPONSES in a disposable VM")
	}
	var responses []string
	if err := json.Unmarshal([]byte(encodedResponses), &responses); err != nil {
		t.Fatal("TAKO_TEST_PAM_RESPONSES must be a JSON string array")
	}
	os.Unsetenv("TAKO_TEST_PAM_RESPONSES")
	service := os.Getenv("TAKO_TEST_PAM_SERVICE")
	if service == "" {
		service = "tako"
	}
	index := 0
	session, err := (PAMAuthenticator{Service: service}).OpenSessionWithConversation(username, func(style PromptStyle, _ string) (string, error) {
		if style == PromptInfo || style == PromptError {
			return "", nil
		}
		if index >= len(responses) {
			return "", errors.New("PAM requested more responses than the VM supplied")
		}
		response := responses[index]
		responses[index] = ""
		index++
		return response, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.Identity.Username == "" || session.Close == nil {
		t.Fatalf("incomplete PAM user session: %#v", session.Identity)
	}
	session.Close()
	session.Close()
}
