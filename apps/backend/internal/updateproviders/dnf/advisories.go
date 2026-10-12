package dnf

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

const (
	maxAdvisoryText = 4096
	maxSummaryText  = 1024
	maxAdvisoryURLs = 16
)

// advisory is the subset of `dnf5 advisory info --json` that the inventory uses.
type advisory struct {
	Name        string `json:"Name"`
	Title       string `json:"Title"`
	Severity    string `json:"Severity"`
	Type        string `json:"Type"`
	Issued      int64  `json:"Issued"`
	Description string `json:"Description"`
	References  []struct {
		ID   string `json:"Id"`
		Type string `json:"Type"`
		URL  string `json:"Url"`
	} `json:"references"`
	Collections struct {
		Packages []string `json:"packages"`
	} `json:"collections"`
}

// enrich adds installed versions and errata metadata. Both lookups are
// best-effort: a failure leaves the plain inventory intact.
func enrich(ctx context.Context, dnf5 bool, items []platform.UpdatePackage) []platform.UpdatePackage {
	if len(items) == 0 {
		return items
	}
	installed := installedVersions(ctx, items)
	var byPackage map[string]advisory
	if dnf5 {
		r := platform.RunUpdateCommand(ctx, "dnf", []string{"--cacheonly", "--quiet", "advisory", "info", "--updates", "--json"}, nil, nil)
		if r.ExitCode == 0 {
			byPackage = indexAdvisories(parseAdvisoryInfo(r.Output), items)
		}
	} else {
		r := platform.RunUpdateCommand(ctx, "dnf", []string{"--cacheonly", "--quiet", "updateinfo", "list", "--updates"}, nil, nil)
		if r.ExitCode == 0 {
			byPackage = indexAdvisories(parseUpdateinfoList(r.Output), items)
		}
	}
	for index := range items {
		item := &items[index]
		key := packageKey(item.Name, item.Architecture)
		if item.CurrentVersion == "" {
			item.CurrentVersion = installed[key]
		}
		if value, ok := byPackage[key]; ok {
			applyAdvisory(item, value)
		}
	}
	return items
}

func packageKey(name, arch string) string { return name + "." + arch }

func installedVersions(ctx context.Context, items []platform.UpdatePackage) map[string]string {
	arguments := []string{"-q", "--qf", "%{NAME}\t%{ARCH}\t%{EPOCH}:%{VERSION}-%{RELEASE}\n"}
	seen := map[string]bool{}
	for _, item := range items {
		if !seen[item.Name] {
			seen[item.Name] = true
			arguments = append(arguments, item.Name)
		}
	}
	// rpm exits non-zero when any name is missing; the matched rows are still valid.
	r := platform.RunUpdateCommand(ctx, "rpm", arguments, nil, nil)
	return parseInstalled(r.Output)
}

func parseInstalled(output string) map[string]string {
	versions := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 3 {
			continue
		}
		evr := fields[2]
		if epoch, rest, ok := strings.Cut(evr, ":"); ok && (epoch == "(none)" || epoch == "0") {
			evr = rest
		}
		// Multi-version packages such as kernel list oldest first; keep the newest.
		versions[packageKey(fields[0], fields[1])] = evr
	}
	return versions
}

func parseAdvisoryInfo(output string) []advisory {
	var values []advisory
	if json.Unmarshal([]byte(output), &values) != nil {
		return nil
	}
	return values
}

// parseUpdateinfoList reads dnf4 `updateinfo list` rows: ID TYPE NEVRA, where
// TYPE is bugfix, enhancement, or "<Severity>/Sec." for security errata.
func parseUpdateinfoList(output string) []advisory {
	byName := map[string]*advisory{}
	order := []string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || !strings.Contains(fields[2], ".") {
			continue
		}
		value, ok := byName[fields[0]]
		if !ok {
			value = &advisory{Name: fields[0], Type: fields[1]}
			if severity, found := strings.CutSuffix(fields[1], "/Sec."); found {
				value.Type, value.Severity = "security", severity
			}
			byName[fields[0]] = value
			order = append(order, fields[0])
		}
		value.Collections.Packages = append(value.Collections.Packages, fields[2])
	}
	values := make([]advisory, 0, len(order))
	for _, name := range order {
		values = append(values, *byName[name])
	}
	return values
}

// splitNEVRA returns name, version-release, and arch. Epochs are dropped so the
// version compares against candidate versions that may or may not carry one.
func splitNEVRA(nevra string) (string, string, string) {
	dot := strings.LastIndexByte(nevra, '.')
	if dot <= 0 {
		return "", "", ""
	}
	arch, rest := nevra[dot+1:], nevra[:dot]
	release := strings.LastIndexByte(rest, '-')
	if release <= 0 {
		return "", "", ""
	}
	version := strings.LastIndexByte(rest[:release], '-')
	if version <= 0 {
		return "", "", ""
	}
	return rest[:version], stripEpoch(rest[version+1:]), arch
}

func stripEpoch(version string) string {
	if _, rest, ok := strings.Cut(version, ":"); ok {
		return rest
	}
	return version
}

// indexAdvisories maps each pending package to the advisory that ships its
// candidate version, falling back to the most severe advisory naming it.
func indexAdvisories(values []advisory, items []platform.UpdatePackage) map[string]advisory {
	candidates := map[string]string{}
	for _, item := range items {
		candidates[packageKey(item.Name, item.Architecture)] = stripEpoch(item.CandidateVersion)
	}
	index := map[string]advisory{}
	exact := map[string]bool{}
	for _, value := range values {
		for _, nevra := range value.Collections.Packages {
			name, version, arch := splitNEVRA(nevra)
			key := packageKey(name, arch)
			candidate, pending := candidates[key]
			if !pending || exact[key] {
				continue
			}
			if version == candidate {
				index[key], exact[key] = value, true
				continue
			}
			if current, ok := index[key]; !ok || advisoryRank(value) > advisoryRank(current) {
				index[key] = value
			}
		}
	}
	return index
}

func advisoryRank(value advisory) int {
	switch normalizeType(value.Type) {
	case "security":
		return 3
	case "bugfix":
		return 2
	case "enhancement":
		return 1
	}
	return 0
}

func normalizeType(value string) string {
	switch strings.ToLower(value) {
	case "security":
		return "security"
	case "bugfix":
		return "bugfix"
	case "enhancement", "newpackage":
		return "enhancement"
	}
	return ""
}

func applyAdvisory(item *platform.UpdatePackage, value advisory) {
	item.AdvisoryID = value.Name
	item.GroupKey = value.Name
	item.Severity = normalizeType(value.Type)
	if item.Severity == "security" {
		switch severity := strings.ToLower(value.Severity); severity {
		case "critical", "important", "moderate", "low":
			item.SecSeverity = severity
		}
	}
	if value.Title != "" {
		item.Summary = truncate(value.Title, maxSummaryText)
	}
	item.Description = truncate(strings.TrimSpace(value.Description), maxAdvisoryText)
	if value.Issued > 0 {
		item.Issued = time.Unix(value.Issued, 0).UTC().Format(time.RFC3339)
	}
	item.CVEUrls, item.BugUrls, item.VendorUrls = nil, nil, nil
	for _, reference := range value.References {
		if !strings.HasPrefix(reference.URL, "https://") && !strings.HasPrefix(reference.URL, "http://") {
			continue
		}
		switch strings.ToLower(reference.Type) {
		case "cve":
			item.CVEUrls = appendBounded(item.CVEUrls, reference.URL)
		case "bugzilla":
			item.BugUrls = appendBounded(item.BugUrls, reference.URL)
		default:
			item.VendorUrls = appendBounded(item.VendorUrls, reference.URL)
		}
	}
	if strings.HasPrefix(value.Name, "FEDORA-") {
		item.VendorUrls = appendBounded(item.VendorUrls, "https://bodhi.fedoraproject.org/updates/"+value.Name)
	}
}

func appendBounded(values []string, value string) []string {
	if len(values) >= maxAdvisoryURLs || len(value) > 512 {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}
