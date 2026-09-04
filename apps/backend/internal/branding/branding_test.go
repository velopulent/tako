package branding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIDReadsOnlyTheIDField(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want string
	}{
		{name: "plain", data: "NAME=Ubuntu\nID=ubuntu\nID_LIKE=debian\n", want: "ubuntu"},
		{name: "quoted and mixed case", data: "ID=\"OpenSUSE-Leap\"\n", want: "opensuse-leap"},
		{name: "single quoted", data: "ID='Fedora'\n", want: "fedora"},
		{name: "missing", data: "ID_LIKE=debian\n", want: ""},
		{name: "malformed", data: "ID ubuntu\n", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ParseID([]byte(test.data)); got != test.want {
				t.Fatalf("ParseID() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAssetForIDUsesExplicitMappings(t *testing.T) {
	for _, test := range []struct {
		id           string
		distribution string
		asset        string
	}{
		{id: "almalinux", distribution: "almalinux", asset: "almalinux.png"},
		{id: "arch", distribution: "arch", asset: "archlinux.png"},
		{id: "debian", distribution: "debian", asset: "debian.png"},
		{id: "fedora", distribution: "fedora", asset: "fedora.png"},
		{id: "rhel", distribution: "rhel", asset: "rhel.png"},
		{id: "ubuntu", distribution: "ubuntu", asset: "ubuntu.png"},
		{id: "rocky", distribution: "rockylinux", asset: "rocky.png"},
		{id: "rockylinux", distribution: "rockylinux", asset: "rocky.png"},
		{id: "opensuse-leap", distribution: "opensuse", asset: "opensuse.png"},
		{id: "opensuse-tumbleweed", distribution: "opensuse", asset: "opensuse.png"},
		{id: "redhat", distribution: "", asset: ""},
		{id: "ubuntu-derived", distribution: "", asset: ""},
		{id: "", distribution: "", asset: ""},
	} {
		t.Run(test.id, func(t *testing.T) {
			gotDistribution, gotAsset := AssetForID(test.id)
			if gotDistribution != test.distribution || gotAsset != test.asset {
				t.Fatalf("AssetForID(%q) = (%q, %q), want (%q, %q)", test.id, gotDistribution, gotAsset, test.distribution, test.asset)
			}
		})
	}
}

func TestServiceMetadataRequiresAnInstalledRegularAsset(t *testing.T) {
	directory := t.TempDir()
	assetPath := filepath.Join(directory, "ubuntu.png")
	if err := os.WriteFile(assetPath, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(directory, "1.2+build", false)
	service.readOSRelease = func() ([]byte, error) {
		return []byte("ID=ubuntu\nID_LIKE=debian\n"), nil
	}
	service.hostname = func() (string, error) {
		return " server-01 ", nil
	}

	metadata := service.Metadata()
	if metadata.Distribution != "ubuntu" || metadata.Hostname != "server-01" {
		t.Fatalf("metadata identity = %#v", metadata)
	}
	if metadata.BackgroundURL != "/branding/ubuntu.png?v=1.2%2Bbuild" {
		t.Fatalf("metadata background URL = %q", metadata.BackgroundURL)
	}

	if err := os.Remove(assetPath); err != nil {
		t.Fatal(err)
	}
	if metadata = service.Metadata(); metadata.BackgroundURL != "" || metadata.Distribution != "ubuntu" {
		t.Fatalf("missing asset metadata = %#v", metadata)
	}
}

func TestServicePathAllowListsAssets(t *testing.T) {
	service := New(t.TempDir(), "dev", true)
	if path, ok := service.Path("ubuntu.png"); !ok || path != filepath.Join(service.directory, "ubuntu.png") {
		t.Fatalf("allowed asset path = %q, %v", path, ok)
	}
	for _, name := range []string{"not-allowed.png", "../ubuntu.png", "ubuntu.png/secret"} {
		if path, ok := service.Path(name); ok || path != "" {
			t.Fatalf("Path(%q) = %q, %v; want rejection", name, path, ok)
		}
	}
}

func TestServiceImmutableOnlyForReleasedProductionAssets(t *testing.T) {
	if !New(t.TempDir(), "1.2.3", false).Immutable() {
		t.Fatal("released production service should use immutable assets")
	}
	if New(t.TempDir(), "1.2.3", true).Immutable() {
		t.Fatal("development service should not use immutable assets")
	}
	if New(t.TempDir(), DefaultVersion, false).Immutable() {
		t.Fatal("default development version should not use immutable assets")
	}
}
