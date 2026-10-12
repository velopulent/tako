package zypper

import (
	"strings"
	"testing"

	"github.com/velopulent/tako/internal/platform"
)

func TestParseXMLAndHistory(t *testing.T) {
	items := parseXML(`<stream><update-list><update name="openssl" edition="3.2" edition-old="3.1" arch="x86_64"/></update-list></stream>`)
	if len(items) != 1 || items[0].Name != "openssl" || items[0].CurrentVersion != "3.1" {
		t.Fatalf("unexpected inventory: %#v", items)
	}
	history := parseHistory("2026-01-02 03:04:05|update|openssl|3.1|3.2|x86_64|repo\n")
	if len(history) != 1 || history[0].Packages["openssl"] != "3.2" {
		t.Fatalf("unexpected history: %#v", history)
	}
}

func TestParsePlanXMLIncludesRiskyActions(t *testing.T) {
	changes := parsePlanXML(`<stream><solvable status="to-be-uninstalled" name="old" edition-old="1"/><solvable status="to-be-upgraded" name="new" edition="2"/></stream>`)
	if len(changes) != 2 || changes[0].Action != "upgrade" || changes[1].Action != "remove" {
		t.Fatalf("unexpected plan: %#v", changes)
	}
}

func TestOperationDependsOnlyOnReleaseKind(t *testing.T) {
	if operation(Provider{}) != "update" || operation(Provider{tumbleweed: true}) != "dup" {
		t.Fatal("unexpected zypper operation")
	}
}

const sampleUpdateinfo = `<?xml version="1.0" encoding="UTF-8"?>
<updates>
  <update from="maint-coord@suse.de" status="stable" type="security" version="1">
    <id>openSUSE-SLE-15.6-2026-1234</id>
    <title>Security update for openssl-3</title>
    <severity>important</severity>
    <issued date="1790817084"/>
    <references>
      <reference href="https://bugzilla.suse.com/1223456" id="1223456" title="crash" type="bugzilla"/>
      <reference href="https://www.suse.com/security/cve/CVE-2026-1234/" id="CVE-2026-1234" title="CVE" type="cve"/>
      <reference href="javascript:alert(1)" id="x" title="bad" type="self"/>
    </references>
    <description>This update for openssl-3 fixes the following issues.</description>
    <pkglist><collection>
      <package name="openssl-3" epoch="0" version="3.1.4" release="150600.5.7.1" arch="x86_64" src="src"><filename>f</filename></package>
      <package name="libopenssl3" epoch="0" version="3.1.4" release="150600.5.7.1" arch="x86_64" src="src"><filename>f</filename></package>
    </collection></pkglist>
  </update>
  <update from="maint-coord@suse.de" status="stable" type="recommended" version="1">
    <id>openSUSE-SLE-15.6-2026-9999</id>
    <title>Recommended update for unrelated</title>
    <severity>moderate</severity>
    <issued date="2026-01-02 03:04:05"/>
    <description>Unrelated.</description>
    <pkglist><collection>
      <package name="unrelated" version="1" release="1" arch="noarch"/>
    </collection></pkglist>
  </update>
</updates>`

func TestUpdateinfoClassifiesPendingPackages(t *testing.T) {
	items := []platform.UpdatePackage{
		{Name: "openssl-3", Architecture: "x86_64", CurrentVersion: "3.1.4-150600.5.3.1", CandidateVersion: "3.1.4-150600.5.7.1"},
		{Name: "libopenssl3", Architecture: "x86_64", CandidateVersion: "3.1.4-150600.5.7.1"},
		{Name: "vim", Architecture: "x86_64", CandidateVersion: "9.1-1"},
	}
	pending := map[string]bool{}
	for _, item := range items {
		pending[platform.UpdatePackageKey(item.Name, item.Architecture)] = true
	}
	advisories := parseUpdateinfo(strings.NewReader(sampleUpdateinfo), pending)
	if len(advisories) != 1 {
		t.Fatalf("expected only the relevant advisory, got %#v", advisories)
	}
	platform.ApplyUpdateAdvisories(items, advisories)
	got := items[0]
	if got.AdvisoryID != "openSUSE-SLE-15.6-2026-1234" || got.GroupKey != got.AdvisoryID || got.Severity != "security" || got.SecSeverity != "important" {
		t.Fatalf("unexpected classification: %#v", got)
	}
	if len(got.CVEUrls) != 1 || len(got.BugUrls) != 1 || len(got.VendorUrls) != 0 || got.Issued != "2026-10-01T01:11:24Z" {
		t.Fatalf("unexpected references: %#v", got)
	}
	if items[1].AdvisoryID != got.AdvisoryID {
		t.Fatalf("sibling package not grouped: %#v", items[1])
	}
	if items[2].Severity != "" || items[2].AdvisoryID != "" {
		t.Fatalf("package without errata was classified: %#v", items[2])
	}
}

func TestUpdateinfoMapsRecommendedToBugfixAndParsesDates(t *testing.T) {
	advisories := parseUpdateinfo(strings.NewReader(sampleUpdateinfo), nil)
	if len(advisories) != 2 || platform.NormalizeAdvisoryType(advisories[1].Type) != "bugfix" || advisories[1].Issued.IsZero() {
		t.Fatalf("unexpected advisories: %#v", advisories)
	}
}

func TestUpdateinfoToleratesTruncatedInput(t *testing.T) {
	if got := parseUpdateinfo(strings.NewReader(sampleUpdateinfo[:400]), nil); len(got) != 0 {
		t.Fatalf("truncated document produced advisories: %#v", got)
	}
}
