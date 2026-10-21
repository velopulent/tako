package sessiond

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/fdpass"
	"golang.org/x/sys/unix"
)

// serveBridgePing answers bridge frames on conn with an "ok" payload.
func serveBridgePing(conn io.ReadWriteCloser) {
	defer conn.Close()
	for {
		var size uint32
		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		size = uint32(header[0])<<24 | uint32(header[1])<<16 | uint32(header[2])<<8 | uint32(header[3])
		payload := make([]byte, size)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
		var request bridgeRequest
		_ = json.Unmarshal(payload, &request)
		response, _ := json.Marshal(map[string]any{"id": request.ID, "payload": map[string]bool{"ok": true}})
		frame := append([]byte{byte(len(response) >> 24), byte(len(response) >> 16), byte(len(response) >> 8), byte(len(response))}, response...)
		if _, err := conn.Write(frame); err != nil {
			return
		}
	}
}

// helperStarter starts script as the helper on the control socket sessiond
// passed, standing in for systemd.
func helperStarter(t *testing.T, script func(control *helperControl)) serviceStarter {
	t.Helper()
	return func(_ context.Context, unit, _ string, argv []string, stdio *os.File) error {
		if !strings.HasPrefix(unit, "tako-login-") || argv[len(argv)-1] != "session-helper" {
			t.Errorf("unexpected helper unit %q %v", unit, argv)
		}
		duplicate, err := unix.Dup(int(stdio.Fd()))
		if err != nil {
			return err
		}
		control, err := fdpass.Conn(os.NewFile(uintptr(duplicate), "fake-helper"))
		if err != nil {
			return err
		}
		go func() {
			defer control.Close()
			script(&helperControl{t: t, send: func(message helperMessage, files ...*os.File) error {
				return sendHelperMessage(control, message, files...)
			}, receive: func() (helperMessage, error) {
				message, files, err := receiveHelperMessage(control)
				fdpass.CloseAll(files)
				return message, err
			}})
		}()
		return nil
	}
}

type helperControl struct {
	t       *testing.T
	send    func(helperMessage, ...*os.File) error
	receive func() (helperMessage, error)
}

func TestSignInRunsThroughTheHelperAndHandsOverTheBridge(t *testing.T) {
	helperDone := make(chan struct{})
	release := make(chan struct{})
	opener := helperOpener{executable: "/usr/bin/tako", start: helperStarter(t, func(control *helperControl) {
		defer close(helperDone)
		start, err := control.receive()
		if err != nil || start.Username != "octopus" || start.RemoteHost != "192.0.2.4" {
			t.Errorf("unexpected start %+v %v", start, err)
			return
		}
		if err := control.send(helperMessage{Type: "prompt", Style: auth.PromptHidden, Message: "Password:"}); err != nil {
			t.Error(err)
			return
		}
		answer, err := control.receive()
		if err != nil || answer.Type != "answer" || answer.Value != "secret" {
			t.Errorf("unexpected answer %+v %v", answer, err)
			return
		}
		rpc, rpcPeer, _ := fdpass.Pair(unix.SOCK_STREAM)
		terminals, terminalsPeer, _ := fdpass.Pair(unix.SOCK_SEQPACKET)
		defer terminalsPeer.Close()
		go serveBridgePing(rpcPeer)
		identity := auth.Identity{Username: "octopus", UID: 1000, GID: 1000}
		if err := control.send(helperMessage{Type: "opened", Identity: &identity}, rpc, terminals); err != nil {
			t.Error(err)
		}
		rpc.Close()
		terminals.Close()
		<-release
	})}

	var prompts []string
	session, err := opener.OpenSessionWithConversation(context.Background(), "octopus", "192.0.2.4", func(style auth.PromptStyle, message string) (string, error) {
		prompts = append(prompts, message)
		return "secret", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.Identity.Username != "octopus" || len(prompts) != 1 || prompts[0] != "Password:" {
		t.Fatalf("unexpected session %+v prompts %v", session.Identity, prompts)
	}
	bridge, ok := session.Bridge.(bridgeCaller)
	if !ok {
		t.Fatal("session has no callable bridge")
	}
	if raw, err := bridge.call(context.Background(), "ping", nil); err != nil || !bytes.Contains(raw, []byte("ok")) {
		t.Fatalf("handed-over bridge did not answer: %s %v", raw, err)
	}
	select {
	case <-session.Done:
		t.Fatal("session ended while the helper was still running")
	default:
	}
	close(release)
	<-helperDone
	select {
	case <-session.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("session did not end when the helper exited")
	}
}

func TestSignInHelperFailuresMapToStableErrors(t *testing.T) {
	for _, test := range []struct {
		code string
		want error
	}{
		{"authentication-failed", auth.ErrAuthenticationFailed},
		{"session-failed", errSessionFailed},
	} {
		opener := helperOpener{executable: "/usr/bin/tako", start: helperStarter(t, func(control *helperControl) {
			_, _ = control.receive()
			_ = control.send(helperMessage{Type: "failed", Error: test.code})
		})}
		_, err := opener.OpenSessionWithConversation(context.Background(), "octopus", "", func(auth.PromptStyle, string) (string, error) { return "", nil })
		if !errors.Is(err, test.want) {
			t.Fatalf("%s mapped to %v", test.code, err)
		}
	}
}

func TestAbandonedSignInClosesTheHelper(t *testing.T) {
	helperGone := make(chan struct{})
	opener := helperOpener{executable: "/usr/bin/tako", start: helperStarter(t, func(control *helperControl) {
		defer close(helperGone)
		_, _ = control.receive()
		_ = control.send(helperMessage{Type: "prompt", Style: auth.PromptHidden, Message: "Password:"})
		if _, err := control.receive(); err == nil {
			t.Error("helper got an answer after the sign-in was abandoned")
		}
	})}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := opener.OpenSessionWithConversation(ctx, "octopus", "", func(auth.PromptStyle, string) (string, error) {
		cancel()
		return "", ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("abandoned sign-in error = %v", err)
	}
	select {
	case <-helperGone:
	case <-time.After(2 * time.Second):
		t.Fatal("helper kept waiting after the sign-in was abandoned")
	}
}
