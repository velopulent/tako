package rpmmd

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/velopulent/tako/internal/platform"
)

const advisoryXML = `<updates><update type="security"><id>TEST-1</id><title>Security fixes</title><severity>high</severity><references><reference id="CVE-2026-12345" type="cve"/></references><pkglist><collection><package name="one" epoch="2" version="1.0" release="3" arch="x86_64"/></collection></pkglist></update></updates>`

func TestCachedMetadataPreservesEpochAndReferenceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repo-updateinfo.xml.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	if _, err = writer.Write([]byte(advisoryXML)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	values, err := ReadCached([]string{filepath.Join(dir, "*updateinfo.xml*")}, []platform.UpdatePackage{{Name: "one", Architecture: "x86_64"}})
	if err != nil || len(values) != 1 || values[0].Packages[0].Version != "2:1.0-3" || values[0].References[0].ID != "CVE-2026-12345" {
		t.Fatalf("wrong metadata: %#v %v", values, err)
	}
	if err = os.WriteFile(filepath.Join(dir, "other-updateinfo.xml.zst"), []byte("unsupported"), 0600); err != nil {
		t.Fatal(err)
	}
	values, err = ReadCached([]string{filepath.Join(dir, "*updateinfo.xml*")}, []platform.UpdatePackage{{Name: "one", Architecture: "x86_64"}})
	if err == nil || len(values) != 1 {
		t.Fatal("did not retain good metadata and report partial coverage")
	}
}
func TestMalformedMetadataReportsPartialResults(t *testing.T) {
	values, err := Parse(strings.NewReader(strings.TrimSuffix(advisoryXML, "</updates>")+"<update>"), nil)
	if err == nil || len(values) != 1 {
		t.Fatalf("expected one complete record and parse error: %d %v", len(values), err)
	}
}
