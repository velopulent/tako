package sessiond

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/user"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/fdpass"
	"golang.org/x/sys/unix"
)

const (
	maxBridgeFrame = 8 << 20
	// bridgeCancelMethod asks the bridge to cancel the in-flight request with
	// the same ID. It has no response.
	bridgeCancelMethod = "$cancel"
)

// userBridgeProcess is sessiond's connection to a bridge. Requests are
// multiplexed over one socket: a request that times out is canceled on its
// own, and the connection, with the user's login, stays up. The process itself
// belongs to the sign-in helper (user bridges) or to its own transient service
// (the administrative bridge), never to sessiond.
type userBridgeProcess struct {
	input   io.WriteCloser
	output  io.ReadCloser
	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan bridgeResponse
	broken  error
	next    atomic.Uint64
	once    sync.Once
	// terminals hands terminal sockets to a user bridge; nil for the
	// administrative bridge.
	terminals *net.UnixConn
}

type bridgeRequest struct {
	ID      string `json:"id"`
	Method  string `json:"method"`
	Payload any    `json:"payload,omitempty"`
}

type bridgeResponse struct {
	payload json.RawMessage
	err     error
}

// newUserBridgeProcess starts the response reader for a bridge connected
// through input and output.
func newUserBridgeProcess(input io.WriteCloser, output io.ReadCloser) *userBridgeProcess {
	process := &userBridgeProcess{input: input, output: output, pending: make(map[string]chan bridgeResponse)}
	go process.readResponses(bufio.NewReader(output))
	return process
}

// alive reports whether the bridge still answers requests.
func (process *userBridgeProcess) alive() bool {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.broken == nil
}

// openTerminal asks the bridge for a new login shell and returns the socket
// carrying its raw terminal bytes. The bridge runs the shell in the user's
// own session; closing the socket hangs it up.
func (process *userBridgeProcess) openTerminal(size auth.TerminalSize) (net.Conn, error) {
	if process.terminals == nil {
		return nil, errors.New("this bridge has no terminals")
	}
	local, peer, err := fdpass.Pair(unix.SOCK_STREAM)
	if err != nil {
		return nil, err
	}
	defer peer.Close()
	header, err := json.Marshal(size)
	if err == nil {
		err = fdpass.Send(process.terminals, header, peer)
	}
	if err != nil {
		local.Close()
		return nil, err
	}
	return fdpass.Conn(local)
}

func (process *userBridgeProcess) ready() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := process.call(ctx, "ping", nil)
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("user bridge readiness check timed out")
	}
	if err != nil {
		return errors.New("user bridge readiness check failed")
	}
	return nil
}

// bridgeCaller is the user-lane RPC surface. The per-login bridge implements
// it; tests substitute recorders.
type bridgeCaller interface {
	call(ctx context.Context, method string, payload any) (json.RawMessage, error)
}

// callBridge runs one bridge method and decodes its result.
func callBridge[T any](ctx context.Context, bridge bridgeCaller, method string, payload any) (T, error) {
	var result T
	if bridge == nil {
		return result, auth.Code("user-bridge-unavailable")
	}
	raw, err := bridge.call(ctx, method, payload)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, errors.New("invalid user bridge result")
	}
	return result, nil
}

func (process *userBridgeProcess) call(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	id := strconv.FormatUint(process.next.Add(1), 36)
	encoded, err := json.Marshal(bridgeRequest{ID: id, Method: method, Payload: payload})
	if err != nil {
		return nil, err
	}
	waiter := make(chan bridgeResponse, 1)
	process.mu.Lock()
	if process.broken != nil {
		err := process.broken
		process.mu.Unlock()
		return nil, err
	}
	process.pending[id] = waiter
	process.mu.Unlock()
	if err := process.writeFrame(encoded); err != nil {
		process.forget(id)
		return nil, err
	}
	select {
	case response := <-waiter:
		return response.payload, response.err
	case <-ctx.Done():
		process.forget(id)
		go func() {
			if cancel, err := json.Marshal(bridgeRequest{ID: id, Method: bridgeCancelMethod}); err == nil {
				_ = process.writeFrame(cancel)
			}
		}()
		return nil, ctx.Err()
	}
}

func (process *userBridgeProcess) forget(id string) {
	process.mu.Lock()
	delete(process.pending, id)
	process.mu.Unlock()
}

func (process *userBridgeProcess) writeFrame(encoded []byte) error {
	if len(encoded) > maxBridgeFrame {
		return fmt.Errorf("user bridge request exceeds %d bytes", maxBridgeFrame)
	}
	process.writeMu.Lock()
	defer process.writeMu.Unlock()
	if err := binary.Write(process.input, binary.BigEndian, uint32(len(encoded))); err != nil {
		return err
	}
	_, err := process.input.Write(encoded)
	return err
}

// readResponses routes each response to its waiting call. Responses for
// canceled requests are dropped. When the bridge stops, every waiting call
// fails and later calls fail immediately.
func (process *userBridgeProcess) readResponses(reader *bufio.Reader) {
	for {
		var size uint32
		err := binary.Read(reader, binary.BigEndian, &size)
		if err == nil && (size == 0 || size > maxBridgeFrame) {
			err = fmt.Errorf("invalid user bridge response frame: %d", size)
		}
		var encoded []byte
		if err == nil {
			encoded = make([]byte, size)
			_, err = io.ReadFull(reader, encoded)
		}
		var response struct {
			ID      string          `json:"id"`
			Error   string          `json:"error"`
			Payload json.RawMessage `json:"payload"`
		}
		if err == nil {
			err = json.Unmarshal(encoded, &response)
		}
		if err != nil {
			process.fail(fmt.Errorf("user bridge stopped: %w", err))
			return
		}
		process.mu.Lock()
		waiter, ok := process.pending[response.ID]
		delete(process.pending, response.ID)
		process.mu.Unlock()
		if !ok {
			continue
		}
		result := bridgeResponse{payload: response.Payload}
		if response.Error != "" {
			result.err = auth.Code(response.Error)
		}
		waiter <- result
	}
}

func (process *userBridgeProcess) fail(err error) {
	process.mu.Lock()
	if process.broken == nil {
		process.broken = err
	}
	pending := process.pending
	process.pending = make(map[string]chan bridgeResponse)
	process.mu.Unlock()
	for _, waiter := range pending {
		waiter <- bridgeResponse{err: err}
	}
}

// Close disconnects from the bridge, which then exits on its own.
func (process *userBridgeProcess) Close() error {
	process.once.Do(func() {
		_ = process.input.Close()
		_ = process.output.Close()
		if process.terminals != nil {
			_ = process.terminals.Close()
		}
	})
	return nil
}

func userCredential(account *user.User, identity auth.Identity) (*syscall.Credential, error) {
	credential := &syscall.Credential{Uid: uint32(identity.UID), Gid: uint32(identity.GID)}
	groupIDs, err := account.GroupIds()
	if err != nil {
		return nil, err
	}
	for _, groupID := range groupIDs {
		parsed, err := strconv.ParseUint(groupID, 10, 32)
		if err != nil {
			return nil, err
		}
		credential.Groups = append(credential.Groups, uint32(parsed))
	}
	return credential, nil
}

func bridgeEnvironment(pamEnvironment map[string]string, account *user.User) []string {
	environment := make(map[string]string, len(pamEnvironment)+6)
	for key, value := range pamEnvironment {
		if key != "" {
			environment[key] = value
		}
	}
	environment["HOME"] = account.HomeDir
	environment["USER"] = account.Username
	environment["LOGNAME"] = account.Username
	if environment["PATH"] == "" {
		environment["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+environment[key])
	}
	return result
}
