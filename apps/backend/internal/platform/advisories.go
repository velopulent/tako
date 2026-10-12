package platform

import (
	"strings"
	"time"
)

const (
	maxAdvisoryText = 4096
	maxSummaryText  = 1024
	maxAdvisoryURLs = 16
	maxAdvisoryURL  = 512
)

// UpdateAdvisory is a vendor erratum normalized from a provider's native
// format (dnf5 JSON, rpm-md updateinfo.xml, and so on).
type UpdateAdvisory struct {
	ID          string
	Title       string
	Type        string
	Severity    string
	Description string
	Issued      time.Time
	References  []UpdateReference
	Packages    []UpdateAdvisoryPackage
}

// UpdateReference links an advisory to a CVE, bug report, or vendor page.
// Type is "cve", "bugzilla", or anything else for vendor links.
type UpdateReference struct {
	Type string
	URL  string
}

// UpdateAdvisoryPackage names one binary package an advisory ships. Version
// is version-release without an epoch.
type UpdateAdvisoryPackage struct {
	Name         string
	Architecture string
	Version      string
}

func UpdatePackageKey(name, architecture string) string { return name + "." + architecture }

// StripEpoch removes a leading "epoch:" so versions compare across tools that
// do and do not print epochs.
func StripEpoch(version string) string {
	if _, rest, ok := strings.Cut(version, ":"); ok {
		return rest
	}
	return version
}

// IndexUpdateAdvisories maps each pending package to the advisory that ships
// its candidate version, falling back to the most severe advisory naming it.
func IndexUpdateAdvisories(values []UpdateAdvisory, items []UpdatePackage) map[string]UpdateAdvisory {
	candidates := make(map[string]string, len(items))
	for _, item := range items {
		candidates[UpdatePackageKey(item.Name, item.Architecture)] = StripEpoch(item.CandidateVersion)
	}
	index := map[string]UpdateAdvisory{}
	exact := map[string]bool{}
	for _, value := range values {
		for _, pkg := range value.Packages {
			key := UpdatePackageKey(pkg.Name, pkg.Architecture)
			candidate, pending := candidates[key]
			if !pending || exact[key] {
				continue
			}
			if StripEpoch(pkg.Version) == candidate {
				index[key], exact[key] = value, true
				continue
			}
			if current, ok := index[key]; !ok || advisoryRank(value.Type) > advisoryRank(current.Type) {
				index[key] = value
			}
		}
	}
	return index
}

// ApplyUpdateAdvisories annotates items in place with their indexed advisory.
func ApplyUpdateAdvisories(items []UpdatePackage, values []UpdateAdvisory) {
	if len(values) == 0 {
		return
	}
	index := IndexUpdateAdvisories(values, items)
	for i := range items {
		if value, ok := index[UpdatePackageKey(items[i].Name, items[i].Architecture)]; ok {
			ApplyUpdateAdvisory(&items[i], value)
		}
	}
}

// NormalizeAdvisoryType maps vendor categories onto security, bugfix, or
// enhancement. Unknown categories return "" so the package stays unclassified.
func NormalizeAdvisoryType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "security":
		return "security"
	case "bugfix", "recommended":
		return "bugfix"
	case "enhancement", "newpackage", "feature", "optional":
		return "enhancement"
	}
	return ""
}

func advisoryRank(value string) int {
	switch NormalizeAdvisoryType(value) {
	case "security":
		return 3
	case "bugfix":
		return 2
	case "enhancement":
		return 1
	}
	return 0
}

func ApplyUpdateAdvisory(item *UpdatePackage, value UpdateAdvisory) {
	item.AdvisoryID = truncateText(value.ID, 128)
	item.GroupKey = truncateText(value.ID, 256)
	item.Severity = NormalizeAdvisoryType(value.Type)
	item.SecSeverity = ""
	if item.Severity == "security" {
		switch severity := strings.ToLower(value.Severity); severity {
		case "critical", "important", "moderate", "low":
			item.SecSeverity = severity
		}
	}
	if value.Title != "" {
		item.Summary = truncateText(value.Title, maxSummaryText)
	}
	item.Description = truncateText(strings.TrimSpace(value.Description), maxAdvisoryText)
	item.Issued = ""
	if !value.Issued.IsZero() {
		item.Issued = value.Issued.UTC().Format(time.RFC3339)
	}
	item.CVEUrls, item.BugUrls, item.VendorUrls = nil, nil, nil
	for _, reference := range value.References {
		if !strings.HasPrefix(reference.URL, "https://") && !strings.HasPrefix(reference.URL, "http://") {
			continue
		}
		switch strings.ToLower(reference.Type) {
		case "cve":
			item.CVEUrls = appendAdvisoryURL(item.CVEUrls, reference.URL)
		case "bugzilla":
			item.BugUrls = appendAdvisoryURL(item.BugUrls, reference.URL)
		default:
			item.VendorUrls = appendAdvisoryURL(item.VendorUrls, reference.URL)
		}
	}
}

func appendAdvisoryURL(values []string, value string) []string {
	if len(values) >= maxAdvisoryURLs || len(value) > maxAdvisoryURL {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}
