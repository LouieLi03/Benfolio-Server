//go:build !linux && !darwin

package localfile

import (
	"errors"
	"os"
)

func OpenRegular(root *os.Root, name string) (*os.File, error) {
	return nil, errors.New("本地原书文件读取目前仅支持 Linux 和 macOS")
}
