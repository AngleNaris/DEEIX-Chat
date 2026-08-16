//go:build !linux

package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func openExportRegularFile(root, rel string) (*os.File, int64, error) {
	clean := filepath.Clean(rel)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, 0, fmt.Errorf("export is outside shared root")
	}
	scopedRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, 0, err
	}
	defer scopedRoot.Close()
	file, err := scopedRoot.Open(clean)
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
		return nil, 0, fmt.Errorf("export is not a regular file")
	}
	return file, info.Size(), nil
}
