package sessiond

import (
	"bufio"
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
)

type userBridgeProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	done    chan error
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
		return nil, err
	}
	process := &userBridgeProcess{command: command, input: input, output: output, done: make(chan error, 1)}
	go func() { process.done <- command.Wait() }()
	if err := process.ready(); err != nil {
		_ = process.Close()
		return nil, err
	}
	return process, nil
}

func (process *userBridgeProcess) ready() error {
	result := make(chan error, 1)
	go func() {
		request := []byte(`{"id":"ready","method":"ping"}`)
		if err := binary.Write(process.input, binary.BigEndian, uint32(len(request))); err != nil {
			result <- err
			return
		}
		if _, err := process.input.Write(request); err != nil {
			result <- err
			return
		}
		reader := bufio.NewReader(process.output)
		var size uint32
		if err := binary.Read(reader, binary.BigEndian, &size); err != nil {
			result <- err
			return
		}
		if size == 0 || size > 16<<10 {
			result <- fmt.Errorf("invalid user bridge readiness frame: %d", size)
			return
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(reader, payload); err != nil {
			result <- err
			return
		}
		var response struct {
			ID    string `json:"id"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(payload, &response); err != nil {
			result <- err
			return
		}
		if response.ID != "ready" || response.Error != "" {
			result <- errors.New("user bridge readiness check failed")
			return
		}
		result <- nil
	}()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		return errors.New("user bridge readiness check timed out")
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
