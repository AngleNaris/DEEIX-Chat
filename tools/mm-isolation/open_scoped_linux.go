//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type scopedOutputDir struct {
	rootFD    int
	scopeFD   int
	tempFD    int
	root      string
	tempName  string
	finalPath string
	committed bool
}

func openScopedRegularFile(root, rel string) (*os.File, int64, error) {
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, 0, err
	}
	defer unix.Close(rootFD)

	fd, err := unix.Openat2(rootFD, rel, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, 0, err
	}
	file := os.NewFile(uintptr(fd), rel)
	if file == nil {
		_ = unix.Close(fd)
		return nil, 0, fmt.Errorf("open input file")
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
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	closeRoot := true
	defer func() {
		if closeRoot {
			_ = unix.Close(rootFD)
		}
	}()

	if err := unix.Mkdirat(rootFD, scope, 0755); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, err
	}
	scopeFD, err := unix.Openat2(rootFD, scope, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, err
	}
	closeScope := true
	defer func() {
		if closeScope {
			_ = unix.Close(scopeFD)
		}
	}()

	var tempName string
	for attempt := 0; attempt < 8; attempt++ {
		name, nameErr := randomOutputTempName()
		if nameErr != nil {
			return nil, nameErr
		}
		if err := unix.Mkdirat(rootFD, name, 0755); err == nil {
			tempName = name
			break
		} else if !errors.Is(err, unix.EEXIST) {
			return nil, err
		}
	}
	if tempName == "" {
		return nil, fmt.Errorf("create output staging directory")
	}
	tempFD, err := unix.Openat2(rootFD, tempName, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		_ = os.RemoveAll(filepath.Join(root, tempName))
		return nil, err
	}
	closeRoot = false
	closeScope = false
	return &scopedOutputDir{
		rootFD:    rootFD,
		scopeFD:   scopeFD,
		tempFD:    tempFD,
		root:      root,
		tempName:  tempName,
		finalPath: filepath.Join(root, scope, finalName),
	}, nil
}

func (d *scopedOutputDir) CreateFile(name string) (*os.File, error) {
	if d == nil || name == "" || filepath.Base(name) != name {
		return nil, fmt.Errorf("invalid output file name")
	}
	fd, err := unix.Openat(d.tempFD, name, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0644)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("create output file")
	}
	return file, nil
}

func (d *scopedOutputDir) Commit(finalName string) (string, error) {
	if d == nil || finalName == "" || filepath.Base(finalName) != finalName {
		return "", fmt.Errorf("invalid output directory name")
	}
	if err := unix.Renameat2(d.rootFD, d.tempName, d.scopeFD, finalName, unix.RENAME_NOREPLACE); err != nil {
		return "", err
	}
	d.committed = true
	return d.finalPath, nil
}

func (d *scopedOutputDir) Cleanup() {
	if d == nil {
		return
	}
	if d.tempFD >= 0 {
		_ = unix.Close(d.tempFD)
		d.tempFD = -1
	}
	if d.scopeFD >= 0 {
		_ = unix.Close(d.scopeFD)
		d.scopeFD = -1
	}
	if d.rootFD >= 0 {
		_ = unix.Close(d.rootFD)
		d.rootFD = -1
	}
	if !d.committed {
		_ = os.RemoveAll(filepath.Join(d.root, d.tempName))
	}
}

func randomOutputTempName() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return ".tmp-mm-" + hex.EncodeToString(value[:]), nil
}
