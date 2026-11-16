package zypper

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/updateproviders/rpmmd"
)

// Repository caches survive disable/removal. Only read metadata from enabled
// repositories and, when reported, the repository supplying the candidate.
func cachedAdvisories(items []platform.UpdatePackage) ([]platform.UpdateAdvisory, error) {
	configs, _ := filepath.Glob("/etc/zypp/repos.d/*.repo")
	enabled := map[string]string{}
	var failures []error
	for _, path := range configs {
		file, err := os.Open(path)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		repos, err := enabledRepositories(file)
		_ = file.Close()
		if err != nil {
			failures = append(failures, err)
		}
		for alias, name := range repos {
			enabled[alias] = name
		}
	}
	paths, _ := filepath.Glob("/var/cache/zypp/raw/*/repodata/*updateinfo.xml*")
	var values []platform.UpdateAdvisory
	read := false
	for _, path := range paths {
		alias := filepath.Base(filepath.Dir(filepath.Dir(path)))
		name, ok := enabled[alias]
		if !ok {
			continue
		}
		pending := map[string]bool{}
		for _, item := range items {
			if item.Repository == "" || item.Repository == alias || item.Repository == name {
				pending[platform.UpdatePackageKey(item.Name, item.Architecture)] = true
			}
		}
		if len(pending) == 0 {
			continue
		}
		read = true
		records, err := rpmmd.ReadFile(path, pending)
		values = append(values, records...)
		if err != nil {
			failures = append(failures, err)
		}
		if len(values) >= 4096 {
			failures = append(failures, errors.New("advisory metadata limit reached"))
			break
		}
	}
	if !read {
		failures = append(failures, errors.New("enabled repository advisory metadata unavailable"))
	}
	return values, errors.Join(failures...)
}

func enabledRepositories(reader io.Reader) (map[string]string, error) {
	result := map[string]string{}
	alias, name, enabled := "", "", true
	save := func() {
		if alias != "" && enabled {
			result[alias] = name
		}
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			save()
			alias = strings.TrimSpace(line[1 : len(line)-1])
			name = ""
			enabled = true
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "name":
			name = value
		case "enabled":
			enabled = value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
		}
	}
	save()
	return result, scanner.Err()
}
