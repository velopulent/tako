package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Address        string
	DataDir        string
	Certificate    string
	CertificateKey string
	SessionSocket  string
	AllowedOrigins []string
	Development    bool
}

func Default() Config {
	return Config{
		Address:        ":9090",
		DataDir:        "/var/lib/tako",
		SessionSocket:  "/run/tako/session.sock",
		AllowedOrigins: []string{"https://localhost:9090"},
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
		if len(parts) != 2 || section != "server" {
			return cfg, errors.New("invalid or unsupported configuration line")
		}
		key := strings.TrimSpace(parts[0])
		value, err := strconv.Unquote(strings.TrimSpace(parts[1]))
		if err != nil {
			return cfg, err
		}
		switch key {
		case "address":
			cfg.Address = value
		case "data_dir":
			cfg.DataDir = value
		case "certificate":
			cfg.Certificate = value
		case "certificate_key":
			cfg.CertificateKey = value
		case "session_socket":
			cfg.SessionSocket = value
		case "allowed_origin":
			cfg.AllowedOrigins = []string{value}
		default:
			return cfg, errors.New("unknown server configuration key: " + key)
		}
	}
	return cfg, scanner.Err()
}
