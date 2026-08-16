package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	mmScopePattern      = regexp.MustCompile(`^deeix-[0-9]+-[0-9]+$`)
	mmOutputPattern     = regexp.MustCompile(`^mm-[A-Za-z0-9_.-]+-[0-9a-f]{12}$`)
	mmOutputTempPattern = regexp.MustCompile(`^\.tmp-mm-[0-9a-f]{24}$`)
)

const (
	defaultMMSweepTTL      = 24 * time.Hour
	defaultMMSweepInterval = time.Hour
)

func sweepMMOutputs(root string, now time.Time, ttl time.Duration) error {
	if strings.TrimSpace(root) == "" || root == "." || ttl <= 0 {
		return nil
	}
	var sweepErr error
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			sweepErr = errors.Join(sweepErr, fmt.Errorf("stat %s: %w", entry.Name(), err))
			continue
		}
		if !info.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "deeix-") {
			if mmScopePattern.MatchString(name) {
				sweepErr = errors.Join(sweepErr, sweepMMOutputScope(filepath.Join(root, name), now, ttl))
			}
			continue
		}
		if mmOutputTempPattern.MatchString(name) && now.Sub(info.ModTime()) > ttl {
			if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
				sweepErr = errors.Join(sweepErr, fmt.Errorf("remove temp %s: %w", name, err))
			}
		}
	}
	return sweepErr
}

func sweepMMOutputScope(scope string, now time.Time, ttl time.Duration) error {
	var sweepErr error
	entries, err := os.ReadDir(scope)
	if err != nil {
		return fmt.Errorf("read scope %s: %w", filepath.Base(scope), err)
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !mmOutputPattern.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			sweepErr = errors.Join(sweepErr, fmt.Errorf("stat %s: %w", entry.Name(), err))
			continue
		}
		if info.IsDir() && now.Sub(info.ModTime()) > ttl {
			if err := os.RemoveAll(filepath.Join(scope, entry.Name())); err != nil {
				sweepErr = errors.Join(sweepErr, fmt.Errorf("remove output %s: %w", entry.Name(), err))
			}
		}
	}
	return sweepErr
}
