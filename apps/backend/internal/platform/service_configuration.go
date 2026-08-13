package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxUnitConfigurationBytes = 1 << 20

var errUnitConfigurationPath = errors.New("unit configuration path is not trusted")

type UnitConfiguration struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

func ReadUnitConfiguration(ctx context.Context, scope, name string) (UnitConfiguration, error) {
	if err := ValidateServiceTarget(scope, name); err != nil {
		return UnitConfiguration{}, err
	}
	detail, err := UnitDetails(ctx, scope, name)
	if err != nil {
		return UnitConfiguration{}, err
	}
	path, err := trustedUnitFilePath(scope, detail.Path, name)
	if err != nil {
		return UnitConfiguration{}, err
	}
	if err := ctx.Err(); err != nil {
		return UnitConfiguration{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return UnitConfiguration{}, err
	}
	defer file.Close()
	content, truncated, err := readBoundedUnitConfiguration(file)
	if err != nil {
		return UnitConfiguration{}, err
	}
	return UnitConfiguration{Path: detail.Path, Content: string(content), Truncated: truncated}, nil
}

func readBoundedUnitConfiguration(reader io.Reader) ([]byte, bool, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxUnitConfigurationBytes+1))
	if err != nil {
		return nil, false, err
	}
	truncated := len(content) > maxUnitConfigurationBytes
	if truncated {
		content = content[:maxUnitConfigurationBytes]
	}
	return content, truncated, nil
}

func trustedUnitFilePath(scope, path, name string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Base(path) != name || filepath.Clean(path) != path {
		return "", errUnitConfigurationPath
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errUnitConfigurationPath
	}
	if !filepath.IsAbs(resolved) || filepath.Base(path) != name {
		return "", errUnitConfigurationPath
	}
	if scope == "system" {
		for _, root := range []string{
			"/etc/systemd/system",
			"/run/systemd/system",
			"/run/systemd/generator",
			"/run/systemd/generator.early",
			"/run/systemd/generator.late",
			"/run/systemd/transient",
			"/usr/lib/systemd/system",
			"/usr/local/lib/systemd/system",
			"/lib/systemd/system",
		} {
			if pathWithin(root, resolved) {
				return resolved, nil
			}
		}
		return "", errUnitConfigurationPath
	}
	for _, root := range []string{
		"/etc/systemd/user",
		"/run/systemd/user",
		"/usr/lib/systemd/user",
		"/usr/local/lib/systemd/user",
		"/lib/systemd/user",
	} {
		if pathWithin(root, resolved) {
			return resolved, nil
		}
	}
	if userUnitPath(resolved) {
		return resolved, nil
	}
	return "", errUnitConfigurationPath
}

func pathWithin(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func userUnitPath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(path, "/run/user/") && strings.Contains(path, "/systemd/user/") {
		return true
	}
	for _, marker := range []string{"/.config/systemd/user/", "/.local/share/systemd/user/"} {
		if strings.Contains(path, marker) && strings.HasPrefix(path, "/home/") {
			return true
		}
	}
	return false
}
