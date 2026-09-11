package platform

import (
	"bytes"
	"testing"
)

func FuzzValidateFileOperation(f *testing.F) {
	f.Add("read", "notes.txt", "", "", "", "", "", "", int64(0), int64(4096), int64(0), 100, 100, 100, false, false, false)
	f.Add("archive", "source", "", "archive.tar.gz", "", "", "", "", int64(0), int64(0), int64(0), 0, 0, 0, false, false, false)
	f.Fuzz(func(t *testing.T, action, path, destination, archivePath, query, content, contentSHA, expectedFingerprint string, offset, limit, totalSize int64, lineOffset, lineLimit, maxEntries int, overwrite, recursive, permanent bool) {
		_ = ValidateFileOperation(FileOperation{
			Action:              action,
			Path:                path,
			Destination:         destination,
			ArchivePath:         archivePath,
			Query:               query,
			Content:             []byte(content),
			ContentSHA256:       contentSHA,
			ExpectedFingerprint: expectedFingerprint,
			Offset:              offset,
			Limit:               limit,
			TotalSize:           totalSize,
			LineOffset:          lineOffset,
			LineLimit:           lineLimit,
			MaxEntries:          maxEntries,
			Overwrite:           overwrite,
			Recursive:           recursive,
			Permanent:           permanent,
		})
	})
}

func FuzzSafeArchiveName(f *testing.F) {
	for _, seed := range []string{"file.txt", "nested/file.txt", "../escape", "/absolute", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, name string) {
		_ = safeArchiveName(name)
	})
}

func FuzzReadBounded(f *testing.F) {
	f.Add([]byte("small output"), uint8(64))
	f.Add([]byte("oversized output"), uint8(4))
	f.Fuzz(func(_ *testing.T, payload []byte, limit uint8) {
		_, _ = readBounded(bytes.NewReader(payload), int64(limit))
	})
}

func FuzzParseLogEntry(f *testing.F) {
	f.Add([]byte(`{"__REALTIME_TIMESTAMP":"1704067200000000","MESSAGE":"ready","__CURSOR":"s=1"}`), true)
	f.Add([]byte(`not-json`), false)
	f.Fuzz(func(_ *testing.T, payload []byte, details bool) {
		_, _ = parseLogEntryWithDetails(payload, details)
	})
}

func FuzzValidateUpdateOperation(f *testing.F) {
	f.Add("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "job-1", true, true)
	f.Add("", "", false, false)
	f.Fuzz(func(_ *testing.T, fingerprint, jobID string, confirmed, applying bool) {
		_ = ValidateUpdateOperation(UpdateOperation{
			ExpectedFingerprint: fingerprint,
			JobID:               jobID,
			Confirmed:           confirmed,
		}, applying)
	})
}

func FuzzValidateTimerOperation(f *testing.F) {
	f.Add("preview", "user", "nightly", "*-*-* 03:00:00", "/bin/true")
	f.Add("delete", "system", "", "", "")
	f.Fuzz(func(_ *testing.T, action, scope, name, schedule, command string) {
		_ = ValidateTimerOperation(TimerOperation{
			Action:     action,
			Scope:      scope,
			Name:       name,
			OnCalendar: schedule,
			Command:    command,
		})
	})
}
