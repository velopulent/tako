package dnf

import (
	"testing"

	"github.com/velopulent/tako/internal/platform"
)

func TestParseJSONAndTable(t *testing.T) {
	jsonItems := parseJSON(`{"packages":[{"name":"openssl","version":"3.2","arch":"x86_64"}]}`)
	if len(jsonItems) != 1 || jsonItems[0].Name != "openssl" {
		t.Fatalf("unexpected JSON inventory: %#v", jsonItems)
	}
	table := parseTable("openssl.x86_64 3.2 updates\n")
	if len(table) != 1 || table[0].CandidateVersion != "3.2" {
		t.Fatalf("unexpected table inventory: %#v", table)
	}
}

func TestParsePlanIncludesRiskyActions(t *testing.T) {
	changes := parsePlan("Upgrading:\n openssl x86_64 3.2 updates 1 M\n\nRemoving:\n obsolete x86_64 1.0 installed 1 M\n")
	if len(changes) != 2 || changes[0].Action != "remove" || changes[1].Action != "upgrade" {
		t.Fatalf("unexpected plan: %#v", changes)
	}
}

func TestParseHistory(t *testing.T) {
	items := parseHistory("2026-01-02T03:04:05+0000 Upgraded: openssl-3.2-1.x86_64\n")
	if len(items) != 1 {
		t.Fatalf("unexpected history: %#v", items)
	}
}

func TestSplitNEVRA(t *testing.T) {
	cases := map[string][3]string{
		"kernel-core-7.2.8-200.fc44.x86_64":                {"kernel-core", "7.2.8-200.fc44", "x86_64"},
		"kdesu-1:6.7.5-1.fc44.x86_64":                      {"kdesu", "6.7.5-1.fc44", "x86_64"},
		"python3-boto3-1.43.90-1.fc44.noarch":              {"python3-boto3", "1.43.90-1.fc44", "noarch"},
		"libibverbs-61.1^20260812git975bdaf-1.fc44.x86_64": {"libibverbs", "61.1^20260812git975bdaf-1.fc44", "x86_64"},
		"broken": {"", "", ""},
	}
	for input, want := range cases {
		name, version, arch := splitNEVRA(input)
		if [3]string{name, version, arch} != want {
			t.Fatalf("splitNEVRA(%q) = %q %q %q, want %v", input, name, version, arch, want)
		}
	}
}

func TestParseInstalledDropsEmptyEpochAndKeepsNewest(t *testing.T) {
	versions := parseInstalled("kernel\tx86_64\t(none):7.2.7-200.fc44\nkernel\tx86_64\t(none):7.2.8-200.fc44\nkdesu\tx86_64\t1:6.7.4-1.fc44\npackage foo is not installed\n")
	if versions["kernel.x86_64"] != "7.2.8-200.fc44" || versions["kdesu.x86_64"] != "1:6.7.4-1.fc44" || len(versions) != 2 {
		t.Fatalf("unexpected installed versions: %#v", versions)
	}
}

func TestEnrichmentPrefersAdvisoryForCandidateVersion(t *testing.T) {
	advisories := parseAdvisoryInfo(`[
	 {"Name":"FEDORA-2026-old","Title":"openssl-3.1","Severity":"Important","Type":"security","Issued":1790000000,"Description":"Old fix","references":[],"collections":{"packages":["openssl-3.1-1.fc44.x86_64"]}},
	 {"Name":"FEDORA-2026-new","Title":"openssl-3.2","Severity":"None","Type":"bugfix","Issued":1790817084,"Description":"New fix","references":[
	  {"Id":"CVE-2026-1","Type":"cve","Url":"https://www.cve.org/CVERecord?id=CVE-2026-1"},
	  {"Id":"1","Type":"bugzilla","Url":"https://bugzilla.redhat.com/show_bug.cgi?id=1"},
	  {"Id":"x","Type":"bugzilla","Url":"javascript:alert(1)"}],
	  "collections":{"packages":["openssl-1:3.2-1.fc44.x86_64","openssl-3.2-1.fc44.src"]}}
	]`)
	items := []platform.UpdatePackage{{Name: "openssl", Architecture: "x86_64", CandidateVersion: "1:3.2-1.fc44"}, {Name: "zlib", Architecture: "x86_64", CandidateVersion: "1.3"}}
	index := indexAdvisories(advisories, items)
	for i := range items {
		if value, ok := index[packageKey(items[i].Name, items[i].Architecture)]; ok {
			applyAdvisory(&items[i], value)
		}
	}
	got := items[0]
	if got.AdvisoryID != "FEDORA-2026-new" || got.Severity != "bugfix" || got.SecSeverity != "" || got.Description != "New fix" {
		t.Fatalf("unexpected advisory: %#v", got)
	}
	if len(got.CVEUrls) != 1 || len(got.BugUrls) != 1 || len(got.VendorUrls) != 1 || got.Issued == "" {
		t.Fatalf("unexpected references: %#v", got)
	}
	if items[1].AdvisoryID != "" || items[1].Severity != "" {
		t.Fatalf("package without advisory was classified: %#v", items[1])
	}
}

func TestParseUpdateinfoListDNF4(t *testing.T) {
	values := parseUpdateinfoList("RHSA-2026:1 Important/Sec. openssl-3.2-1.el9.x86_64\nRHBA-2026:2 bugfix zlib-1.3-1.el9.x86_64\nRHSA-2026:1 Important/Sec. openssl-libs-3.2-1.el9.x86_64\n")
	if len(values) != 2 || values[0].Type != "security" || values[0].Severity != "Important" || len(values[0].Collections.Packages) != 2 {
		t.Fatalf("unexpected updateinfo: %#v", values)
	}
}
