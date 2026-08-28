package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepMMOutputs(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	scope := filepath.Join(root, "deeix-1-2")
	if err := os.MkdirAll(scope, 0755); err != nil {
		t.Fatal(err)
	}
	oldMM := filepath.Join(scope, "mm-output-0123456789ab")
	freshMM := filepath.Join(scope, "mm-output-abcdef012345")
	nonMM := filepath.Join(scope, "sandbox")
	userCreatedMM := filepath.Join(scope, "mm-user-created")
	for _, dir := range []string{oldMM, freshMM, nonMM, userCreatedMM} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	nestedOutside := filepath.Join(root, "outside")
	if err := os.WriteFile(nestedOutside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nestedOutside, filepath.Join(oldMM, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_ = os.Chtimes(oldMM, old, old)
	_ = os.Chtimes(userCreatedMM, old, old)
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(freshMM, "nested-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	tmp := filepath.Join(root, ".tmp-mm-0123456789abcdef01234567")
	fakeTmp := filepath.Join(root, ".tmp-mm-user-created")
	if err := os.Mkdir(tmp, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fakeTmp, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(tmp, old, old)
	_ = os.Chtimes(fakeTmp, old, old)
	invalid := filepath.Join(root, "deeix-nope")
	if err := os.Mkdir(invalid, 0755); err != nil {
		t.Fatal(err)
	}
	if err := sweepMMOutputs(root, now, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldMM); !os.IsNotExist(err) {
		t.Fatalf("old MM output retained: %v", err)
	}
	if _, err := os.Stat(freshMM); err != nil {
		t.Fatalf("fresh MM output removed: %v", err)
	}
	if _, err := os.Stat(nonMM); err != nil {
		t.Fatalf("non-MM directory removed: %v", err)
	}
	if _, err := os.Stat(userCreatedMM); err != nil {
		t.Fatalf("user-created mm-prefixed directory removed: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("old temp retained: %v", err)
	}
	if _, err := os.Stat(fakeTmp); err != nil {
		t.Fatalf("user-created temp-prefixed directory removed: %v", err)
	}
	if _, err := os.Stat(invalid); err != nil {
		t.Fatalf("invalid root touched: %v", err)
	}
	if data, err := os.ReadFile(nestedOutside); err != nil || string(data) != "outside" {
		t.Fatalf("nested symlink target changed: %q %v", data, err)
	}
}

func TestSweepMMOutputsDisabledWithoutOutput(t *testing.T) {
	if err := sweepMMOutputs("", time.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestSweepMMOutputsAggregatesDeletionErrors(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	scope := filepath.Join(root, "deeix-1-2")
	if err := os.MkdirAll(scope, 0755); err != nil {
		t.Fatal(err)
	}
	oldMM := filepath.Join(scope, "mm-output-0123456789ab")
	if err := os.Mkdir(oldMM, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(oldMM, old, old)
	originalRemoveAll := removeMMOutputAll
	removeMMOutputAll = func(path string) error {
		if path == oldMM {
			return os.ErrPermission
		}
		return originalRemoveAll(path)
	}
	t.Cleanup(func() { removeMMOutputAll = originalRemoveAll })
	if err := sweepMMOutputs(root, now, 24*time.Hour); err == nil {
		t.Fatal("sweepMMOutputs() error = nil, want aggregated removal error")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("sweepMMOutputs() error = %v, want permission error", err)
	}
}
