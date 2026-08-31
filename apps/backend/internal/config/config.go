package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address        string
	DataDir        string
	Certificate    string
	CertificateKey string
	SessionSocket  string
	// A nil list derives the allowed origin from each request's scheme and host.
	// A non-nil list is an explicit origin allowlist.
	AllowedOrigins     []string
	Development        bool
	MonitoringInterval time.Duration
	HistoryRetention   time.Duration
	AdminIdleTimeout   time.Duration
}

func Default() Config {
	return Config{
		Address:            ":9090",
		DataDir:            "/var/lib/tako",
		SessionSocket:      "/run/tako/session.sock",
		MonitoringInterval: time.Minute,
		HistoryRetention:   24 * time.Hour,
		AdminIdleTimeout:   5 * time.Minute,
	}
}

// Load accepts a deliberately small TOML subset: flat key/value pairs under
// [server]. Unknown keys fail closed so configuration mistakes stay visible.
func Load(path string, development bool) (Config, error) {
	cfg := Default()
	cfg.Development = development
	if development {
		cfg.Address = "127.0.0.1:9090"
		cfg.DataDir = filepath.Join(os.TempDir(), "tako-dev")
		cfg.AllowedOrigins = []string{"http://127.0.0.1:9090", "http://localhost:9090"}
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer file.Close()

	section := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[] ")
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return cfg, errors.New("invalid or unsupported configuration line")
		}
		key := strings.TrimSpace(parts[0])
		value, err := strconv.Unquote(strings.TrimSpace(parts[1]))
		if err != nil {
			return cfg, err
		}
		switch section + "." + key {
		case "server.address":
			cfg.Address = value
		case "server.data_dir":
			cfg.DataDir = value
		case "server.certificate":
			cfg.Certificate = value
		case "server.certificate_key":
			cfg.CertificateKey = value
		case "server.session_socket":
			cfg.SessionSocket = value
		case "server.allowed_origin":
			cfg.AllowedOrigins = splitOrigins(value)
		case "monitoring.default_interval":
			cfg.MonitoringInterval, err = parseDuration(value, time.Second, 5*time.Minute)
		case "monitoring.history_retention":
			cfg.HistoryRetention, err = parseDuration(value, 15*time.Minute, 24*time.Hour)
		case "admin.idle_timeout":
			cfg.AdminIdleTimeout, err = parseDuration(value, time.Minute, time.Hour)
		default:
			return cfg, errors.New("unknown configuration key: " + section + "." + key)
		}
		if err != nil {
			return cfg, err
		}
	}
	return cfg, scanner.Err()
}

func splitOrigins(value string) []string {
	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			origins = append(origins, part)
		}
	}
	return origins
}

func parseDuration(value string, minimum, maximum time.Duration) (time.Duration, error) {
	result, err := time.ParseDuration(value)
	if err != nil || result < minimum || result > maximum {
		return 0, errors.New("duration outside allowed range: " + value)
	}
	return result, nil
}
