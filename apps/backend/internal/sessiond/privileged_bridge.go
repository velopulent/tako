package sessiond

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/velopulent/tako/internal/fdpass"
	"golang.org/x/sys/unix"
)

// privilegedBridge serves administrative work that must see the real host,
// such as file changes under /usr, /boot, or /tmp and SELinux or AppArmor
// remediation. It is `tako bridge --privileged`, running as root in its own
// transient service per administrative grant, so sessiond keeps its sandbox.
type privilegedBridge interface {
	bridgeCaller
	io.Closer
	alive() bool
}

type privilegedBridgeStarter func(context.Context) (privilegedBridge, error)

func newPrivilegedBridgeStarter(start serviceStarter, executable string) privilegedBridgeStarter {
	return func(ctx context.Context) (privilegedBridge, error) {
		local, peer, err := fdpass.Pair(unix.SOCK_STREAM)
		if err != nil {
			return nil, err
		}
		name, err := randomToken(12)
		if err != nil {
			local.Close()
			peer.Close()
			return nil, err
		}
		startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = start(startCtx, "tako-admin-"+name+".service", "Tako administrative bridge", []string{executable, "bridge", "--privileged"}, peer)
		cancel()
		peer.Close()
		if err != nil {
			local.Close()
			return nil, fmt.Errorf("start administrative bridge: %w", err)
		}
		conn, err := fdpass.Conn(local)
		if err != nil {
			return nil, err
		}
		process := newUserBridgeProcess(conn, conn)
		if err := process.ready(); err != nil {
			_ = process.Close()
			return nil, err
		}
		return process, nil
	}
}

func closePrivilegedBridge(bridge privilegedBridge) {
	if bridge != nil {
		_ = bridge.Close()
	}
}
