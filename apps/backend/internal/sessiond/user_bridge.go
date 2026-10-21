package sessiond

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/velopulent/tako/internal/auth"
)

const (
	maxBridgeFrame = 8 << 20
	// bridgeCancelMethod asks the bridge to cancel the in-flight request with
	// the same ID. It has no response.
	bridgeCancelMethod = "$cancel"
)

// userBridgeProcess multiplexes concurrent requests over the bridge's pipes.
// A request that times out is canceled on its own; the pipes, and with them
// the user's login, stay up.
type userBridgeProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan bridgeResponse
	broken  error
	next    atomic.Uint64
	done    chan error
	exited  chan struct{}
	once    sync.Once
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

func startUserBridge(session auth.UserSession) (*userBridgeProcess, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return startUserBridgeExecutable(session, executable)
}

func startUserBridgeExecutable(session auth.UserSession, executable string) (*userBridgeProcess, error) {
	account, err := user.LookupId(strconv.Itoa(session.Identity.UID))
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, "bridge")
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	command.Stderr = os.Stderr
	command.Dir = account.HomeDir
	command.Env = bridgeEnvironment(session.Environment, account)
	credential, err := userCredential(account, session.Identity)
	if err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: credential, Setsid: true}
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, fmt.Errorf("start user bridge executable %s uid=%d gid=%d: %w", executable, session.Identity.UID, session.Identity.GID, err)
	}
	process := newUserBridgeProcess(input, output)
	process.command = command
	go func() {
		process.done <- command.Wait()
		close(process.exited)
	}()
	if err := process.ready(); err != nil {
		_ = process.Close()
		return nil, err
	}
	return process, nil
}

// newUserBridgeProcess starts the response reader for an already running
// bridge connected through input and output.
func newUserBridgeProcess(input io.WriteCloser, output io.ReadCloser) *userBridgeProcess {
	process := &userBridgeProcess{input: input, output: output, pending: make(map[string]chan bridgeResponse), done: make(chan error, 1), exited: make(chan struct{})}
	go process.readResponses(bufio.NewReader(output))
	return process
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

func (process *userBridgeProcess) Close() error {
	var closeErr error
	process.once.Do(func() {
		_ = process.input.Close()
		_ = process.output.Close()
		if process.command == nil {
			return
		}
		select {
		case err := <-process.done:
			if err != nil && !errors.Is(err, os.ErrProcessDone) {
				closeErr = err
			}
		case <-time.After(2 * time.Second):
			if process.command.Process != nil {
				_ = process.command.Process.Kill()
			}
			<-process.done
		}
	})
	return closeErr
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
