package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

type Layout struct {
	Root      string
	Database  string
	Originals string
	Working   string
	Resources string
	Staging   string
	Exports   string
}

// OpenLayout touches only the application's state directory, never the source
// book mount. No cleanup or migration of existing user files is performed.
func OpenLayout(root string) (Layout, error) {
	layout := Layout{
		Root: root, Database: filepath.Join(root, "benfolio.sqlite"),
		Originals: filepath.Join(root, "originals"), Working: filepath.Join(root, "working"),
		Resources: filepath.Join(root, "resources"), Staging: filepath.Join(root, "staging"),
		Exports: filepath.Join(root, "exports"),
	}
	for _, dir := range []string{layout.Root, layout.Originals, layout.Working, layout.Resources, layout.Staging, layout.Exports} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Layout{}, fmt.Errorf("创建状态目录: %w", err)
		}
	}
	return layout, nil
}
