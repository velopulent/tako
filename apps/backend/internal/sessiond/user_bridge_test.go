package sessiond

import (
	"os/user"
	"slices"
	"testing"

	"github.com/velopulent/tako/internal/auth"
)

func TestBridgeEnvironmentUsesPAMAndCanonicalAccountValues(t *testing.T) {
	account := &user.User{Username: "octopus", HomeDir: "/home/octopus"}
	environment := bridgeEnvironment(map[string]string{
		"XDG_RUNTIME_DIR": "/run/user/1000",
		"HOME":            "/wrong",
		"USER":            "wrong",
	}, account)
	for _, expected := range []string{
		"HOME=/home/octopus",
		"USER=octopus",
		"LOGNAME=octopus",
		"XDG_RUNTIME_DIR=/run/user/1000",
	} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("bridge environment lacks %q: %#v", expected, environment)
		}
	}
}

func TestUserCredentialIncludesSupplementaryGroups(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := lookupTestIdentity(account)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := userCredential(account, identity)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Uid != uint32(identity.UID) || credential.Gid != uint32(identity.GID) || len(credential.Groups) == 0 {
		t.Fatalf("incomplete user credential: %#v", credential)
	}
}

func lookupTestIdentity(account *user.User) (auth.Identity, error) {
	uid, err := parseID(account.Uid)
	if err != nil {
		return auth.Identity{}, err
	}
	gid, err := parseID(account.Gid)
	if err != nil {
		return auth.Identity{}, err
	}
	return auth.Identity{Username: account.Username, UID: uid, GID: gid}, nil
}
