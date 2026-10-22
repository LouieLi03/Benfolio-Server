//go:build linux || darwin

package source

import (
	"os"
	"syscall"
)

func openRegularSource(root *os.Root, name string) (*os.File, error) {
	// A replaced FIFO must not block open; the final component cannot be a link.
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
