package zypper

import (
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

// maxUpdateinfoBytes bounds the decompressed metadata read per repository.
const maxUpdateinfoBytes = 256 << 20

// updateinfoGlobs locate rpm-md errata cached by `zypper refresh`. Repositories
// that publish zstd-compressed metadata are skipped; their packages stay
// unclassified rather than pulling in a decompressor dependency.
var updateinfoGlobs = []string{
	"/var/cache/zypp/raw/*/repodata/*updateinfo.xml.gz",
	"/var/cache/zypp/raw/*/repodata/*updateinfo.xml",
}

type updateinfoUpdate struct {
	Type        string `xml:"type,attr"`
	ID          string `xml:"id"`
	Title       string `xml:"title"`
	Severity    string `xml:"severity"`
	Description string `xml:"description"`
	Issued      struct {
		Date string `xml:"date,attr"`
	} `xml:"issued"`
	References []struct {
		Href string `xml:"href,attr"`
		Type string `xml:"type,attr"`
	} `xml:"references>reference"`
	Packages []struct {
		Name    string `xml:"name,attr"`
		Arch    string `xml:"arch,attr"`
		Version string `xml:"version,attr"`
		Release string `xml:"release,attr"`
	} `xml:"pkglist>collection>package"`
}

// cachedAdvisories reads every cached updateinfo file and keeps only the
// advisories that name a pending package.
func cachedAdvisories(items []platform.UpdatePackage) []platform.UpdateAdvisory {
	pending := make(map[string]bool, len(items))
	for _, item := range items {
		pending[platform.UpdatePackageKey(item.Name, item.Architecture)] = true
	}
	var advisories []platform.UpdateAdvisory
	for _, pattern := range updateinfoGlobs {
		paths, _ := filepath.Glob(pattern)
		for _, path := range paths {
			advisories = append(advisories, readUpdateinfoFile(path, pending)...)
		}
	}
	return advisories
}

func readUpdateinfoFile(path string, pending map[string]bool) []platform.UpdateAdvisory {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(path, ".gz") {
		decompressed, err := gzip.NewReader(file)
		if err != nil {
			return nil
		}
		defer decompressed.Close()
		reader = decompressed
	}
	return parseUpdateinfo(io.LimitReader(reader, maxUpdateinfoBytes), pending)
}

// parseUpdateinfo streams an rpm-md updateinfo document. A nil pending set
// keeps every advisory.
func parseUpdateinfo(reader io.Reader, pending map[string]bool) []platform.UpdateAdvisory {
	decoder := xml.NewDecoder(reader)
	var advisories []platform.UpdateAdvisory
	for {
		token, err := decoder.Token()
		if err != nil {
			return advisories
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "update" {
			continue
		}
		var update updateinfoUpdate
		if decoder.DecodeElement(&update, &start) != nil {
			return advisories
		}
		advisory := platform.UpdateAdvisory{
			ID:          strings.TrimSpace(update.ID),
			Title:       strings.TrimSpace(update.Title),
			Type:        update.Type,
			Severity:    strings.TrimSpace(update.Severity),
			Description: update.Description,
			Issued:      parseIssued(update.Issued.Date),
		}
		relevant := false
		for _, pkg := range update.Packages {
			if pending != nil && !pending[platform.UpdatePackageKey(pkg.Name, pkg.Arch)] {
				continue
			}
			relevant = true
			advisory.Packages = append(advisory.Packages, platform.UpdateAdvisoryPackage{Name: pkg.Name, Architecture: pkg.Arch, Version: pkg.Version + "-" + pkg.Release})
		}
		if !relevant || advisory.ID == "" {
			continue
		}
		for _, reference := range update.References {
			advisory.References = append(advisory.References, platform.UpdateReference{Type: reference.Type, URL: reference.Href})
		}
		advisories = append(advisories, advisory)
	}
}

// parseIssued accepts the Unix seconds SUSE publishes and the
// "YYYY-MM-DD HH:MM:SS" form other rpm-md producers use.
func parseIssued(value string) time.Time {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return time.Unix(seconds, 0)
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
