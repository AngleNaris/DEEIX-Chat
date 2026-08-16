//go:build linux

package conversation

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openExportRegularFile(root, rel string) (*os.File, int64, error) {
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
		return nil, 0, fmt.Errorf("open export file")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, 0, fmt.Errorf("export is not a regular file")
	}
	return file, info.Size(), nil
}
