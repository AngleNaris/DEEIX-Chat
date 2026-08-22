package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// dockerClient 封装宿主机 Docker API 的最小操作面（容器创建/执行/销毁/卷）。
// 沙箱 MCP 服务需要挂载宿主 /var/run/docker.sock，且仅本服务持有该权限。
type dockerClient struct {
	cli         *client.Client
	execCounter atomic.Uint64
}

func newDockerClient() (*dockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &dockerClient{cli: cli}, nil
}

func (d *dockerClient) close() {
	if d.cli != nil {
		_ = d.cli.Close()
	}
}

// ensureImage 确保镜像本地存在（不存在则拉取）。
func (d *dockerClient) ensureImage(ctx context.Context, ref string) error {
	summary, err := d.cli.ImageList(ctx, image.ListOptions{Filters: filters.NewArgs(filters.Arg("reference", ref))})
	if err == nil && len(summary) > 0 {
		return nil
	}
	rc, err := d.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}
	defer rc.Close()
	// 完整消费拉取响应：关闭并读空 reader，释放连接、暴露拉取错误（P1-05）。
	_, _ = io.Copy(io.Discard, rc)
	return nil
}

// containerSpec 一次容器创建所需的全部参数。
type containerSpec struct {
	Name          string
	Image         string
	Env           []string
	Memory        string
	PidsLimit     int64
	CPUs          float64
	Workspace     string
	CacheMount    bool   // 挂载用户级共享缓存卷（pip/npm 缓存）
	CacheVol      string // 缓存卷名（使用配置值，禁止硬编码）
	SharedBind    string // 宿主共享子目录（仅本 scope）bind 到 SharedTarget；空则不挂
	SharedTarget  string // 容器内共享目录路径（/shared/<scope>）
	ImportsBind   string // 当前 scope 的导入目录，始终只读
	ImportsTarget string
	Network       string // 专用沙箱网络（仅允许受控公网出口）
}

// createContainer 创建并启动一个会话容器。volumeName 为空时不挂工作区卷。
func (d *dockerClient) createContainer(ctx context.Context, spec containerSpec, volumeName string) error {
	if err := d.ensureImage(ctx, spec.Image); err != nil {
		return err
	}
	cfg, host := buildContainerConfig(spec, volumeName)
	_, err := d.cli.ContainerCreate(ctx, cfg, host, nil, nil, spec.Name)
	if err != nil {
		return fmt.Errorf("create container %s: %w", spec.Name, err)
	}
	if err := d.cli.ContainerStart(ctx, spec.Name, container.StartOptions{}); err != nil {
		// 补偿清理：启动失败的容器不留残余（P1-04 顺手项）。
		_ = d.removeContainer(ctx, spec.Name)
		return fmt.Errorf("start container %s: %w", spec.Name, err)
	}
	return nil
}

func buildContainerConfig(spec containerSpec, volumeName string) (*container.Config, *container.HostConfig) {
	mounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: volumeName, Target: spec.Workspace},
	}
	if spec.CacheMount {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: spec.CacheVol,
			Target: "/root/.cache",
		})
	}
	if spec.SharedBind != "" {
		// P0-05：只 bind 挂载当前会话的 scope 子目录，不做整卷挂载，
		// 容器内无法枚举/读取其他租户目录。
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: spec.SharedBind,
			Target: spec.SharedTarget,
		})
	}
	if spec.ImportsBind != "" {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   spec.ImportsBind,
			Target:   spec.ImportsTarget,
			ReadOnly: true,
		})
	}
	cfg := &container.Config{
		Image:      spec.Image,
		User:       "0:0",
		Env:        spec.Env,
		WorkingDir: spec.Workspace,
		Entrypoint: []string{"/bin/sh", "-c", "sleep infinity"},
	}
	host := &container.HostConfig{
		Mounts:         mounts,
		SecurityOpt:    []string{"no-new-privileges:true"},
		Privileged:     false,
		ReadonlyRootfs: false,
		Resources: container.Resources{
			Memory:    parseMemoryBytes(spec.Memory),
			NanoCPUs:  int64(spec.CPUs * 1e9),
			PidsLimit: &spec.PidsLimit,
		},
	}
	if strings.TrimSpace(spec.Network) != "" {
		host.NetworkMode = container.NetworkMode(strings.TrimSpace(spec.Network))
	}
	return cfg, host
}

type inspectedContainerConfig struct {
	Config     *container.Config
	HostConfig *container.HostConfig
	Running    bool
	Networks   []string
}

func (d *dockerClient) inspectContainerConfig(ctx context.Context, name string) (*inspectedContainerConfig, bool, error) {
	inspected, err := d.cli.ContainerInspect(ctx, name)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	result := &inspectedContainerConfig{
		Config:     inspected.Config,
		HostConfig: inspected.HostConfig,
		Running:    inspected.State != nil && inspected.State.Running,
	}
	if inspected.NetworkSettings != nil {
		result.Networks = make([]string, 0, len(inspected.NetworkSettings.Networks))
		for name := range inspected.NetworkSettings.Networks {
			result.Networks = append(result.Networks, name)
		}
	}
	return result, true, nil
}

func containerConfigMatches(
	actual *inspectedContainerConfig,
	desiredConfig *container.Config,
	desiredHost *container.HostConfig,
) bool {
	if actual == nil || actual.Config == nil || actual.HostConfig == nil || !actual.Running {
		return false
	}
	if actual.Config.User != desiredConfig.User ||
		actual.Config.WorkingDir != desiredConfig.WorkingDir ||
		!stringSliceEqual(actual.Config.Entrypoint, desiredConfig.Entrypoint) ||
		!containsAllStrings(actual.Config.Env, desiredConfig.Env) {
		return false
	}
	if actual.HostConfig.Privileged != desiredHost.Privileged ||
		actual.HostConfig.ReadonlyRootfs != desiredHost.ReadonlyRootfs ||
		!containsAllStrings(actual.HostConfig.SecurityOpt, desiredHost.SecurityOpt) ||
		actual.HostConfig.NetworkMode != desiredHost.NetworkMode ||
		actual.HostConfig.Resources.Memory != desiredHost.Resources.Memory ||
		actual.HostConfig.Resources.NanoCPUs != desiredHost.Resources.NanoCPUs ||
		!equalInt64Pointers(actual.HostConfig.Resources.PidsLimit, desiredHost.Resources.PidsLimit) ||
		len(actual.HostConfig.Binds) != 0 ||
		len(actual.HostConfig.VolumesFrom) != 0 ||
		!mountSetsEqual(actual.HostConfig.Mounts, desiredHost.Mounts) {
		return false
	}
	network := strings.TrimSpace(string(desiredHost.NetworkMode))
	return network == "" || len(actual.Networks) == 1 && actual.Networks[0] == network
}

func stringSliceEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsAllStrings(actual, required []string) bool {
	for _, expected := range required {
		found := false
		for _, value := range actual {
			if value == expected {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func equalInt64Pointers(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func mountSetsEqual(actual, desired []mount.Mount) bool {
	if len(actual) != len(desired) {
		return false
	}
	for _, expected := range desired {
		found := false
		for _, candidate := range actual {
			if candidate.Type == expected.Type &&
				candidate.Source == expected.Source &&
				candidate.Target == expected.Target &&
				candidate.ReadOnly == expected.ReadOnly {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (d *dockerClient) renameContainer(ctx context.Context, name, nextName string) error {
	if err := d.cli.ContainerRename(ctx, name, nextName); err != nil {
		return fmt.Errorf("rename container %s to %s: %w", name, nextName, err)
	}
	return nil
}

func (d *dockerClient) removeContainer(ctx context.Context, name string) error {
	err := d.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
	if err != nil && !client.IsErrNotFound(err) {
		return err
	}
	return nil
}

func (d *dockerClient) removeVolume(volume string) error {
	err := d.cli.VolumeRemove(context.Background(), volume, true)
	if err != nil && !strings.Contains(err.Error(), "No such volume") {
		return err
	}
	return nil
}

// execResult 一条容器命令的执行结果。
type execResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	originalLen := len(p)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.buffer.Write(p)
	}
	return originalLen, nil
}

func (b *cappedBuffer) String() string {
	return b.buffer.String()
}

const execWrapperScript = `pid_file=$1
shift
setsid "$@" &
child=$!
printf '%s' "$child" > "$pid_file"
wait "$child"
status=$?
rm -f "$pid_file"
exit "$status"`

const terminateExecScript = `pid_file=$1
# 等待 wrapper 写入 PID：上限 4s，避免命令刚启动即超时时清理脚本抢跑。
i=0
while [ "$i" -lt 80 ] && [ ! -s "$pid_file" ]; do
  i=$((i + 1))
  sleep 0.05
done
if [ -s "$pid_file" ]; then
  pid=$(cat "$pid_file")
  kill -TERM -- "-$pid" 2>/dev/null || true
  sleep 1
  kill -KILL -- "-$pid" 2>/dev/null || true
fi
rm -f "$pid_file"`

// execInContainer 在容器内同步执行命令。
// workingDir 经 Docker Exec 原生 WorkingDir 传递（不经 shell cd 拼接，P0-04）。
// stdinData 非空时通过标准输入注入（用于 base64 写文件等大载荷场景）。
func (d *dockerClient) execInContainer(
	ctx context.Context,
	name string,
	cmd []string,
	workingDir string,
	stdinData []byte,
	timeout time.Duration,
	outputLimit int,
) (*execResult, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if outputLimit <= 0 {
		outputLimit = 64 << 10
	}
	if len(cmd) == 0 {
		return nil, errors.New("exec command is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pidFile := fmt.Sprintf("/tmp/.deeix-exec-%d-%d.pid", time.Now().UnixNano(), d.execCounter.Add(1))
	wrappedCmd := append([]string{"/bin/sh", "-c", execWrapperScript, "deeix-exec", pidFile}, cmd...)
	execCfg := container.ExecOptions{
		Cmd:          wrappedCmd,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  stdinData != nil,
		WorkingDir:   workingDir,
	}
	execID, err := d.cli.ContainerExecCreate(ctx, name, execCfg)
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}
	resp, err := d.cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close()

	stdoutBuf := &cappedBuffer{limit: outputLimit + 1}
	stderrBuf := &cappedBuffer{limit: outputLimit + 1}
	writeErr := make(chan error, 1)
	go func() {
		if stdinData != nil {
			_, _ = resp.Conn.Write(stdinData)
			_ = resp.CloseWrite()
		}
		_, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, resp.Reader)
		writeErr <- err
	}()

	select {
	case <-ctx.Done():
		resp.Close()
		<-writeErr
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		terminateErr := d.terminateExecProcessGroup(terminateCtx, name, pidFile)
		terminateCancel()
		if terminateErr != nil {
			return nil, fmt.Errorf("exec timeout after %s: %w; terminate process group: %v", timeout, ctx.Err(), terminateErr)
		}
		return nil, fmt.Errorf("exec timeout after %s: %w", timeout, ctx.Err())
	case err := <-writeErr:
		if err != nil {
			return nil, fmt.Errorf("exec stream: %w", err)
		}
	}
	inspect, err := d.cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return nil, fmt.Errorf("exec inspect: %w", err)
	}
	return &execResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: inspect.ExitCode,
	}, nil
}

func (d *dockerClient) terminateExecProcessGroup(ctx context.Context, name string, pidFile string) error {
	execID, err := d.cli.ContainerExecCreate(ctx, name, container.ExecOptions{
		Cmd:          []string{"/bin/sh", "-c", terminateExecScript, "deeix-kill", pidFile},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return err
	}
	resp, err := d.cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return err
	}
	defer resp.Close()
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(io.Discard, resp.Reader)
		copyDone <- copyErr
	}()
	select {
	case copyErr := <-copyDone:
		return copyErr
	case <-ctx.Done():
		resp.Close()
		<-copyDone
		return ctx.Err()
	}
}

// parseMemoryBytes 解析 docker --memory 风格字符串（"1g"/"512m"/"268435456"）。
func parseMemoryBytes(raw string) int64 {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return 0
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(raw, "g"):
		mult, raw = 1<<30, strings.TrimSuffix(raw, "g")
	case strings.HasSuffix(raw, "m"):
		mult, raw = 1<<20, strings.TrimSuffix(raw, "m")
	case strings.HasSuffix(raw, "k"):
		mult, raw = 1<<10, strings.TrimSuffix(raw, "k")
	case strings.HasSuffix(raw, "b"):
		raw = strings.TrimSuffix(raw, "b")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n * mult
}

var errEmptyOutput = errors.New("empty output")

// decodeBase64Output 将容器返回的 base64 文本解码为原始字节。
func decodeBase64Output(s string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 {
		return nil, errEmptyOutput
	}
	return decoded, nil
}

// 供 files.go 使用的 io 工具。
func copyLimited(dst io.Writer, src io.Reader, limit int) (int64, error) {
	return io.Copy(dst, io.LimitReader(src, int64(limit)))
}
