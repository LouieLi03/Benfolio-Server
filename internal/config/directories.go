package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Validate the configured mount before any state directories are created. This
// checks directory listing access, not whether every book is readable or valid.
func readableSourceDirectory(sourcePath, statePath string) (string, error) {
	resolvedSource, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return "", errors.New("无法解析书籍来源目录，请检查目录是否存在及挂载权限")
	}
	resolvedState, err := resolveStateDirectory(statePath)
	if err != nil {
		return "", errors.New("无法解析状态目录，请检查父目录与符号链接")
	}
	if contains(resolvedState, resolvedSource) || contains(resolvedSource, resolvedState) {
		return "", errors.New("状态目录与书籍来源目录不能通过符号链接互相包含")
	}
	directory, err := os.Open(resolvedSource)
	if err != nil {
		return "", errors.New("无法打开书籍来源目录，请检查读取权限")
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		return "", errors.New("书籍来源路径必须是可读取的目录")
	}
	// Read at most one name, without opening book contents or walking subfolders.
	if _, err := directory.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return "", errors.New("无法列举书籍来源目录，请检查目录读取权限")
	}
	return resolvedSource, nil
}

// The state directory may not exist yet. Resolve its nearest existing ancestor
// and append missing components without creating files or following broken links.
func resolveStateDirectory(path string) (string, error) {
	current := filepath.Clean(path)
	var missing []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				return "", errors.New("状态路径的已有部分必须是目录")
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
