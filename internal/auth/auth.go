package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/user"
	"strconv"
	"sync"
	"time"

	"github.com/msteinert/pam/v2"
)

var (
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrServiceUnavailable   = errors.New("authentication service unavailable")
)

type Identity struct {
	Username    string `json:"username"`
	Name        string `json:"name"`
	UID         int    `json:"uid"`
	GID         int    `json:"gid"`
	BridgeToken string `json:"-"`
}

type Request struct {
	Operation string `json:"operation,omitempty"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Token     string `json:"token,omitempty"`
	Columns   uint16 `json:"columns,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
}

type Response struct {
	Identity    *Identity `json:"identity,omitempty"`
	BridgeToken string    `json:"bridgeToken,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type Authenticator interface {
	Authenticate(context.Context, string, string) (Identity, error)
}

type PAMAuthenticator struct{ Service string }

func (auth PAMAuthenticator) Authenticate(_ context.Context, username, password string) (Identity, error) {
	transaction, clear, err := auth.authenticate(username, password)
	if clear != nil {
		defer clear()
	}
	if err != nil {
		return Identity{}, err
	}
	_ = transaction
	return lookupIdentity(username)
}

// OpenSession authenticates and keeps the PAM session/credentials alive until
// the returned close function is called by the privileged session service.
func (auth PAMAuthenticator) OpenSession(username, password string) (Identity, func(), error) {
	transaction, clear, err := auth.authenticate(username, password)
	if err != nil {
		if clear != nil {
			clear()
		}
		return Identity{}, nil, err
	}
	if err := transaction.SetCred(pam.EstablishCred); err != nil {
		clear()
		return Identity{}, nil, err
	}
	if err := transaction.OpenSession(0); err != nil {
		_ = transaction.SetCred(pam.DeleteCred)
		clear()
		return Identity{}, nil, err
	}
	identity, err := lookupIdentity(username)
	clear()
	if err != nil {
		_ = transaction.CloseSession(0)
		_ = transaction.SetCred(pam.DeleteCred)
		return Identity{}, nil, err
	}
	var once sync.Once
	closeSession := func() {
		once.Do(func() {
			_ = transaction.CloseSession(0)
			_ = transaction.SetCred(pam.DeleteCred)
		})
	}
	return identity, closeSession, nil
}

func (auth PAMAuthenticator) authenticate(username, password string) (*pam.Transaction, func(), error) {
	if username == "" || password == "" {
		return nil, nil, errors.New("missing credentials")
	}
	secret := password
	clear := func() { secret = "" }
	transaction, err := pam.StartFunc(auth.Service, username, func(style pam.Style, _ string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return secret, nil
		case pam.PromptEchoOn:
			return username, nil
		default:
			return "", nil
		}
	})
	if err != nil {
		clear()
		return nil, nil, err
	}
	if err := transaction.Authenticate(0); err != nil {
		clear()
		return nil, nil, err
	}
	if err := transaction.AcctMgmt(0); err != nil {
		clear()
		return nil, nil, err
	}
	return transaction, clear, nil
}

type SocketAuthenticator struct{ Path string }

func (auth SocketAuthenticator) Authenticate(ctx context.Context, username, password string) (Identity, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", auth.Path)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: connect to %s: %v", ErrServiceUnavailable, auth.Path, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := json.NewEncoder(conn).Encode(Request{Operation: "authenticate", Username: username, Password: password}); err != nil {
		return Identity{}, fmt.Errorf("%w: send request: %v", ErrServiceUnavailable, err)
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return Identity{}, fmt.Errorf("%w: read response: %v", ErrServiceUnavailable, err)
	}
	if response.Identity == nil {
		if response.Error == "authentication-failed" {
			return Identity{}, ErrAuthenticationFailed
		}
		if response.Error == "" {
			response.Error = "empty response"
		}
		return Identity{}, fmt.Errorf("%w: %s", ErrServiceUnavailable, response.Error)
	}
	response.Identity.BridgeToken = response.BridgeToken
	return *response.Identity, nil
}

func OpenTerminal(ctx context.Context, path, token string, columns, rows uint16) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(Request{Operation: "terminal", Token: token, Columns: columns, Rows: rows}); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func ResizeTerminal(ctx context.Context, path, token string, columns, rows uint16) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(Request{Operation: "resize-terminal", Token: token, Columns: columns, Rows: rows}); err != nil {
		return err
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<10)).Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

type DevelopmentAuthenticator struct{}

func (DevelopmentAuthenticator) Authenticate(_ context.Context, username, _ string) (Identity, error) {
	if username == "" {
		current, err := user.Current()
		if err != nil {
			return Identity{}, err
		}
		username = current.Username
	}
	return lookupIdentity(username)
}

func lookupIdentity(username string) (Identity, error) {
	account, err := user.Lookup(username)
	if err != nil {
		return Identity{}, err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return Identity{}, err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return Identity{}, err
	}
	name := account.Name
	if name == "" {
		name = account.Username
	}
	return Identity{Username: account.Username, Name: name, UID: uid, GID: gid}, nil
}
