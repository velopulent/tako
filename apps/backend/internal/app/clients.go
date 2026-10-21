package app

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// clientAddress is the address sign-in limits and logs attribute a request
// to. X-Forwarded-For counts only when the direct peer is a configured trusted
// proxy, and then the right-most untrusted entry is the client, since anything
// further left was supplied by the client itself.
func (server *Server) clientAddress(request *http.Request) string {
	peer, ok := parseHostAddress(request.RemoteAddr)
	if !ok {
		return request.RemoteAddr
	}
	if !server.trustedProxy(peer) {
		return peer.String()
	}
	entries := strings.Split(strings.Join(request.Header.Values("X-Forwarded-For"), ","), ",")
	client := peer
	for index := len(entries) - 1; index >= 0; index-- {
		entry := strings.TrimSpace(entries[index])
		if entry == "" {
			continue
		}
		address, ok := parseHostAddress(entry)
		if !ok {
			break
		}
		client = address
		if !server.trustedProxy(address) {
			break
		}
	}
	return client.String()
}

func (server *Server) trustedProxy(address netip.Addr) bool {
	for _, prefix := range server.config.TrustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseHostAddress(value string) (netip.Addr, bool) {
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	address, err := netip.ParseAddr(strings.Trim(value, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

// clientLimitKey groups IPv6 clients by /64, the smallest block a single
// host or customer is normally given, so rotating addresses inside it does
// not reset the limits.
func clientLimitKey(client string) string {
	address, err := netip.ParseAddr(client)
	if err != nil || address.Is4() {
		return client
	}
	return netip.PrefixFrom(address, 64).Masked().String()
}

const (
	// maxOpenSignInsPerClient caps sign-in conversations one client can keep
	// parked on unanswered prompts.
	maxOpenSignInsPerClient = 3
	signInConversationTTL   = 2*time.Minute + 15*time.Second
)

// signInConversations remembers which client started each open PAM
// conversation.
type signInConversations struct {
	mu   sync.Mutex
	open map[string]openSignIn
}

type openSignIn struct {
	client  string
	expires time.Time
}

func newSignInConversations() *signInConversations {
	return &signInConversations{open: make(map[string]openSignIn)}
}

func (conversations *signInConversations) Allow(client string) bool {
	conversations.mu.Lock()
	defer conversations.mu.Unlock()
	now := time.Now()
	count := 0
	for id, entry := range conversations.open {
		if now.After(entry.expires) {
			delete(conversations.open, id)
			continue
		}
		if entry.client == client {
			count++
		}
	}
	return count < maxOpenSignInsPerClient
}

func (conversations *signInConversations) Track(id, client string) {
	if id == "" {
		return
	}
	conversations.mu.Lock()
	conversations.open[id] = openSignIn{client: client, expires: time.Now().Add(signInConversationTTL)}
	conversations.mu.Unlock()
}

func (conversations *signInConversations) Done(id string) {
	conversations.mu.Lock()
	delete(conversations.open, id)
	conversations.mu.Unlock()
}
