package platform

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Keep parent descriptors open through mutations. No path component may redirect
// an administrative operation through a symlink or a procfs magic link.
type fileRoot struct{ directory *os.File }

func openFileRoot(path string) (*fileRoot, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		file.Close()
		if err == nil {
			err = ErrFilePermission
		}
		return nil, err
	}
	return &fileRoot{file}, nil
}
func (r *fileRoot) Close() error { return r.directory.Close() }
func (r *fileRoot) Open(name string) (*os.File, error) {
	return r.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
func (r *fileRoot) OpenFile(name string, flags int, mode os.FileMode) (*os.File, error) {
	if !filepath.IsLocal(name) {
		return nil, ErrFilePermission
	}
	creationMode := uint64(0)
	if flags&os.O_CREATE != 0 {
		creationMode = uint64(mode.Perm())
	}
	fd, err := unix.Openat2(int(r.directory.Fd()), name, &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC), Mode: creationMode, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}
func (r *fileRoot) Stat(name string) (os.FileInfo, error) {
	f, e := r.OpenFile(name, unix.O_PATH, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return f.Stat()
}
func (r *fileRoot) Lstat(name string) (os.FileInfo, error) {
	f, e := r.OpenFile(name, unix.O_PATH|unix.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return f.Stat()
}
func (r *fileRoot) OpenRoot(name string) (*fileRoot, error) {
	f, e := r.OpenFile(name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if e != nil {
		return nil, e
	}
	return &fileRoot{f}, nil
}
func (r *fileRoot) parent(name string) (*fileRoot, string, error) {
	if name == "." || !filepath.IsLocal(name) {
		return nil, "", ErrFilePermission
	}
	parent, err := r.OpenRoot(filepath.Dir(name))
	return parent, filepath.Base(name), err
}
func (r *fileRoot) Mkdir(name string, mode os.FileMode) error {
	p, b, e := r.parent(name)
	if e != nil {
		return e
	}
	defer p.Close()
	return unix.Mkdirat(int(p.directory.Fd()), b, uint32(mode.Perm()))
}
func (r *fileRoot) MkdirAll(name string, mode os.FileMode) error {
	if !filepath.IsLocal(name) {
		return ErrFilePermission
	}
	current := "."
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		e := r.Mkdir(current, mode)
		if e != nil && !os.IsExist(e) {
			return e
		}
		directory, e := r.OpenRoot(current)
		if e != nil {
			return e
		}
		directory.Close()
	}
	return nil
}
func (r *fileRoot) Remove(name string) error {
	p, b, e := r.parent(name)
	if e != nil {
		return e
	}
	defer p.Close()
	e = unix.Unlinkat(int(p.directory.Fd()), b, 0)
	if e == unix.EISDIR {
		e = unix.Unlinkat(int(p.directory.Fd()), b, unix.AT_REMOVEDIR)
	}
	return e
}
func (r *fileRoot) RemoveAll(name string) error {
	p, b, e := r.parent(name)
	if e != nil {
		return e
	}
	defer p.Close()
	// os.Root.RemoveAll never follows symlinks in the recursively removed tree.
	root, e := os.OpenRoot("/proc/self/fd/" + strconv.Itoa(int(p.directory.Fd())))
	if e != nil {
		return e
	}
	defer root.Close()
	return root.RemoveAll(b)
}
func (r *fileRoot) rename(source, destination string, noReplace bool) error {
	a, an, e := r.parent(source)
	if e != nil {
		return e
	}
	defer a.Close()
	b, bn, e := r.parent(destination)
	if e != nil {
		return e
	}
	defer b.Close()
	flags := uint(0)
	if noReplace {
		flags = unix.RENAME_NOREPLACE
	}
	return unix.Renameat2(int(a.directory.Fd()), an, int(b.directory.Fd()), bn, flags)
}
func (r *fileRoot) Rename(a, b string) error { return r.rename(a, b, false) }
func (r *fileRoot) Readlink(name string) (string, error) {
	p, b, e := r.parent(name)
	if e != nil {
		return "", e
	}
	defer p.Close()
	buffer := make([]byte, MaxFilePath+1)
	n, e := unix.Readlinkat(int(p.directory.Fd()), b, buffer)
	if e != nil {
		return "", e
	}
	if n > MaxFilePath {
		return "", ErrFileTooLarge
	}
	return string(buffer[:n]), nil
}

type rootedFiles struct{ root *fileRoot }

func (f rootedFiles) Open(name string) (fs.File, error) { return f.root.Open(name) }
func (r *fileRoot) FS() fs.FS                           { return rootedFiles{r} }
