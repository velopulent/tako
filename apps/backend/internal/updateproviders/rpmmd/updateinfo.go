package rpmmd

import (
	"compress/bzip2"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/platform"
)

// maxUpdateinfoBytes bounds the decompressed metadata read per repository.
const maxUpdateinfoBytes = 256 << 20

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
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
		Type string `xml:"type,attr"`
	} `xml:"references>reference"`
	Packages []struct {
		Epoch   string `xml:"epoch,attr"`
		Name    string `xml:"name,attr"`
		Arch    string `xml:"arch,attr"`
		Version string `xml:"version,attr"`
		Release string `xml:"release,attr"`
	} `xml:"pkglist>collection>package"`
}

// ReadCached reads only existing repository metadata. Unsupported compression,
// missing files and malformed documents are reported so the UI can show partial
// coverage instead of interpreting missing data as absence of security fixes.
func ReadCached(patterns []string, items []platform.UpdatePackage) ([]platform.UpdateAdvisory, error) {
	pending := make(map[string]bool, len(items))
	for _, item := range items {
		pending[platform.UpdatePackageKey(item.Name, item.Architecture)] = true
	}
	paths := map[string]bool{}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			paths[path] = true
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var values []platform.UpdateAdvisory
	var failures []error
	if len(ordered) == 0 {
		return nil, errors.New("cached advisory metadata unavailable")
	}
	for _, path := range ordered {
		if len(values) >= 4096 {
			failures = append(failures, errors.New("advisory metadata limit reached"))
			break
		}
		records, err := ReadFile(path, pending)
		values = append(values, records...)
		if err != nil {
			failures = append(failures, err)
		}
	}
	return values, errors.Join(failures...)
}

func ReadFile(path string, pending map[string]bool) ([]platform.UpdateAdvisory, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var reader io.Reader = file
	switch {
	case strings.HasSuffix(path, ".gz"):
		decompressed, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer decompressed.Close()
		reader = decompressed
	case strings.HasSuffix(path, ".bz2"):
		reader = bzip2.NewReader(file)
	case strings.HasSuffix(path, ".xml"):
	default:
		return nil, errors.New("unsupported advisory metadata compression")
	}
	limited := &io.LimitedReader{R: reader, N: maxUpdateinfoBytes + 1}
	records, err := Parse(limited, pending)
	if limited.N == 0 {
		err = errors.New("advisory metadata byte limit reached")
	}
	return records, err
}

// parseUpdateinfo streams an rpm-md updateinfo document. A nil pending set
// keeps every advisory.
func Parse(reader io.Reader, pending map[string]bool) ([]platform.UpdateAdvisory, error) {
	decoder := xml.NewDecoder(reader)
	var advisories []platform.UpdateAdvisory
	for len(advisories) < 4096 {
		token, err := decoder.Token()
		if err == io.EOF {
			return advisories, nil
		}
		if err != nil {
			return advisories, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "update" {
			continue
		}
		var update updateinfoUpdate
		if err := decoder.DecodeElement(&update, &start); err != nil {
			return advisories, err
		}
		advisory := platform.UpdateAdvisory{
			ID:          strings.TrimSpace(update.ID),
			Source:      "rpm-md",
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
			advisory.Packages = append(advisory.Packages, platform.UpdateAdvisoryPackage{Name: pkg.Name, Architecture: pkg.Arch, Version: packageVersion(pkg.Epoch, pkg.Version, pkg.Release)})
		}
		if !relevant || advisory.ID == "" {
			continue
		}
		for _, reference := range update.References {
			advisory.References = append(advisory.References, platform.UpdateReference{ID: reference.ID, Type: reference.Type, URL: reference.Href})
		}
		advisories = append(advisories, advisory)
	}
	return advisories, fmt.Errorf("advisory metadata limit reached")
}

func packageVersion(epoch, version, release string) string {
	if release != "" {
		version += "-" + release
	}
	if epoch != "" && epoch != "0" {
		version = epoch + ":" + version
	}
	return version
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
