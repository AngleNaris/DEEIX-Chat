//go:build !linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type scopedOutputDir struct {
	tempPath  string
	finalPath string
	committed bool
}

// Non-Linux builds are used for local tests only. Production uses openat2 on Linux.
func openScopedRegularFile(root, rel string) (*os.File, int64, error) {
	clean := filepath.Clean(rel)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, 0, fmt.Errorf("input is outside root")
	}
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, 0, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, 0, fmt.Errorf("symlink input is not allowed")
		}
	}
	file, err := os.Open(current)
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, 0, fmt.Errorf("input is not a regular file")
	}
	return file, info.Size(), nil
}

func createScopedOutputDir(root, scope, finalName string) (*scopedOutputDir, error) {
	if err := ensureDirectoryWithoutSymlinks(root, scope); err != nil {
		return nil, err
	}
	tempPath, err := os.MkdirTemp(root, ".tmp-mm-")
	if err != nil {
		return nil, err
	}
	return &scopedOutputDir{
		tempPath:  tempPath,
		finalPath: filepath.Join(root, scope, finalName),
	}, nil
}

func ensureDirectoryWithoutSymlinks(root, rel string) error {
	clean := filepath.Clean(rel)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("output scope is outside root")
	}
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0755); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output scope is not a regular directory")
		}
	}
	return nil
}

func (d *scopedOutputDir) CreateFile(name string) (*os.File, error) {
	if d == nil || name == "" || filepath.Base(name) != name {
		return nil, fmt.Errorf("invalid output file name")
	}
	return os.OpenFile(filepath.Join(d.tempPath, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
}

func (d *scopedOutputDir) Commit(finalName string) (string, error) {
	if d == nil || finalName == "" || filepath.Base(finalName) != finalName {
		return "", fmt.Errorf("invalid output directory name")
	}
	if _, err := os.Lstat(d.finalPath); !os.IsNotExist(err) {
		if err == nil {
			return "", fmt.Errorf("output directory already exists")
		}
		return "", err
	}
	if err := os.Rename(d.tempPath, d.finalPath); err != nil {
		return "", err
	}
	d.committed = true
	return d.finalPath, nil
}

func (d *scopedOutputDir) Cleanup() {
	if d != nil && !d.committed {
		_ = os.RemoveAll(d.tempPath)
	}
}
