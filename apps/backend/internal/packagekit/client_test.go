package packagekit

import (
	"reflect"
	"testing"
)

func TestMapInfoToSeverity(t *testing.T) {
	cases := []struct {
		info uint32
		want string
	}{
		{EnumInfoSecurity, "security"},
		{EnumInfoBugfix, "bugfix"},
		{EnumInfoImportant, "bugfix"},
		{EnumInfoNormal, "bugfix"},
		{EnumInfoLow, "enhancement"},
		{EnumInfoEnhancement, "enhancement"},
		{99, "bugfix"}, // unknown -> normal -> bugfix
	}
	for _, c := range cases {
		got := mapInfoToSeverity(c.info)
		if got != c.want {
			t.Fatalf("mapInfoToSeverity(%d)=%q want %q", c.info, got, c.want)
		}
	}
}

func TestParseCVEs(t *testing.T) {
	text := "Fixes CVE-2024-1234 and CVE-2024-5678 also CVE-2023-1"
	got := parseCVEs(text)
	want := []string{
		"https://www.cve.org/CVERecord?id=CVE-2024-1234",
		"https://www.cve.org/CVERecord?id=CVE-2024-5678",
		"https://www.cve.org/CVERecord?id=CVE-2023-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCVEs mismatch got %#v want %#v", got, want)
	}
}

func TestDeduplicate(t *testing.T) {
	got := deduplicate([]string{"b", "a", "b", "c", "a"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deduplicate got %#v want %#v", got, want)
	}
}

func TestRemoveHeading(t *testing.T) {
	if got := removeHeading("== version ==\nhello"); got != "hello" {
		t.Fatalf("removeHeading failed got %q", got)
	}
	if got := removeHeading("hello\nworld"); got != "hello\nworld" {
		t.Fatalf("removeHeading changed non-heading %q", got)
	}
}

func TestBatchSplitting(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e"}
	batches := splitIntoBatches(ids, 2)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches got %d", len(batches))
	}
	if len(batches[0]) != 2 || batches[2][0] != "e" {
		t.Fatalf("unexpected batches %#v", batches)
	}
}
