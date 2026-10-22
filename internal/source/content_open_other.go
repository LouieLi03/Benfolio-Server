//go:build !linux && !darwin

package source

import (
	"errors"
	"os"
)

func openRegularSource(root *os.Root, name string) (*os.File, error) {
	return nil, errors.New("来源正文读取目前仅支持 Linux 和 macOS")
}
