//go:build linux && integration

package sessiond

import (
	"bufio"
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/velopulent/tako/internal/fdpass"
	"golang.org/x/sys/unix"
)

// TestTransientServiceReceivesTheSocketAsStdio starts a real transient unit on
// the caller's user manager, which accepts the same StartTransientUnit call
// as the system manager, and talks to it over the passed socket.
func TestTransientServiceReceivesTheSocketAsStdio(t *testing.T) {
	local, peer, err := fdpass.Pair(unix.SOCK_STREAM)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := fdpass.Conn(local)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unit := "tako-transient-test-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".service"
	err = startTransientServiceOn(ctx, dbus.ConnectSessionBus, unit, "Tako transient service test", []string{"/bin/sh", "-c", "read line; echo \"echo:$line\""}, peer)
	peer.Close()
	if err != nil {
		t.Skipf("no user service manager: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || reply != "echo:hello\n" {
		t.Fatalf("service replied %q: %v", reply, err)
	}
}
