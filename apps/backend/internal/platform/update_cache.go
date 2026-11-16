package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const updateStatusCacheTTL = 15 * time.Second

// Providers opt in with a signature of repository metadata, package database,
// and relevant configuration. The TTL also bounds changes not captured by it.
type updateCacheKeyProvider interface{ UpdateCacheKey() string }

// UpdateFilesFingerprint records deletions, additions, sizes and nanosecond
// mtimes. Including both a database and its WAL catches external transactions.
func UpdateFilesFingerprint(patterns ...string) string {
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
	hash := sha256.New()
	for _, path := range ordered {
		if info, err := os.Stat(path); err == nil {
			_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%d\n", path, info.Size(), info.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (service *UpdateService) invalidateUpdateCache() {
	service.cacheMu.Lock()
	service.cacheGeneration++
	service.cacheJSON = nil
	service.cacheMu.Unlock()
}

func (service *UpdateService) Status(ctx context.Context) UpdateStatus {
	if service == nil || service.provider == nil {
		return unavailableUpdateStatus("No distro update provider was configured")
	}
	provider, cached := service.provider.(updateCacheKeyProvider)
	if !cached {
		return service.readStatus(ctx)
	}
	for {
		if err := ctx.Err(); err != nil {
			return unavailableUpdateStatus(err.Error())
		}
		key := provider.UpdateCacheKey()
		service.cacheMu.Lock()
		if len(service.cacheJSON) > 0 && key == service.cacheKey && time.Now().Before(service.cacheUntil) {
			// The JSON buffer is immutable after publication. Decoding isolates
			// returned slices from concurrent readers and from callers' mutations.
			data := service.cacheJSON
			service.cacheMu.Unlock()
			var status UpdateStatus
			if json.Unmarshal(data, &status) != nil {
				service.invalidateUpdateCache()
				continue
			}
			deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
			status.ExternalLock, status.LockReason = service.provider.LockStatus(deadline)
			cancel()
			status.Fingerprint = UpdateFingerprint(status)
			return status
		}
		if loading := service.cacheLoading; loading != nil {
			service.cacheMu.Unlock()
			select {
			case <-loading:
				continue
			case <-ctx.Done():
				return unavailableUpdateStatus(ctx.Err().Error())
			}
		}
		loading := make(chan struct{})
		service.cacheLoading = loading
		generation := service.cacheGeneration
		service.cacheMu.Unlock()
		status := service.readStatus(ctx)
		endKey := provider.UpdateCacheKey()
		data, err := json.Marshal(status)
		service.cacheMu.Lock()
		if status.Available && ctx.Err() == nil && err == nil && key == endKey && generation == service.cacheGeneration {
			service.cacheJSON, service.cacheKey, service.cacheUntil = data, key, time.Now().Add(updateStatusCacheTTL)
		}
		service.cacheLoading = nil
		close(loading)
		service.cacheMu.Unlock()
		return status
	}
}
