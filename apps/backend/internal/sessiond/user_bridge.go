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
	"syscall"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/platform"
)

type userBridgeProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	reader  *bufio.Reader
	rpcMu   sync.Mutex
	done    chan error
	exited  chan struct{}
	once    sync.Once
}

func parseID(value string) (int, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return int(parsed), err
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
	process := &userBridgeProcess{command: command, input: input, output: output, reader: bufio.NewReader(output), done: make(chan error, 1), exited: make(chan struct{})}
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

func (process *userBridgeProcess) ready() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := process.call(ctx, "ready", "ping", nil)
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("user bridge readiness check timed out")
	}
	if err != nil {
		return errors.New("user bridge readiness check failed")
	}
	return nil
}

func (process *userBridgeProcess) applyTimer(ctx context.Context, operation platform.TimerOperation) (platform.TimerState, error) {
	payload, err := process.call(ctx, "timer", "timer.apply", operation)
	if err != nil {
		return platform.TimerState{}, err
	}
	var state platform.TimerState
	if err := json.Unmarshal(payload, &state); err != nil {
		return platform.TimerState{}, err
	}
	return state, nil
}

func (process *userBridgeProcess) applyOverride(ctx context.Context, operation platform.OverrideOperation) (platform.OverrideState, error) {
	payload, err := process.call(ctx, "override", "override.apply", operation)
	if err != nil {
		return platform.OverrideState{}, err
	}
	var state platform.OverrideState
	if err := json.Unmarshal(payload, &state); err != nil {
		return platform.OverrideState{}, err
	}
	return state, nil
}

func (process *userBridgeProcess) applyFile(ctx context.Context, operation platform.FileOperation) (platform.FileResult, error) {
	payload, err := process.call(ctx, "file", "file.apply", operation)
	if err != nil {
		return platform.FileResult{}, err
	}
	var result platform.FileResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return platform.FileResult{}, err
	}
	return result, nil
}

func (process *userBridgeProcess) call(ctx context.Context, id, method string, payload any) (json.RawMessage, error) {
	result := make(chan struct {
		payload json.RawMessage
		err     error
	}, 1)
	go func() {
		process.rpcMu.Lock()
		defer process.rpcMu.Unlock()
		request := struct {
			ID      string `json:"id"`
			Method  string `json:"method"`
			Payload any    `json:"payload,omitempty"`
		}{ID: id, Method: method, Payload: payload}
		encoded, err := json.Marshal(request)
		if err == nil {
			err = binary.Write(process.input, binary.BigEndian, uint32(len(encoded)))
		}
		if err == nil {
			_, err = process.input.Write(encoded)
		}
		if err != nil {
			result <- struct {
				payload json.RawMessage
				err     error
			}{err: err}
			return
		}
		var size uint32
		if err = binary.Read(process.reader, binary.BigEndian, &size); err == nil && (size == 0 || size > 8<<20) {
			err = fmt.Errorf("invalid user bridge response frame: %d", size)
		}
		if err != nil {
			result <- struct {
				payload json.RawMessage
				err     error
			}{err: err}
			return
		}
		encoded = make([]byte, size)
		if _, err = io.ReadFull(process.reader, encoded); err != nil {
			result <- struct {
				payload json.RawMessage
				err     error
			}{err: err}
			return
		}
		var response struct {
			ID      string          `json:"id"`
			Error   string          `json:"error"`
			Payload json.RawMessage `json:"payload"`
		}
		if err = json.Unmarshal(encoded, &response); err == nil && response.ID != id {
			err = errors.New("user bridge response id mismatch")
		}
		if err == nil && response.Error != "" {
			err = errors.New(response.Error)
		}
		result <- struct {
			payload json.RawMessage
			err     error
		}{payload: response.Payload, err: err}
	}()
	select {
	case response := <-result:
		return response.payload, response.err
	case <-ctx.Done():
		_ = process.input.Close()
		_ = process.output.Close()
		return nil, ctx.Err()
	}
}

func (process *userBridgeProcess) Close() error {
	var closeErr error
	process.once.Do(func() {
		_ = process.input.Close()
		_ = process.output.Close()
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
