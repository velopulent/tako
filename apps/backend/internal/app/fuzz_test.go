package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func FuzzDecodeUpdateOperation(f *testing.F) {
	f.Add(`{"expectedFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","confirmed":true}`, false)
	f.Add(`{"unexpected":true}`, true)
	f.Fuzz(func(_ *testing.T, payload string, preview bool) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/updates/preview", strings.NewReader(payload))
		recorder := httptest.NewRecorder()
		decodeUpdateOperation(recorder, request, preview)
	})
}
