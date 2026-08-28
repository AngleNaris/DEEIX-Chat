package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepSharedExports(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	for _, name := range []string{"deeix-1-2", "deeix-3-4", "invalid"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	oldFile := filepath.Join(root, "deeix-1-2", "old.txt")
	freshFile := filepath.Join(root, "deeix-1-2", "fresh.txt")
	if err := os.WriteFile(oldFile, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshFile, []byte("fresh"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(oldFile, old, old)
	_ = os.Chtimes(freshFile, fresh, fresh)
	mm := filepath.Join(root, "deeix-1-2", "mm-kept")
	if err := os.Mkdir(mm, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(mm, old, old)
	invalid := filepath.Join(root, "invalid", "kept")
	if err := os.WriteFile(invalid, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	ordinary := filepath.Join(root, "deeix-1-2", "ordinary")
	if err := os.MkdirAll(filepath.Join(ordinary, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(ordinary, old, old)
	symlink := filepath.Join(root, "deeix-1-2", "old-link")
	if err := os.Symlink(filepath.Join(root, "outside"), symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	live := filepath.Join(root, "deeix-3-4", "live-old")
	if err := os.WriteFile(live, []byte("live"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(live, old, old)
	if err := sweepSharedExports(root, now, 7*24*time.Hour, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old export retained: %v", err)
	}
	if _, err := os.Stat(freshFile); err != nil {
		t.Fatalf("fresh export removed: %v", err)
	}
	if _, err := os.Stat(mm); err != nil {
		t.Fatalf("mm directory removed: %v", err)
	}
	if _, err := os.Stat(invalid); err != nil {
		t.Fatalf("invalid scope touched: %v", err)
	}
	if _, err := os.Stat(ordinary); err != nil {
		t.Fatalf("ordinary old directory removed: %v", err)
	}
	if _, err := os.Lstat(symlink); err != nil {
		t.Fatalf("symlink removed or followed: %v", err)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatalf("old live export retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "deeix-3-4")); err != nil {
		t.Fatalf("live scope removed: %v", err)
	}
}

func TestSweepSharedExportsReclaimsEmptyScopesOnlyWithoutLiveSession(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	for _, name := range []string{"deeix-1-2", "deeix-3-4", "deeix-5-6"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"deeix-1-2", "deeix-3-4", "deeix-5-6"} {
		dir := filepath.Join(root, name)
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(dir, old, old)
		_ = info
	}
	liveScopes := func(scope string) bool { return scope == "deeix-1-2" }
	if err := sweepSharedExports(root, now, 7*24*time.Hour, liveScopes); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "deeix-1-2")); err != nil {
		t.Fatalf("live empty scope removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "deeix-3-4")); !os.IsNotExist(err) {
		t.Fatalf("old empty scope retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "deeix-5-6")); !os.IsNotExist(err) {
		t.Fatalf("old empty scope retained: %v", err)
	}

	// 新目录（未超 TTL）即使为空也不回收。
	fresh := filepath.Join(root, "deeix-7-8")
	if err := os.MkdirAll(fresh, 0755); err != nil {
		t.Fatal(err)
	}
	if err := sweepSharedExports(root, now, 7*24*time.Hour, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh empty scope removed: %v", err)
	}
}

func TestSweepSharedExportsAggregatesDeletionErrors(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	scope := filepath.Join(root, "deeix-1-2")
	if err := os.MkdirAll(scope, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(scope, "stale.txt")
	if err := os.WriteFile(stale, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(stale, old, old)
	originalRemove := removeSharedExport
	removeSharedExport = func(path string) error {
		if path == stale {
			return os.ErrPermission
		}
		return originalRemove(path)
	}
	t.Cleanup(func() { removeSharedExport = originalRemove })
	if err := sweepSharedExports(root, now, 7*24*time.Hour, nil); err == nil {
		t.Fatal("sweepSharedExports() error = nil, want aggregated removal error")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("sweepSharedExports() error = %v, want permission error", err)
	}
}
