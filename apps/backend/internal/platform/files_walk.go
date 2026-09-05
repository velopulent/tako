package platform

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
)

// Read directory entries in bounded batches, including trees with no matches.
func (f fileTree) walk(ctx context.Context, root string, visit fs.WalkDirFunc) error {
	remaining := MaxSearchEntries
	var walk func(string, int) error
	walk = func(path string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if remaining == 0 || depth > MaxArchiveDepth {
			return ErrArchiveLimit
		}
		remaining--
		info, err := f.root.Lstat(path)
		if err != nil {
			return visit(path, nil, err)
		}
		entry := fs.FileInfoToDirEntry(info)
		if err := visit(path, entry, nil); err != nil {
			if errors.Is(err, fs.SkipDir) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return nil
		}
		directory, err := f.root.Open(path)
		if err != nil {
			return visit(path, entry, err)
		}
		defer directory.Close()
		for {
			entries, err := directory.ReadDir(128)
			for _, entry := range entries {
				if err := walk(filepath.Join(path, entry.Name()), depth+1); err != nil {
					return err
				}
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	err := walk(root, 0)
	if errors.Is(err, fs.SkipAll) {
		return nil
	}
	return err
}
