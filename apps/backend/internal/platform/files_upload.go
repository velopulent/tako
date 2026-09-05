package platform

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type uploadRecord struct {
	Path        string `json:"path"`
	Total       int64  `json:"total"`
	Fingerprint string `json:"fingerprint"`
}

func uploadNames(path, id string) (string, string) {
	data := filepath.Join(filepath.Dir(path), ".tako-upload-"+id)
	return data, data + ".json"
}
func (f fileTree) writeChunk(path string, operation FileOperation) (FileResult, error) {
	digest := sha256.Sum256(operation.Content)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), operation.ContentSHA256) {
		return FileResult{}, ErrFileConflict
	}
	id := operation.UploadID
	if id == "" {
		if operation.Offset != 0 {
			return FileResult{}, ErrFileConflict
		}
		var value [16]byte
		if _, err := rand.Read(value[:]); err != nil {
			return FileResult{}, err
		}
		id = hex.EncodeToString(value[:])
	}
	data, metadata := uploadNames(path, id)
	flags := os.O_RDWR
	if operation.UploadID == "" {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := f.root.OpenFile(data, flags, 0600)
	if err != nil {
		return FileResult{}, err
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return FileResult{}, ErrFileConflict
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	info, err := file.Stat()
	if err != nil {
		return FileResult{}, err
	}
	if !info.Mode().IsRegular() {
		return FileResult{}, ErrFilePermission
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return FileResult{}, ErrFilePermission
	}
	record := uploadRecord{Path: path, Total: operation.TotalSize, Fingerprint: operation.ExpectedFingerprint}
	if operation.UploadID == "" {
		if current, e := f.root.Stat(path); e == nil {
			if record.Fingerprint == "" || statFingerprint(current) != record.Fingerprint {
				f.root.Remove(data)
				return FileResult{}, ErrFileConflict
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			f.root.Remove(data)
			return FileResult{}, e
		}
		meta, e := f.root.OpenFile(metadata, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			f.root.Remove(data)
			return FileResult{}, e
		}
		e = json.NewEncoder(meta).Encode(record)
		if e == nil {
			e = meta.Sync()
		}
		meta.Close()
		if e != nil {
			f.root.Remove(data)
			f.root.Remove(metadata)
			return FileResult{}, e
		}
	} else {
		meta, e := f.root.Open(metadata)
		if e != nil {
			return FileResult{}, e
		}
		e = json.NewDecoder(io.LimitReader(meta, 8192)).Decode(&record)
		meta.Close()
		if e != nil || record.Path != path || record.Total != operation.TotalSize {
			return FileResult{}, ErrFileConflict
		}
	}
	if info.Size() != operation.Offset {
		// A retry may acknowledge bytes already committed, but never overwrite them.
		if operation.Offset+int64(len(operation.Content)) > info.Size() {
			return FileResult{}, ErrFileConflict
		}
		previous := make([]byte, len(operation.Content))
		if _, err := file.ReadAt(previous, operation.Offset); err != nil || !bytes.Equal(previous, operation.Content) {
			return FileResult{}, ErrFileConflict
		}
		return FileResult{UploadID: id, Offset: info.Size(), Total: record.Total}, nil
	}
	if _, err := file.WriteAt(operation.Content, operation.Offset); err != nil {
		return FileResult{}, err
	}
	if err := file.Sync(); err != nil {
		return FileResult{}, err
	}
	next := operation.Offset + int64(len(operation.Content))
	result := FileResult{UploadID: id, Offset: next, Total: record.Total}
	if next == record.Total {
		if record.Fingerprint != "" {
			current, e := f.root.Stat(path)
			if e != nil || statFingerprint(current) != record.Fingerprint {
				return FileResult{}, ErrFileConflict
			}
			if e := f.preserveMetadata(data, current); e != nil {
				return FileResult{}, e
			}
		}
		if err := f.root.rename(data, path, record.Fingerprint == ""); err != nil {
			return FileResult{}, err
		}
		f.root.Remove(metadata)
		entry, err := f.entry(path, path)
		if err != nil {
			return FileResult{}, err
		}
		result.Entry = &entry
		result.Fingerprint = entry.Fingerprint
		result.EOF = true
	}
	return result, nil
}
func (f fileTree) cancelUpload(path string, operation FileOperation) (FileResult, error) {
	if operation.UploadID == "" {
		return FileResult{}, ErrInvalidFileOperation
	}
	data, metadata := uploadNames(path, operation.UploadID)
	file, err := f.root.OpenFile(data, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return FileResult{Message: "Upload already removed."}, nil
	}
	if err != nil {
		return FileResult{}, err
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return FileResult{}, ErrFileConflict
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	if err := f.root.Remove(data); err != nil {
		return FileResult{}, err
	}
	f.root.Remove(metadata)
	return FileResult{Message: "Upload cancelled."}, nil
}
