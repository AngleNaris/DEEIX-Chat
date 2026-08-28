package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSessionDocker struct {
	mu sync.Mutex

	inspectFn func(context.Context, string) (*inspectedContainerConfig, bool, error)
	createFn  func(context.Context, containerSpec, string) error
	removeFn  func(context.Context, string) error
	execFn    func(context.Context, string, []string) (*execResult, error)

	removeCalls  int
	execCommands [][]string
	events       []string
}

func (f *fakeSessionDocker) inspectContainerConfig(ctx context.Context, name string) (*inspectedContainerConfig, bool, error) {
	if f.inspectFn != nil {
		return f.inspectFn(ctx, name)
	}
	return nil, false, nil
}

func (f *fakeSessionDocker) createContainer(ctx context.Context, spec containerSpec, volumeName string) error {
	if f.createFn != nil {
		return f.createFn(ctx, spec, volumeName)
	}
	return nil
}

func (f *fakeSessionDocker) renameContainer(context.Context, string, string) error {
	return nil
}

func (f *fakeSessionDocker) removeContainer(ctx context.Context, name string) error {
	f.mu.Lock()
	f.removeCalls++
	f.events = append(f.events, "remove")
	f.mu.Unlock()
	if f.removeFn != nil {
		return f.removeFn(ctx, name)
	}
	return nil
}

func (f *fakeSessionDocker) removeVolume(string) error {
	return nil
}

func (f *fakeSessionDocker) execInContainer(ctx context.Context, name string, cmd []string, _ string, _ []byte, _ time.Duration, _ int) (*execResult, error) {
	f.mu.Lock()
	f.execCommands = append(f.execCommands, append([]string(nil), cmd...))
	f.events = append(f.events, "exec")
	f.mu.Unlock()
	if f.execFn != nil {
		return f.execFn(ctx, name, cmd)
	}
	return &execResult{}, nil
}

func testSessionManagerConfig(t *testing.T) *Config {
	t.Helper()
	root := t.TempDir()
	return &Config{
		BaseImage:        "sandbox:test",
		WorkspaceDir:     "/workspace",
		CacheVolume:      "cache",
		SharedHostDir:    filepath.Join(root, "shared"),
		SharedMountDir:   "/shared",
		ImportsHostDir:   filepath.Join(root, "imports"),
		ImportsMountDir:  "/imports",
		OutputLimitBytes: 64 << 10,
	}
}

func readySession(scope string, activeOps int) *Session {
	ready := make(chan struct{})
	close(ready)
	return &Session{
		Scope:      scope,
		Image:      "sandbox:test",
		Container:  scope,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		ready:      ready,
		activeOps:  activeOps,
		tasks:      make(map[string]*BackgroundTask),
	}
}

func waitForManagerStopping(t *testing.T, manager *SessionManager) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		stopping := manager.stopping
		manager.mu.Unlock()
		if stopping {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("session manager did not enter stopping state")
}

func TestShutdownRejectsAdmissionAndWaitsForActiveOperation(t *testing.T) {
	docker := &fakeSessionDocker{}
	manager := NewSessionManager(testSessionManagerConfig(t), docker)
	session := readySession("deeix-42-7", 1)
	manager.live[session.Scope] = session
	release := releaseSessionOperation(session)

	shutdownDone := make(chan struct{})
	go func() {
		manager.Shutdown(context.Background())
		close(shutdownDone)
	}()
	waitForManagerStopping(t, manager)

	if _, _, _, err := manager.GetOrCreate(context.Background(), "deeix-42-8"); !errors.Is(err, errSessionManagerShuttingDown) {
		t.Fatalf("new admission error = %v, want shutdown error", err)
	}
	time.Sleep(20 * time.Millisecond)
	docker.mu.Lock()
	removeBeforeRelease := docker.removeCalls
	docker.mu.Unlock()
	if removeBeforeRelease != 0 {
		t.Fatalf("container removed while operation was active: %d", removeBeforeRelease)
	}

	release()
	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after active operation released")
	}
	docker.mu.Lock()
	defer docker.mu.Unlock()
	if docker.removeCalls != 1 {
		t.Fatalf("remove calls = %d, want 1", docker.removeCalls)
	}
}

func TestShutdownTerminatesBackgroundTaskBeforeRemovingContainer(t *testing.T) {
	docker := &fakeSessionDocker{}
	manager := NewSessionManager(testSessionManagerConfig(t), docker)
	session := readySession("deeix-42-7", 0)
	session.tasks["task-1"] = &BackgroundTask{ID: "task-1", PID: "4242", Output: "/tmp/custom.out"}
	manager.live[session.Scope] = session

	manager.Shutdown(context.Background())

	docker.mu.Lock()
	defer docker.mu.Unlock()
	if len(docker.execCommands) != 1 {
		t.Fatalf("task cleanup exec count = %d, want 1", len(docker.execCommands))
	}
	cleanup := strings.Join(docker.execCommands[0], " ")
	for _, expected := range []string{"kill -TERM", "4242", "kill -KILL", "/tmp/custom.out"} {
		if !strings.Contains(cleanup, expected) {
			t.Fatalf("task cleanup command missing %q: %s", expected, cleanup)
		}
	}
	if strings.Join(docker.events, ",") != "exec,remove" {
		t.Fatalf("shutdown order = %v, want exec before remove", docker.events)
	}
}

func TestConcurrentShutdownIsIdempotent(t *testing.T) {
	removeStarted := make(chan struct{})
	allowRemove := make(chan struct{})
	var removeStartedOnce sync.Once
	docker := &fakeSessionDocker{removeFn: func(context.Context, string) error {
		removeStartedOnce.Do(func() { close(removeStarted) })
		<-allowRemove
		return nil
	}}
	manager := NewSessionManager(testSessionManagerConfig(t), docker)
	session := readySession("deeix-42-7", 0)
	manager.live[session.Scope] = session

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.Shutdown(context.Background())
		}()
	}
	select {
	case <-removeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not start container removal")
	}
	close(allowRemove)
	wg.Wait()

	docker.mu.Lock()
	defer docker.mu.Unlock()
	if docker.removeCalls != 1 {
		t.Fatalf("remove calls = %d, want 1", docker.removeCalls)
	}
}

func TestGetOrCreateCreationRaceWithShutdown(t *testing.T) {
	createStarted := make(chan struct{})
	allowCreate := make(chan struct{})
	var createStartedOnce sync.Once
	docker := &fakeSessionDocker{createFn: func(context.Context, containerSpec, string) error {
		createStartedOnce.Do(func() { close(createStarted) })
		<-allowCreate
		return nil
	}}
	manager := NewSessionManager(testSessionManagerConfig(t), docker)

	type createResult struct {
		created bool
		release func()
		err     error
	}
	createdResult := make(chan createResult, 1)
	go func() {
		_, created, release, err := manager.GetOrCreate(context.Background(), "deeix-42-7")
		createdResult <- createResult{created: created, release: release, err: err}
	}()
	select {
	case <-createStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("session creation did not start")
	}

	shutdownDone := make(chan struct{})
	go func() {
		manager.Shutdown(context.Background())
		close(shutdownDone)
	}()
	waitForManagerStopping(t, manager)
	if _, _, _, err := manager.GetOrCreate(context.Background(), "deeix-42-8"); !errors.Is(err, errSessionManagerShuttingDown) {
		t.Fatalf("new admission error = %v, want shutdown error", err)
	}

	close(allowCreate)
	result := <-createdResult
	if result.err != nil || !result.created || result.release == nil {
		t.Fatalf("creation result = created:%v release:%v err:%v", result.created, result.release != nil, result.err)
	}
	result.release()
	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after creation lease released")
	}
	docker.mu.Lock()
	defer docker.mu.Unlock()
	if docker.removeCalls != 1 {
		t.Fatalf("remove calls = %d, want 1", docker.removeCalls)
	}
}

func TestSessionContainerSpecRejectsScopeSymlinks(t *testing.T) {
	for _, target := range []string{"shared", "imports"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			sharedRoot := filepath.Join(root, "shared")
			importsRoot := filepath.Join(root, "imports")
			if err := os.MkdirAll(sharedRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(importsRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			linkRoot := sharedRoot
			if target == "imports" {
				linkRoot = importsRoot
			}
			if err := os.Symlink(t.TempDir(), filepath.Join(linkRoot, "deeix-42-7")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			cfg := testSessionManagerConfig(t)
			cfg.SharedHostDir = sharedRoot
			cfg.ImportsHostDir = importsRoot
			manager := NewSessionManager(cfg, &fakeSessionDocker{})
			_, _, err := manager.sessionContainerSpec(readySession("deeix-42-7", 0))
			if err == nil || !strings.Contains(err.Error(), "not a regular directory") {
				t.Fatalf("scope symlink error = %v", err)
			}
		})
	}
}

func TestSessionContainerSpecRejectsRootAncestorSymlink(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	linked := filepath.Join(base, "linked")
	if err := os.Symlink(outside, linked); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	cfg := testSessionManagerConfig(t)
	cfg.SharedHostDir = filepath.Join(linked, "shared")
	manager := NewSessionManager(cfg, &fakeSessionDocker{})
	_, _, err := manager.sessionContainerSpec(readySession("deeix-42-7", 0))
	if err == nil || !strings.Contains(err.Error(), "not a regular directory") {
		t.Fatalf("root ancestor symlink error = %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(outside, "shared")); !os.IsNotExist(statErr) {
		t.Fatalf("symlink target was modified: %v", statErr)
	}
}

func TestSessionContainerSpecCreatesScopedDirectories(t *testing.T) {
	cfg := testSessionManagerConfig(t)
	manager := NewSessionManager(cfg, &fakeSessionDocker{})
	spec, _, err := manager.sessionContainerSpec(readySession("deeix-42-7", 0))
	if err != nil {
		t.Fatal(err)
	}
	if spec.SharedBind != filepath.Join(cfg.SharedHostDir, "deeix-42-7") || spec.ImportsBind != filepath.Join(cfg.ImportsHostDir, "deeix-42-7") {
		t.Fatalf("unexpected scoped binds: shared=%q imports=%q", spec.SharedBind, spec.ImportsBind)
	}
	for _, path := range []string{spec.SharedBind, spec.ImportsBind} {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("scoped directory %q is invalid: info=%v err=%v", path, info, statErr)
		}
	}
}

func TestPrepareSessionBindMarkersUsesContainerPaths(t *testing.T) {
	root := t.TempDir()
	spec := containerSpec{
		SharedBind:    filepath.Join(root, "shared"),
		SharedTarget:  "/shared/deeix-42-7",
		ImportsBind:   filepath.Join(root, "imports"),
		ImportsTarget: "/imports",
	}
	for _, source := range []string{spec.SharedBind, spec.ImportsBind} {
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	markers, cleanup, err := prepareSessionBindMarkers(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(markers) != 2 {
		t.Fatalf("marker count = %d, want 2", len(markers))
	}
	for _, marker := range markers {
		if strings.Contains(marker.containerPath, `\`) {
			t.Fatalf("container marker path must use POSIX separators: %q", marker.containerPath)
		}
	}
}

func TestEnsureSessionContainerFailsClosedOnBindMarkerMismatch(t *testing.T) {
	cfg := testSessionManagerConfig(t)
	docker := &fakeSessionDocker{
		createFn: func(_ context.Context, spec containerSpec, _ string) error {
			for _, source := range []string{spec.SharedBind, spec.ImportsBind} {
				matches, err := filepath.Glob(filepath.Join(source, ".deeix-bind-check-*"))
				if err != nil || len(matches) != 1 {
					t.Fatalf("bind marker missing before create: source=%s matches=%v err=%v", source, matches, err)
				}
			}
			return nil
		},
		execFn: func(context.Context, string, []string) (*execResult, error) {
			return &execResult{ExitCode: 42}, nil
		},
	}
	manager := NewSessionManager(cfg, docker)
	err := manager.ensureSessionContainer(context.Background(), readySession("deeix-42-7", 0))
	if err == nil || !strings.Contains(err.Error(), "marker mismatch") {
		t.Fatalf("bind verification error = %v", err)
	}
	if docker.removeCalls != 1 {
		t.Fatalf("unverified container remove calls = %d, want 1", docker.removeCalls)
	}
	for _, root := range []string{cfg.SharedHostDir, cfg.ImportsHostDir} {
		matches, globErr := filepath.Glob(filepath.Join(root, "deeix-42-7", ".deeix-bind-check-*"))
		if globErr != nil || len(matches) != 0 {
			t.Fatalf("bind markers not cleaned: root=%s matches=%v err=%v", root, matches, globErr)
		}
	}
}
