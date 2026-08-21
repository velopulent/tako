package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maxAuthorizedKeysBytes = 1 << 20
	maxAuthorizedKeyLine   = 16 << 10
	maxAuthorizedKeys      = 2048
	maxAuthorizedKeyField  = 12 << 10
	maxLocalPasswdBytes    = 1 << 20
)

var (
	ErrInvalidSSHKeyOperation = errors.New("invalid SSH key operation")
	ErrSSHKeyConflict         = errors.New("SSH keys changed concurrently")
	ErrSSHKeyNotFound         = errors.New("SSH key not found")
	ErrSSHKeyReadOnly         = errors.New("SSH key target is read-only")
	ErrSSHKeyUnauthorized     = errors.New("SSH key operation is unauthorized")
	ErrSSHKeyProtected        = errors.New("SSH key path is protected")
	ErrSSHKeyUnavailable      = errors.New("SSH key service unavailable")
	ErrSSHKeyVerification     = errors.New("SSH key operation could not be verified")
)

type SSHKeyOperation struct {
	Action              string `json:"action"`
	Username            string `json:"username"`
	Key                 string `json:"key,omitempty"`
	Fingerprint         string `json:"fingerprint,omitempty"`
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`
	Confirmation        string `json:"confirmation,omitempty"`
	Preview             bool   `json:"preview,omitempty"`
}

func (operation *SSHKeyOperation) UnmarshalJSON(payload []byte) error {
	type plain SSHKeyOperation
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value plain
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing SSH key operation data")
	}
	*operation = SSHKeyOperation(value)
	return nil
}

type SSHKey struct {
	Fingerprint string `json:"fingerprint"`
	Type        string `json:"type"`
	Comment     string `json:"comment,omitempty"`
	Line        string `json:"line"`
}

type SSHKeyState struct {
	Username    string   `json:"username"`
	Path        string   `json:"path"`
	Fingerprint string   `json:"fingerprint"`
	Keys        []SSHKey `json:"keys"`
	Writable    bool     `json:"writable"`
	Authority   string   `json:"authority"`
	Reason      string   `json:"reason,omitempty"`
	rawLines    []string
}

func (state SSHKeyState) MarshalJSON() ([]byte, error) {
	if state.Keys == nil {
		state.Keys = []SSHKey{}
	}
	type plain SSHKeyState
	return json.Marshal((plain)(state))
}

func (state *SSHKeyState) UnmarshalJSON(payload []byte) error {
	type plain SSHKeyState
	var raw plain
	if err := json.Unmarshal(payload, &raw); err != nil {
		return err
	}
	*state = SSHKeyState(raw)
	if state.Keys == nil {
		state.Keys = []SSHKey{}
	}
	return nil
}

type SSHKeyPreview struct {
	Action               string      `json:"action"`
	Username             string      `json:"username"`
	Current              SSHKeyState `json:"current"`
	Changes              []string    `json:"changes"`
	Warnings             []string    `json:"warnings"`
	Stale                bool        `json:"stale"`
	Allowed              bool        `json:"allowed"`
	Reason               string      `json:"reason,omitempty"`
	RequiresConfirmation bool        `json:"requiresConfirmation"`
}

func ValidateSSHKeyOperation(operation SSHKeyOperation) error {
	if !validSSHUsername(operation.Username) || len(operation.Username) > 32 {
		return ErrInvalidSSHKeyOperation
	}
	if len(operation.Key) > maxAuthorizedKeyField || len(operation.Confirmation) > 128 || len(operation.ExpectedFingerprint) > 64 || len(operation.Fingerprint) > 64 || strings.ContainsAny(operation.Key+operation.Confirmation, "\x00\r\n") {
		return ErrInvalidSSHKeyOperation
	}
	for _, fingerprint := range []string{operation.Fingerprint, operation.ExpectedFingerprint} {
		if fingerprint == "" {
			continue
		}
		if len(fingerprint) != sha256.Size*2 {
			return ErrInvalidSSHKeyOperation
		}
		if _, err := hex.DecodeString(fingerprint); err != nil {
			return ErrInvalidSSHKeyOperation
		}
	}
	switch operation.Action {
	case "list":
		if operation.Key != "" || operation.Fingerprint != "" || operation.ExpectedFingerprint != "" || operation.Confirmation != "" || !operation.Preview {
			return ErrInvalidSSHKeyOperation
		}
	case "add":
		if operation.Key == "" || operation.Fingerprint != "" || (!operation.Preview && operation.ExpectedFingerprint == "") {
			return ErrInvalidSSHKeyOperation
		}
		if operation.Confirmation != "" {
			return ErrInvalidSSHKeyOperation
		}
	case "remove":
		if operation.Fingerprint == "" || operation.Key != "" || (!operation.Preview && operation.ExpectedFingerprint == "") {
			return ErrInvalidSSHKeyOperation
		}
		if !operation.Preview && operation.Confirmation != "REMOVE KEY "+operation.Fingerprint {
			return ErrInvalidSSHKeyOperation
		}
	default:
		return ErrInvalidSSHKeyOperation
	}
	if operation.Key != "" {
		if _, err := parseAuthorizedKeyLine(operation.Key); err != nil {
			return ErrInvalidSSHKeyOperation
		}
	}
	return nil
}

func validSSHUsername(username string) bool {
	if username == "" || username == "." || username == ".." {
		return false
	}
	for index, character := range username {
		if index == 0 {
			if (character < 'a' || character > 'z') && character != '_' {
				return false
			}
			continue
		}
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '$' {
			return false
		}
	}
	return true
}

func parseAuthorizedKeyLine(line string) (SSHKey, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || len(line) > maxAuthorizedKeyLine {
		return SSHKey{}, ErrInvalidSSHKeyOperation
	}
	fields := strings.Fields(line)
	keyIndex := -1
	for index, field := range fields {
		if !validSSHKeyType(field) || index+1 >= len(fields) {
			continue
		}
		candidate, decodeErr := decodeSSHKeyBlob(fields[index+1])
		if decodeErr == nil && sshKeyBlobType(candidate) == field {
			keyIndex = index
			break
		}
	}
	if keyIndex < 0 || keyIndex+1 >= len(fields) {
		return SSHKey{}, ErrInvalidSSHKeyOperation
	}
	encoded := fields[keyIndex+1]
	if len(encoded) > maxAuthorizedKeyField {
		return SSHKey{}, ErrInvalidSSHKeyOperation
	}
	blob, err := decodeSSHKeyBlob(encoded)
	if err != nil {
		return SSHKey{}, ErrInvalidSSHKeyOperation
	}
	if sshKeyBlobType(blob) != fields[keyIndex] {
		return SSHKey{}, ErrInvalidSSHKeyOperation
	}
	hash := sha256.Sum256(blob)
	comment := ""
	if keyIndex+2 < len(fields) {
		comment = strings.Join(fields[keyIndex+2:], " ")
	}
	return SSHKey{Fingerprint: hex.EncodeToString(hash[:]), Type: fields[keyIndex], Comment: comment, Line: line}, nil
}

func decodeSSHKeyBlob(encoded string) ([]byte, error) {
	if len(encoded) > maxAuthorizedKeyField {
		return nil, ErrInvalidSSHKeyOperation
	}
	blob, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		blob, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil || len(blob) == 0 || len(blob) > maxAuthorizedKeyField {
		return nil, ErrInvalidSSHKeyOperation
	}
	return blob, nil
}

func sshKeyBlobType(blob []byte) string {
	if len(blob) < 4 {
		return ""
	}
	typeLength := int(binary.BigEndian.Uint32(blob[:4]))
	if typeLength <= 0 || typeLength > len(blob)-4 || len(blob) == 4+typeLength {
		return ""
	}
	return string(blob[4 : 4+typeLength])
}

func validSSHKeyType(value string) bool {
	if value == "ssh-dss" || value == "" || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	base := value
	if strings.HasSuffix(base, "-cert-v01@openssh.com") {
		base = strings.TrimSuffix(base, "-cert-v01@openssh.com")
	}
	switch base {
	case "ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com":
		return true
	default:
		return false
	}
}

type sshKeyAccount struct {
	username string
	home     string
	uid      int
	gid      int
}

func currentSSHKeyAccount(username string) (sshKeyAccount, error) {
	if !localPasswdName(username) {
		return sshKeyAccount{}, ErrSSHKeyReadOnly
	}
	account, err := user.Lookup(username)
	if err != nil {
		return sshKeyAccount{}, fmt.Errorf("%w: lookup user", ErrSSHKeyUnavailable)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return sshKeyAccount{}, fmt.Errorf("%w: invalid uid", ErrSSHKeyUnavailable)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return sshKeyAccount{}, fmt.Errorf("%w: invalid gid", ErrSSHKeyUnavailable)
	}
	if !filepath.IsAbs(account.HomeDir) || filepath.Clean(account.HomeDir) != account.HomeDir {
		return sshKeyAccount{}, ErrSSHKeyProtected
	}
	return sshKeyAccount{username: username, home: account.HomeDir, uid: uid, gid: gid}, nil
}

type sshKeyDependencies struct {
	lookup func(string) (sshKeyAccount, error)
	path   func(sshKeyAccount) (string, error)
}

func systemSSHKeyDependencies() sshKeyDependencies {
	return sshKeyDependencies{lookup: currentSSHKeyAccount, path: authorizedKeysPath}
}

func localPasswdName(username string) bool {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return false
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxLocalPasswdBytes+1))
	if err != nil || len(payload) > maxLocalPasswdBytes {
		return false
	}
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.SplitN(line, ":", 2)
		if len(fields) == 2 && fields[0] == username {
			return true
		}
	}
	return false
}

func authorizedKeysPath(account sshKeyAccount) (string, error) {
	homeInfo, err := os.Lstat(account.home)
	if err != nil || !homeInfo.IsDir() || homeInfo.Mode()&os.ModeSymlink != 0 {
		return "", ErrSSHKeyProtected
	}
	sshDirectory := filepath.Join(account.home, ".ssh")
	if info, statErr := os.Lstat(sshDirectory); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", ErrSSHKeyProtected
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("%w: inspect .ssh", ErrSSHKeyUnavailable)
	}
	path := filepath.Join(sshDirectory, "authorized_keys")
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", ErrSSHKeyProtected
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("%w: inspect authorized_keys", ErrSSHKeyUnavailable)
	}
	return path, nil
}

func readSSHKeyState(ctx context.Context, operation SSHKeyOperation, actor string, administrative bool) (SSHKeyState, error) {
	return readSSHKeyStateWithDependencies(ctx, operation, actor, administrative, systemSSHKeyDependencies())
}

func readSSHKeyStateWithDependencies(ctx context.Context, operation SSHKeyOperation, actor string, administrative bool, dependencies sshKeyDependencies) (SSHKeyState, error) {
	if err := ctx.Err(); err != nil {
		return SSHKeyState{}, err
	}
	if !administrative && actor != operation.Username {
		return SSHKeyState{}, ErrSSHKeyUnauthorized
	}
	account, err := dependencies.lookup(operation.Username)
	if err != nil {
		return SSHKeyState{}, err
	}
	path, err := dependencies.path(account)
	if err != nil {
		return SSHKeyState{}, err
	}
	state := SSHKeyState{Username: operation.Username, Path: path, Authority: "user", Writable: true, Keys: []SSHKey{}}
	if administrative {
		state.Authority = "administrative"
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		state.Fingerprint = fingerprintSSHKeyFile(nil)
		if state.Keys == nil {
			state.Keys = []SSHKey{}
		}
		return state, nil
	}
	if err != nil {
		return SSHKeyState{}, fmt.Errorf("%w: read authorized_keys", ErrSSHKeyUnavailable)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAuthorizedKeysBytes+1))
	if err != nil || len(data) > maxAuthorizedKeysBytes {
		return SSHKeyState{}, fmt.Errorf("%w: authorized_keys exceeds limit", ErrSSHKeyUnavailable)
	}
	state.Fingerprint = fingerprintSSHKeyFile(data)
	rawLines := strings.Split(string(data), "\n")
	for index, rawLine := range rawLines {
		if index == len(rawLines)-1 && rawLine == "" {
			continue
		}
		state.rawLines = append(state.rawLines, rawLine)
		if strings.TrimSpace(rawLine) == "" || strings.HasPrefix(strings.TrimSpace(rawLine), "#") {
			continue
		}
		if len(state.Keys) >= maxAuthorizedKeys {
			return SSHKeyState{}, fmt.Errorf("%w: too many keys", ErrSSHKeyUnavailable)
		}
		key, parseErr := parseAuthorizedKeyLine(rawLine)
		if parseErr != nil {
			return SSHKeyState{}, fmt.Errorf("%w: invalid authorized_keys entry", ErrSSHKeyUnavailable)
		}
		state.Keys = append(state.Keys, key)
	}
	if state.Keys == nil {
		state.Keys = []SSHKey{}
	}
	return state, nil
}

func fingerprintSSHKeyFile(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func PreviewSSHKeys(ctx context.Context, operation SSHKeyOperation, actor string, administrative bool) (SSHKeyPreview, error) {
	return PreviewSSHKeysWithDependencies(ctx, operation, actor, administrative, systemSSHKeyDependencies())
}

func PreviewSSHKeysWithDependencies(ctx context.Context, operation SSHKeyOperation, actor string, administrative bool, dependencies sshKeyDependencies) (SSHKeyPreview, error) {
	if err := ValidateSSHKeyOperation(operation); err != nil {
		return SSHKeyPreview{}, err
	}
	current, err := readSSHKeyStateWithDependencies(ctx, operation, actor, administrative, dependencies)
	if err != nil {
		return SSHKeyPreview{}, err
	}
	preview := SSHKeyPreview{Action: operation.Action, Username: operation.Username, Current: current, Changes: []string{}, Warnings: []string{}, Allowed: true, RequiresConfirmation: operation.Action == "remove"}
	if operation.Action == "list" {
		return preview, nil
	}
	if operation.ExpectedFingerprint != "" && operation.ExpectedFingerprint != current.Fingerprint {
		preview.Stale = true
		preview.Allowed = false
		preview.Reason = "authorized_keys changed; refresh before applying"
		return preview, nil
	}
	if operation.Action == "add" {
		key, _ := parseAuthorizedKeyLine(operation.Key)
		for _, existing := range current.Keys {
			if existing.Fingerprint == key.Fingerprint {
				preview.Allowed = false
				preview.Reason = "this key is already authorized"
				return preview, nil
			}
		}
		preview.Changes = []string{"add " + key.Type + " key " + key.Fingerprint}
	} else {
		found := false
		for _, existing := range current.Keys {
			if existing.Fingerprint == operation.Fingerprint {
				found = true
				break
			}
		}
		if !found {
			preview.Allowed = false
			preview.Reason = "the selected key no longer exists"
			return preview, nil
		}
		preview.Changes = []string{"remove key " + operation.Fingerprint}
	}
	return preview, nil
}

func ApplySSHKeys(ctx context.Context, operation SSHKeyOperation, actor string, administrative bool) (SSHKeyState, error) {
	return applySSHKeysWithDependencies(ctx, operation, actor, administrative, systemSSHKeyDependencies())
}

func applySSHKeysWithDependencies(ctx context.Context, operation SSHKeyOperation, actor string, administrative bool, dependencies sshKeyDependencies) (SSHKeyState, error) {
	if err := ValidateSSHKeyOperation(operation); err != nil || operation.Preview {
		return SSHKeyState{}, ErrInvalidSSHKeyOperation
	}
	current, err := readSSHKeyStateWithDependencies(ctx, operation, actor, administrative, dependencies)
	if err != nil {
		return SSHKeyState{}, err
	}
	if operation.ExpectedFingerprint != current.Fingerprint {
		return SSHKeyState{}, ErrSSHKeyConflict
	}
	lines := make([]string, 0, len(current.rawLines)+1)
	removed := false
	for _, line := range current.rawLines {
		if operation.Action == "remove" {
			if key, parseErr := parseAuthorizedKeyLine(line); parseErr == nil && key.Fingerprint == operation.Fingerprint {
				removed = true
				continue
			}
		}
		lines = append(lines, line)
	}
	if operation.Action == "remove" && !removed {
		return SSHKeyState{}, ErrSSHKeyNotFound
	}
	if operation.Action == "add" {
		key, parseErr := parseAuthorizedKeyLine(operation.Key)
		if parseErr != nil {
			return SSHKeyState{}, ErrInvalidSSHKeyOperation
		}
		for _, existing := range current.Keys {
			if existing.Fingerprint == key.Fingerprint {
				return SSHKeyState{}, ErrSSHKeyConflict
			}
		}
		lines = append(lines, key.Line)
	}
	account, accountErr := dependencies.lookup(operation.Username)
	if accountErr != nil {
		return SSHKeyState{}, accountErr
	}
	path, pathErr := dependencies.path(account)
	if pathErr != nil || path != current.Path {
		return SSHKeyState{}, ErrSSHKeyProtected
	}
	if err := writeAuthorizedKeys(path, lines, current, dependencies.lookup); err != nil {
		return SSHKeyState{}, err
	}
	updated, err := readSSHKeyStateWithDependencies(ctx, operation, actor, administrative, dependencies)
	if err != nil {
		return SSHKeyState{}, err
	}
	if operation.Action == "add" && len(updated.Keys) != len(current.Keys)+1 || operation.Action == "remove" && len(updated.Keys) != len(current.Keys)-1 {
		return SSHKeyState{}, ErrSSHKeyVerification
	}
	return updated, nil
}

func writeAuthorizedKeys(path string, lines []string, current SSHKeyState, lookup func(string) (sshKeyAccount, error)) error {
	account, err := lookup(current.Username)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("%w: create .ssh", ErrSSHKeyUnavailable)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("%w: secure .ssh", ErrSSHKeyUnavailable)
	}
	if err := os.Chown(directory, account.uid, account.gid); err != nil {
		return fmt.Errorf("%w: set .ssh ownership", ErrSSHKeyUnavailable)
	}
	temporary, err := os.CreateTemp(directory, ".tako-authorized-*")
	if err != nil {
		return fmt.Errorf("%w: create atomic file", ErrSSHKeyUnavailable)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: secure atomic file", ErrSSHKeyUnavailable)
	}
	if err := temporary.Chown(account.uid, account.gid); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: set key ownership", ErrSSHKeyUnavailable)
	}
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	if _, err := io.WriteString(temporary, content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: write authorized_keys", ErrSSHKeyUnavailable)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: sync authorized_keys", ErrSSHKeyUnavailable)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("%w: close authorized_keys", ErrSSHKeyUnavailable)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("%w: replace authorized_keys", ErrSSHKeyUnavailable)
	}
	directoryFile, err := os.Open(directory)
	if err == nil {
		_ = directoryFile.Sync()
		_ = directoryFile.Close()
	}
	return nil
}
