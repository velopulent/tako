package dnf

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/platform"
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
	for index := range items {
		if items[index].CurrentVersion == "" {
			items[index].CurrentVersion = installed[packageKey(items[index].Name, items[index].Architecture)]
		}
	}
	var values []advisory
	if dnf5 {
		r := platform.RunUpdateCommand(ctx, "dnf", []string{"--cacheonly", "--quiet", "advisory", "info", "--updates", "--json"}, nil, nil)
		if r.ExitCode == 0 {
			values = parseAdvisoryInfo(r.Output)
		}
	} else {
		r := platform.RunUpdateCommand(ctx, "dnf", []string{"--cacheonly", "--quiet", "updateinfo", "list", "--updates"}, nil, nil)
		if r.ExitCode == 0 {
			values = parseUpdateinfoList(r.Output)
		}
	}
	platform.ApplyUpdateAdvisories(items, normalizeAdvisories(values))
	return items
}

func packageKey(name, arch string) string { return platform.UpdatePackageKey(name, arch) }

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
	return rest[:version], platform.StripEpoch(rest[version+1:]), arch
}

// normalizeAdvisories converts dnf advisories into the shared form and adds
// the Bodhi page for Fedora updates.
func normalizeAdvisories(values []advisory) []platform.UpdateAdvisory {
	result := make([]platform.UpdateAdvisory, 0, len(values))
	for _, value := range values {
		normalized := platform.UpdateAdvisory{ID: value.Name, Title: value.Title, Type: value.Type, Severity: value.Severity, Description: value.Description}
		if value.Issued > 0 {
			normalized.Issued = time.Unix(value.Issued, 0)
		}
		for _, reference := range value.References {
			normalized.References = append(normalized.References, platform.UpdateReference{Type: reference.Type, URL: reference.URL})
		}
		if strings.HasPrefix(value.Name, "FEDORA-") {
			normalized.References = append(normalized.References, platform.UpdateReference{Type: "vendor", URL: "https://bodhi.fedoraproject.org/updates/" + value.Name})
		}
		for _, nevra := range value.Collections.Packages {
			if name, version, arch := splitNEVRA(nevra); name != "" {
				normalized.Packages = append(normalized.Packages, platform.UpdateAdvisoryPackage{Name: name, Architecture: arch, Version: version})
			}
		}
		result = append(result, normalized)
	}
	return result
}
