package branding

import (
	"bufio"
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultDirectory = "/usr/share/tako/branding"
	DefaultVersion   = "dev"
)

type Metadata struct {
	Distribution  string `json:"distribution"`
	Hostname      string `json:"hostname"`
	BackgroundURL string `json:"backgroundUrl,omitempty"`
}

type Service struct {
	directory   string
	version     string
	development bool

	readOSRelease func() ([]byte, error)
	hostname      func() (string, error)
	stat          func(string) (os.FileInfo, error)
}

func New(directory, version string, development bool) *Service {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		directory = DefaultDirectory
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = DefaultVersion
	}
	return &Service{
		directory:   directory,
		version:     version,
		development: development,
		readOSRelease: func() ([]byte, error) {
			return os.ReadFile("/etc/os-release")
		},
		hostname: os.Hostname,
		stat:     os.Stat,
	}
}

func (service *Service) Metadata() Metadata {
	metadata := Metadata{}
	if service == nil {
		return metadata
	}

	if hostname, err := service.hostname(); err == nil {
		metadata.Hostname = strings.TrimSpace(hostname)
	}

	var id string
	if payload, err := service.readOSRelease(); err == nil {
		id = ParseID(payload)
	}
	distribution, asset := AssetForID(id)
	metadata.Distribution = distribution
	if asset == "" {
		return metadata
	}

	info, err := service.stat(filepath.Join(service.directory, asset))
	if err != nil || !info.Mode().IsRegular() {
		return metadata
	}
	metadata.BackgroundURL = "/branding/" + asset + "?v=" + url.QueryEscape(service.version)
	return metadata
}

func (service *Service) Path(name string) (string, bool) {
	if service == nil {
		return "", false
	}
	if _, ok := allowedAssets[name]; !ok {
		return "", false
	}
	return filepath.Join(service.directory, name), true
}

func (service *Service) Immutable() bool {
	return service != nil && !service.development && service.version != DefaultVersion
}

func ParseID(payload []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "ID" {
			continue
		}
		return normalizeValue(value)
	}
	return ""
}

func AssetForID(id string) (distribution, asset string) {
	switch normalizeValue(id) {
	case "almalinux":
		return "almalinux", "almalinux.png"
	case "arch":
		return "arch", "archlinux.png"
	case "debian":
		return "debian", "debian.png"
	case "fedora":
		return "fedora", "fedora.png"
	case "rhel":
		return "rhel", "rhel.png"
	case "ubuntu":
		return "ubuntu", "ubuntu.png"
	case "rocky", "rockylinux":
		return "rockylinux", "rocky.png"
	}
	if strings.HasPrefix(normalizeValue(id), "opensuse") {
		return "opensuse", "opensuse.png"
	}
	return "", ""
}

func normalizeValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return strings.ToLower(strings.TrimSpace(value))
}

var allowedAssets = map[string]struct{}{
	"almalinux.png": {},
	"archlinux.png": {},
	"debian.png":    {},
	"fedora.png":    {},
	"rhel.png":      {},
	"opensuse.png":  {},
	"rocky.png":     {},
	"ubuntu.png":    {},
}
