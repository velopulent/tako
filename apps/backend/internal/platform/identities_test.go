package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNSSUsersAndGroupsMarksRemoteReadOnly(t *testing.T) {
	users, err := parseNSSUsers("local:x:1000:1000:Local User:/home/local:/bin/bash\nremote:x:2000:2000:Remote User:/home/remote:/bin/false\n", map[string]bool{"local": true})
	if err != nil || len(users) != 2 {
		t.Fatalf("users=%#v err=%v", users, err)
	}
	if !users[0].Local || !users[0].Mutable || users[1].Local || users[1].Mutable || users[1].Source != "nss-read-only" {
		t.Fatalf("mutability/source not marked: %#v", users)
	}
	groups, err := parseNSSGroups("local:x:1000:local\nremote:x:2000:remote\n", map[string]bool{"local": true})
	if err != nil || len(groups) != 2 || !groups[0].Mutable || groups[1].Mutable {
		t.Fatalf("groups=%#v err=%v", groups, err)
	}
}

func TestNSSCommandIsBoundedAndCancellable(t *testing.T) {
	bin := t.TempDir()
	getent := filepath.Join(bin, "getent")
	script := "#!/bin/sh\ncase \"$1\" in passwd) printf 'remote:x:2000:2000:Remote:/home/remote:/bin/false\\n';; group) printf 'remote:x:2000:remote\\n';; esac\n"
	if err := os.WriteFile(getent, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := nssCommand(context.Background(), "passwd")
	if err != nil || !strings.Contains(output, "remote:x:2000") {
		t.Fatalf("NSS output=%q err=%v", output, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := nssCommand(ctx, "passwd"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled NSS error=%v", err)
	}
}
