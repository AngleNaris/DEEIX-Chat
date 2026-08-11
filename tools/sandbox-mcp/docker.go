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
	cli *client.Client
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
	_, err = d.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", ref, err)
	}
	return nil
}

// containerSpec 一次容器创建所需的全部参数。
type containerSpec struct {
	Name       string
	Image      string
	Env        []string
	Memory     string
	PidsLimit  int64
	CPUs       float64
	Workspace  string
	CacheMount bool   // 挂载用户级共享缓存卷（pip/npm 缓存）
	SharedVol  string // 挂载共享卷（沙箱 <-> mm 多模态工具文件桥接），空则不挂
}

// createContainer 创建并启动一个会话容器。volumeName 为空时不挂工作区卷。
func (d *dockerClient) createContainer(ctx context.Context, spec containerSpec, volumeName string) error {
	if err := d.ensureImage(ctx, spec.Image); err != nil {
		return err
	}
	mounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: volumeName, Target: spec.Workspace},
	}
	if spec.CacheMount {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: "deeix-sandbox-cache-root",
			Target: "/root/.cache",
		})
	}
	if spec.SharedVol != "" {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: spec.SharedVol,
			Target: "/shared",
		})
	}
	cfg := &container.Config{
		Image:      spec.Image,
		Env:        spec.Env,
		WorkingDir: spec.Workspace,
		Entrypoint: []string{"/bin/sh", "-c", "sleep infinity"},
	}
	host := &container.HostConfig{
		Mounts: mounts,
		Resources: container.Resources{
			Memory:    parseMemoryBytes(spec.Memory),
			NanoCPUs:  int64(spec.CPUs * 1e9),
			PidsLimit: &spec.PidsLimit,
		},
	}
	_, err := d.cli.ContainerCreate(ctx, cfg, host, nil, nil, spec.Name)
	if err != nil {
		return fmt.Errorf("create container %s: %w", spec.Name, err)
	}
	if err := d.cli.ContainerStart(ctx, spec.Name, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", spec.Name, err)
	}
	return nil
}

func (d *dockerClient) containerExists(ctx context.Context, name string) (bool, error) {
	_, err := d.cli.ContainerInspect(ctx, name)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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

// execInContainer 在容器内同步执行命令。
// stdinData 非空时通过标准输入注入（用于 base64 写文件等大载荷场景）。
func (d *dockerClient) execInContainer(ctx context.Context, name string, cmd []string, stdinData []byte, timeout time.Duration) (*execResult, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execCfg := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  stdinData != nil,
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

	var stdoutBuf, stderrBuf bytes.Buffer
	writeErr := make(chan error, 1)
	go func() {
		if stdinData != nil {
			_, _ = resp.Conn.Write(stdinData)
			_ = resp.CloseWrite() // 通知容器 stdin EOF
		}
		_, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, resp.Reader)
		writeErr <- err
	}()

	select {
	case <-ctx.Done():
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
