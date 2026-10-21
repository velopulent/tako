package app

import (
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func TestClientAddressTrustsForwardedHeadersOnlyFromProxies(t *testing.T) {
	server, _ := newTestServer(t)
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	request.RemoteAddr = "127.0.0.1:41000"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := server.clientAddress(request); got != "127.0.0.1" {
		t.Fatalf("untrusted peer forwarded header used: %s", got)
	}

	server.config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("10.0.0.0/8")}
	if got := server.clientAddress(request); got != "203.0.113.9" {
		t.Fatalf("trusted proxy header ignored: %s", got)
	}
	// A client-supplied entry on the left cannot override what the trusted
	// proxies appended on the right.
	request.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.9, 10.1.2.3")
	if got := server.clientAddress(request); got != "203.0.113.9" {
		t.Fatalf("spoofed entry won: %s", got)
	}
	request.Header.Set("X-Forwarded-For", "not-an-address")
	if got := server.clientAddress(request); got != "127.0.0.1" {
		t.Fatalf("malformed header changed the client: %s", got)
	}
}

func TestClientLimitKeyGroupsIPv6By64(t *testing.T) {
	if clientLimitKey("2001:db8:1:2:aaaa::1") != clientLimitKey("2001:db8:1:2:bbbb::9") {
		t.Fatal("addresses in one /64 got different keys")
	}
	if clientLimitKey("2001:db8:1:2::1") == clientLimitKey("2001:db8:1:3::1") {
		t.Fatal("different /64s share a key")
	}
	if clientLimitKey("192.0.2.4") != "192.0.2.4" {
		t.Fatal("IPv4 key changed")
	}
}

func TestLoginLimiterEvictsInsteadOfLockingEveryoneOut(t *testing.T) {
	limiter := newLoginLimiter(5, time.Minute)
	for index := range maxTrackedClients {
		limiter.Allow("client-" + strconv.Itoa(index))
	}
	if !limiter.Allow("newcomer") {
		t.Fatal("a full table refused a new client")
	}
	for range 5 {
		limiter.Allow("attacker")
	}
	if limiter.Allow("attacker") {
		t.Fatal("rate limit no longer applies")
	}
}

func TestOpenSignInsAreCappedPerClient(t *testing.T) {
	conversations := newSignInConversations()
	for index := range maxOpenSignInsPerClient {
		if !conversations.Allow("client") {
			t.Fatalf("sign-in %d refused early", index)
		}
		conversations.Track("conversation-"+strconv.Itoa(index), "client")
	}
	if conversations.Allow("client") {
		t.Fatal("client exceeded its open sign-ins")
	}
	if !conversations.Allow("someone-else") {
		t.Fatal("one client's open sign-ins blocked another")
	}
	conversations.Done("conversation-0")
	if !conversations.Allow("client") {
		t.Fatal("a finished sign-in still counted")
	}
}
