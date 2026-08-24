package packagekit

import (
	"regexp"
	"sort"
	"strings"
)

const (
	EnumInfoLow         = 3
	EnumInfoEnhancement = 4
	EnumInfoNormal      = 5
	EnumInfoBugfix      = 6
	EnumInfoImportant   = 7
	EnumInfoSecurity    = 8
)

// mapInfoToSeverity maps a PackageKit info enum to a Tako severity string:
// security, bugfix, or enhancement.
func mapInfoToSeverity(info uint32) string {
	// HACK: security updates have 0x50008 with PK 1.2.8; mask lower 8 bits
	info = info & 0xff
	if info < EnumInfoLow || info > EnumInfoSecurity {
		info = EnumInfoNormal
	}
	switch info {
	case EnumInfoSecurity:
		return "security"
	case EnumInfoLow:
		return "enhancement"
	case EnumInfoEnhancement:
		return "enhancement"
	default:
		// INFO_NORMAL, INFO_BUGFIX, INFO_IMPORTANT all -> bugfix
		return "bugfix"
	}
}

var cvePattern = regexp.MustCompile(`CVE-\d{4}-\d+`)

func parseCVEs(text string) []string {
	if text == "" {
		return nil
	}
	matches := cvePattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	var out []string
	for _, cve := range matches {
		if _, ok := seen[cve]; ok {
			continue
		}
		seen[cve] = struct{}{}
		out = append(out, "https://www.cve.org/CVERecord?id="+cve)
	}
	return out
}

func deduplicate(list []string) []string {
	if len(list) == 0 {
		return list
	}
	seen := make(map[string]struct{}, len(list))
	for _, v := range list {
		seen[v] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func removeHeading(text string) string {
	if text == "" {
		return text
	}
	trimmed := strings.TrimSpace(text)
	// Strip a leading "== Title ==" heading line
	if strings.HasPrefix(trimmed, "== ") {
		// find first newline
		if idx := strings.Index(trimmed, "\n"); idx != -1 {
			firstLine := trimmed[:idx]
			if strings.HasSuffix(strings.TrimSpace(firstLine), "==") {
				return strings.TrimSpace(trimmed[idx+1:])
			}
		} else {
			// single line heading
			if strings.HasSuffix(trimmed, "==") {
				return ""
			}
		}
	}
	return text
}

func splitIntoBatches(ids []string, batchSize int) [][]string {
	if batchSize <= 0 {
		batchSize = len(ids)
	}
	var batches [][]string
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batches = append(batches, ids[i:end])
	}
	return batches
}
