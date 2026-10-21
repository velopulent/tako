// Package fdpass creates Unix socket pairs and passes open files between
// Tako processes with SCM_RIGHTS. Sessiond uses it to hand sockets to the
// sign-in helper and the user bridge without any filesystem path.
package fdpass

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// MaxFiles bounds how many files one message may carry.
const MaxFiles = 4

// Pair returns both ends of a Unix socket pair of kind (unix.SOCK_STREAM or
// unix.SOCK_SEQPACKET). Both are close-on-exec; pass an end to a child
// through exec.Cmd, which clears the flag on the child's copy.
func Pair(kind int) (*os.File, *os.File, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, kind|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(fds[0]), "tako-socket"), os.NewFile(uintptr(fds[1]), "tako-socket-peer"), nil
}

// Conn wraps a socket file as a connection and closes the file, which the
// connection has duplicated.
func Conn(file *os.File) (*net.UnixConn, error) {
	defer file.Close()
	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("not a Unix socket")
	}
	return unixConn, nil
}

// Send writes one message carrying files. The caller keeps its own copies of
// the files and closes them when it no longer needs them.
func Send(conn *net.UnixConn, payload []byte, files ...*os.File) error {
	if len(files) > MaxFiles {
		return fmt.Errorf("at most %d files per message", MaxFiles)
	}
	var rights []byte
	if len(files) > 0 {
		fds := make([]int, len(files))
		for index, file := range files {
			fds[index] = int(file.Fd())
		}
		rights = unix.UnixRights(fds...)
	}
	count, _, err := conn.WriteMsgUnix(payload, rights, nil)
	if err == nil && count != len(payload) {
		err = io.ErrShortWrite
	}
	return err
}

// Receive reads one message of at most limit bytes and the files it carries.
// A closed peer reads as io.EOF. Truncated messages are rejected, and their
// files closed, so a peer cannot smuggle data past the limit.
func Receive(conn *net.UnixConn, limit int) ([]byte, []*os.File, error) {
	payload := make([]byte, limit)
	control := make([]byte, unix.CmsgSpace(MaxFiles*4))
	count, controlCount, flags, _, err := conn.ReadMsgUnix(payload, control)
	files := receivedFiles(control[:controlCount])
	if err == nil && count == 0 && len(files) == 0 {
		err = io.EOF
	}
	if err == nil && flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		err = errors.New("message exceeds the size limit")
	}
	if err != nil {
		CloseAll(files)
		return nil, nil, err
	}
	return payload[:count], files, nil
}

func receivedFiles(control []byte) []*os.File {
	messages, err := unix.ParseSocketControlMessage(control)
	if err != nil {
		return nil
	}
	var files []*os.File
	for index := range messages {
		fds, err := unix.ParseUnixRights(&messages[index])
		if err != nil {
			continue
		}
		for _, fd := range fds {
			unix.CloseOnExec(fd)
			files = append(files, os.NewFile(uintptr(fd), "tako-received"))
		}
	}
	return files
}

// CloseAll closes every file in files.
func CloseAll(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}
