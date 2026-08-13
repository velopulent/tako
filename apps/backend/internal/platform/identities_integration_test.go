//go:build linux && integration

package platform

import (
	"context"
	"os"
	"testing"
)

func TestNSSIdentityInventoryLinuxVMSeam(t *testing.T) {
	if os.Getenv("TAKO_TEST_IDENTITY_VM") != "1" {
		t.Skip("set TAKO_TEST_IDENTITY_VM=1 in a supported Linux VM")
	}
	inventory, err := ListIdentityInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Users) == 0 || len(inventory.Groups) == 0 {
		t.Fatalf("NSS inventory unexpectedly empty: users=%d groups=%d", len(inventory.Users), len(inventory.Groups))
	}
	for _, user := range inventory.Users {
		if user.Source == "" || user.Mutable != user.Local {
			t.Fatalf("identity mutability contract violated: %#v", user)
		}
	}
}
