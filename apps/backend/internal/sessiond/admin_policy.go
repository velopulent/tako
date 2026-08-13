package sessiond

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/velopulent/tako/internal/auth"
)

var errAdministrativeDenied = errors.New("administrative access denied")

const (
	policyOutputLimit = 16 << 10
	policyActionID    = "org.velopulent.tako.administrate"
)

// newAdministrativePolicy builds the only policy adapter used by sessiond.
// The gateway never runs these checks: the root-owned session service evaluates
// the invoking user's sudo or Polkit policy and returns only an opaque grant.
func newAdministrativePolicy() administrativePolicy {
	return func(ctx context.Context, identity auth.Identity, password string) error {
		if os.Geteuid() != 0 {
			return errAdministrativeUnavailable
		}
		policyFound := false
		if sudoPath, err := exec.LookPath("sudo"); err == nil {
			policyFound = true
			if runAsIdentity(ctx, sudoPath, identity, "", "-n", "-u", "root", "--", "/usr/bin/true") == nil {
				return nil // NOPASSWD policy.
			}
			if password != "" && runAsIdentity(ctx, sudoPath, identity, password, "-S", "-p", "", "-u", "root", "--", "/usr/bin/true") == nil {
				return nil // Password-backed sudo, including the host PAM stack.
			}
		}
		if pkcheckPath, err := exec.LookPath("pkcheck"); err == nil {
			policyFound = true
			if runAsIdentity(ctx, pkcheckPath, identity, "", "--action-id", policyActionID, "--allow-user-interaction") == nil {
				return nil
			}
		}
		if policyFound {
			return errAdministrativeDenied
		}
		return errAdministrativeUnavailable
	}
}

type boundedOutput struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (output *boundedOutput) Write(payload []byte) (int, error) {
	if output.limit <= output.Len() {
		output.overflow = true
		return len(payload), nil
	}
	remaining := output.limit - output.Len()
	if len(payload) > remaining {
		_, _ = output.Buffer.Write(payload[:remaining])
		output.overflow = true
		return len(payload), nil
	}
	return output.Buffer.Write(payload)
}

func runAsIdentity(ctx context.Context, executable string, identity auth.Identity, password string, arguments ...string) error {
	account, err := user.LookupId(strconv.Itoa(identity.UID))
	if err != nil {
		return err
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return err
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return err
	}
	groupIDs, err := account.GroupIds()
	if err != nil {
		return err
	}
	groups := make([]uint32, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		parsed, parseErr := strconv.ParseUint(groupID, 10, 32)
		if parseErr != nil {
			return parseErr
		}
		groups = append(groups, uint32(parsed))
	}
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = account.HomeDir
	command.Env = []string{
		"HOME=" + account.HomeDir,
		"LOGNAME=" + account.Username,
		"PATH=/usr/bin:/bin",
		"USER=" + account.Username,
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groups},
	}
	if password != "" {
		command.Stdin = strings.NewReader(password + "\n")
	}
	stdout, stderr := &boundedOutput{limit: policyOutputLimit}, &boundedOutput{limit: policyOutputLimit}
	command.Stdout = stdout
	command.Stderr = stderr
	err = command.Run()
	password = ""
	if stdout.overflow || stderr.overflow {
		return errors.New("policy command output exceeded limit")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	return nil
}
