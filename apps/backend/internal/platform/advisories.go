package platform

import (
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	maxAdvisoryText          = 4096
	maxSummaryText           = 1024
	maxAdvisoryURLs          = 16
	maxAdvisoryURL           = 512
	maxUpdateAdvisories      = 512
	maxAdvisoryResponseBytes = 1 << 20
)

// UpdateAdvisory is native errata data before matching and sanitization.
type UpdateAdvisory struct {
	ID, Title, Type, Severity, Description, Source string
	Issued                                         time.Time
	References                                     []UpdateReference
	Packages                                       []UpdateAdvisoryPackage
}

type UpdateReference struct{ ID, Type, URL string }

// Version retains the complete RPM epoch:version-release.
type UpdateAdvisoryPackage struct{ Name, Architecture, Version, Repository string }

// UpdateAdvisoryDetail is sent once per advisory, even when several available
// packages deliver its fixes. PackageKeys refer to name.architecture in Packages.
type UpdateAdvisoryDetail struct {
	limited        bool
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Type           string   `json:"type"`
	Severity       string   `json:"severity,omitempty"`
	VendorSeverity string   `json:"vendorSeverity,omitempty"`
	Description    string   `json:"description,omitempty"`
	Source         string   `json:"source"`
	Issued         string   `json:"issued,omitempty"`
	CVEIDs         []string `json:"cveIds"`
	CVEUrls        []string `json:"cveUrls"`
	BugUrls        []string `json:"bugUrls"`
	VendorUrls     []string `json:"vendorUrls"`
	PackageKeys    []string `json:"packageKeys"`
}

func UpdatePackageKey(name, architecture string) string { return name + "." + architecture }

var cveIDPattern = regexp.MustCompile(`(?i)^CVE-[0-9]{4}-[0-9]{4,}$`)
var cveInURLPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9-])(CVE-[0-9]{4}-[0-9]{4,})(?:$|[^a-z0-9-])`)

func validRPMVersion(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	rest := value
	if epoch, version, ok := strings.Cut(value, ":"); ok {
		if epoch == "" || version == "" {
			return false
		}
		for _, c := range epoch {
			if c < '0' || c > '9' {
				return false
			}
		}
		rest = version
	}
	hasAlnum := false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if rpmDigit(c) || rpmAlpha(c) {
			hasAlnum = true
			continue
		}
		if !strings.ContainsRune("._+~^-", rune(c)) {
			return false
		}
	}
	return hasAlnum
}

// ApplyUpdateAdvisories retains all fixes crossed by the proposed upgrade.
// A matching package name alone is never sufficient. Without an installed
// version we cannot establish that an advisory is still applicable.
func ApplyUpdateAdvisories(items []UpdatePackage, values []UpdateAdvisory) {
	byPackage := make(map[string][]int, len(items))
	for i := range items {
		byPackage[UpdatePackageKey(items[i].Name, items[i].Architecture)] = append(byPackage[UpdatePackageKey(items[i].Name, items[i].Architecture)], i)
		if items[i].AdvisoryID != "" {
			items[i].Summary = ""
		}
		items[i].AdvisoryID, items[i].GroupKey, items[i].Severity, items[i].SecSeverity = "", "", "", ""
		items[i].Description, items[i].Issued = "", ""
		items[i].CVEUrls, items[i].BugUrls, items[i].VendorUrls = nil, nil, nil
		items[i].Advisories = nil
		items[i].AdvisoryState = "available"
		if !validRPMVersion(items[i].CurrentVersion) || !validRPMVersion(items[i].CandidateVersion) {
			items[i].AdvisoryState = "partial"
		}
	}
	for _, value := range values {
		if strings.TrimSpace(value.ID) == "" {
			continue
		}
		detail := advisoryDetail(value)
		matched := map[int]bool{}
		for _, pkg := range value.Packages {
			if !validRPMVersion(pkg.Version) {
				continue
			}
			for _, i := range byPackage[UpdatePackageKey(pkg.Name, pkg.Architecture)] {
				item := &items[i]
				if !validRPMVersion(item.CurrentVersion) || !validRPMVersion(item.CandidateVersion) {
					continue
				}
				if pkg.Repository != "" && item.Repository != "" && pkg.Repository != item.Repository {
					continue
				}
				if CompareRPMVersions(item.CurrentVersion, pkg.Version) < 0 && CompareRPMVersions(pkg.Version, item.CandidateVersion) <= 0 {
					matched[i] = true
				}
			}
		}
		for i := range matched {
			if len(items[i].Advisories) >= 64 {
				items[i].AdvisoryState = "partial"
				continue
			}
			items[i].Advisories = append(items[i].Advisories, detail)
			if detail.limited {
				items[i].AdvisoryState = "partial"
			}
		}
	}
	for i := range items {
		item := &items[i]
		sort.SliceStable(item.Advisories, func(a, b int) bool { return advisoryLess(item.Advisories[a], item.Advisories[b]) })
		if len(item.Advisories) == 0 {
			continue
		}
		// Keep the legacy summary fields for existing consumers; the full set
		// is serialized once at status level by collectUpdateAdvisories.
		lead := item.Advisories[0]
		item.AdvisoryID, item.GroupKey, item.Severity, item.SecSeverity = lead.ID, lead.ID, lead.Type, lead.Severity
		item.Description, item.Issued = lead.Description, lead.Issued
		if lead.Title != "" {
			item.Summary = lead.Title
		}
		item.CVEUrls = lead.CVEUrls[:min(len(lead.CVEUrls), maxAdvisoryURLs)]
		item.BugUrls, item.VendorUrls = lead.BugUrls, lead.VendorUrls
	}
}

func advisoryLess(a, b UpdateAdvisoryDetail) bool {
	if advisoryRank(a.Type) != advisoryRank(b.Type) {
		return advisoryRank(a.Type) > advisoryRank(b.Type)
	}
	if securityRank(a.Severity) != securityRank(b.Severity) {
		return securityRank(a.Severity) > securityRank(b.Severity)
	}
	return a.ID < b.ID
}

func securityRank(value string) int {
	switch value {
	case "critical":
		return 4
	case "important":
		return 3
	case "moderate":
		return 2
	case "low":
		return 1
	}
	return 0
}

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

func advisoryDetail(value UpdateAdvisory) UpdateAdvisoryDetail {
	detail := UpdateAdvisoryDetail{
		ID: truncateText(strings.TrimSpace(value.ID), 128), Title: truncateText(value.Title, maxSummaryText),
		Type: NormalizeAdvisoryType(value.Type), VendorSeverity: truncateText(value.Severity, 64),
		Description: truncateText(strings.TrimSpace(value.Description), maxAdvisoryText), Source: truncateText(value.Source, 64),
		CVEIDs: []string{}, CVEUrls: []string{}, BugUrls: []string{}, VendorUrls: []string{}, PackageKeys: []string{},
	}
	if !value.Issued.IsZero() {
		detail.Issued = value.Issued.UTC().Format(time.RFC3339)
	}
	if detail.Type == "security" {
		switch strings.ToLower(strings.TrimSpace(value.Severity)) {
		case "critical":
			detail.Severity = "critical"
		case "important", "high":
			detail.Severity = "important"
		case "moderate", "medium":
			detail.Severity = "moderate"
		case "low":
			detail.Severity = "low"
		}
	}
	for _, reference := range value.References {
		kind := strings.ToLower(reference.Type)
		if kind == "cve" {
			id := strings.ToUpper(strings.TrimSpace(reference.ID))
			if !cveIDPattern.MatchString(id) {
				if match := cveInURLPattern.FindStringSubmatch(reference.URL); len(match) > 1 {
					id = strings.ToUpper(match[1])
				}
			}
			if cveIDPattern.MatchString(id) && len(id) <= 64 {
				if len(detail.CVEIDs) == 64 {
					detail.limited = true
				}
				detail.CVEIDs = appendUnique(detail.CVEIDs, id, 64)
			}
			continue
		}
		if !safeAdvisoryURL(reference.URL) {
			continue
		}
		if kind == "bugzilla" {
			detail.BugUrls = appendAdvisoryURL(detail.BugUrls, reference.URL)
		} else {
			detail.VendorUrls = appendAdvisoryURL(detail.VendorUrls, reference.URL)
		}
	}
	sort.Strings(detail.CVEIDs)
	for _, id := range detail.CVEIDs {
		detail.CVEUrls = append(detail.CVEUrls, "https://www.cve.org/CVERecord?id="+id)
	}
	sort.Strings(detail.BugUrls)
	sort.Strings(detail.VendorUrls)
	return detail
}

func safeAdvisoryURL(value string) bool {
	if len(value) > maxAdvisoryURL {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

func appendUnique(values []string, value string, limit int) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	if len(values) < limit {
		return append(values, value)
	}
	return values
}

func appendAdvisoryURL(values []string, value string) []string {
	if !safeAdvisoryURL(value) {
		return values
	}
	return appendUnique(values, value, maxAdvisoryURLs)
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}

func collectUpdateAdvisories(status *UpdateStatus) {
	byID := map[string]*UpdateAdvisoryDetail{}
	state := "unsupported"
	for _, item := range status.Packages {
		if item.AdvisoryState != "" {
			if state == "unsupported" {
				state = item.AdvisoryState
			} else if state != item.AdvisoryState {
				state = "partial"
			}
		}
		for _, detail := range item.Advisories {
			if detail.limited {
				state = "partial"
			}
			key := detail.ID
			current, ok := byID[key]
			if !ok {
				copy := detail
				copy.PackageKeys = []string{}
				current = &copy
				byID[key] = current
			}
			current.PackageKeys = appendUnique(current.PackageKeys, UpdatePackageKey(item.Name, item.Architecture), MaxUpdatePackages)
			for _, id := range detail.CVEIDs {
				if len(current.CVEIDs) == 64 && !slices.Contains(current.CVEIDs, id) {
					state = "partial"
				}
				current.CVEIDs = appendUnique(current.CVEIDs, id, 64)
			}
			for _, u := range detail.CVEUrls {
				current.CVEUrls = appendUnique(current.CVEUrls, u, 64)
			}
			for _, u := range detail.BugUrls {
				current.BugUrls = appendAdvisoryURL(current.BugUrls, u)
			}
			for _, u := range detail.VendorUrls {
				current.VendorUrls = appendAdvisoryURL(current.VendorUrls, u)
			}
		}
	}
	all := make([]UpdateAdvisoryDetail, 0, len(byID))
	for _, detail := range byID {
		sort.Strings(detail.PackageKeys)
		sort.Strings(detail.CVEIDs)
		sort.Strings(detail.CVEUrls)
		all = append(all, *detail)
	}
	sort.Slice(all, func(i, j int) bool { return advisoryLess(all[i], all[j]) })
	status.Advisories = []UpdateAdvisoryDetail{}
	bytes := 0
	for _, detail := range all {
		encoded, _ := json.Marshal(detail)
		if len(status.Advisories) >= maxUpdateAdvisories || bytes+len(encoded) > maxAdvisoryResponseBytes {
			state = "partial"
			break
		}
		status.Advisories = append(status.Advisories, detail)
		bytes += len(encoded)
	}
	status.AdvisoryState = state
}
