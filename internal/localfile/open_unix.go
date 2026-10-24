//go:build linux || darwin

// Package localfile opens ordinary local files without following a final
// symbolic link or blocking on a substituted FIFO during open.
package localfile

import (
	"os"
	"syscall"
)

func OpenRegular(root *os.Root, name string) (*os.File, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, os.ErrInvalid
	}
	return f, nil
}
