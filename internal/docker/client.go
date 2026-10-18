package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Service 是 swarm 中的一个 service 及其当前实际镜像。
type Service struct {
	ID    string
	Name  string
	Image string
	Ref   Ref
}

// Client 封装 docker CLI。使用 CLI 而不是 SDK，行为与手工流程完全一致，
// 并且自动尊重 DOCKER_HOST / docker context 配置。
type Client struct {
	bin string
}

// NewClient 创建客户端；设置 SWARM_UP_DOCKER 可指定 docker 可执行文件。
func NewClient() *Client {
	bin := os.Getenv("SWARM_UP_DOCKER")
	if bin == "" {
		bin = "docker"
	}
	return &Client{bin: bin}
}

func (c *Client) Bin() string { return c.bin }

// ListServices 返回所有 service 及其实际镜像（含 digest）。
func (c *Client) ListServices(ctx context.Context) ([]Service, error) {
	out, err := c.run(ctx, "service", "ls", "-q")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return nil, nil
	}

	args := append([]string{
		"service", "inspect",
		"--format", "{{.ID}}\t{{.Spec.Name}}\t{{.Spec.TaskTemplate.ContainerSpec.Image}}",
	}, ids...)
	out, err = c.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	services := make([]Service, 0, len(ids))
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		svc := Service{ID: parts[0], Name: parts[1], Image: parts[2]}
		if ref, err := ParseRef(svc.Image); err == nil {
			svc.Ref = ref
		}
		services = append(services, svc)
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services, nil
}

var digestLinePattern = regexp.MustCompile(`(?m)^Digest:\s+(sha256:[0-9a-f]{64})\s*$`)

// Pull 拉取 ref 对应的镜像并返回 digest。onLine 用于实时展示 docker 输出。
func (c *Client) Pull(ctx context.Context, ref Ref, onLine func(string)) (string, error) {
	image := ref.RepoTag()
	out, err := c.runStream(ctx, onLine, "pull", image)
	if err != nil {
		return "", fmt.Errorf("docker pull %s 失败: %w", image, err)
	}
	if m := digestLinePattern.FindStringSubmatch(out); m != nil {
		return m[1], nil
	}
	// 本地已是最新时 docker 不打印 Digest 行，改为查询本地镜像。
	return c.resolveDigest(ctx, ref)
}

// resolveDigest 从本地镜像的 RepoDigests 中取出与 ref.Repo 匹配的 digest。
func (c *Client) resolveDigest(ctx context.Context, ref Ref) (string, error) {
	image := ref.RepoTag()
	out, err := c.run(ctx, "image", "inspect", image, "--format", "{{json .RepoDigests}}")
	if err != nil {
		return "", fmt.Errorf("无法获取 %s 的 digest: %w", image, err)
	}
	var digests []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &digests); err != nil {
		return "", fmt.Errorf("解析 %s 的 RepoDigests 失败: %w", image, err)
	}
	for _, d := range digests {
		i := strings.LastIndex(d, "@")
		if i < 0 || i == len(d)-1 {
			continue
		}
		if !sameRepo(d[:i], ref.Repo) {
			continue
		}
		return d[i+1:], nil
	}
	return "", fmt.Errorf("%s 没有可用的 RepoDigest", image)
}

// sameRepo 用于兼容 docker.io/library 等前缀差异。
func sameRepo(a, b string) bool {
	if a == b {
		return true
	}
	return strings.HasSuffix(a, "/"+b) || strings.HasSuffix(b, "/"+a)
}

// UpdateArgs 返回更新 service 镜像的 docker 参数，供展示与实际执行共用。
func UpdateArgs(service, image string) []string {
	return []string{"service", "update", "--image", image, "--quiet", service}
}

// UpdateService 更新 service 镜像。
func (c *Client) UpdateService(ctx context.Context, service, image string, onLine func(string)) error {
	args := UpdateArgs(service, image)
	if _, err := c.runStream(ctx, onLine, args...); err != nil {
		return fmt.Errorf("docker service update %s 失败: %w", service, err)
	}
	return nil
}

func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	out, err := c.runStream(ctx, nil, args...)
	if err != nil {
		return out, fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// runStream 执行命令并把输出同时收集起来；onLine 收到按行切分的内容。
func (c *Client) runStream(ctx context.Context, onLine func(string), args ...string) (string, error) {
	collector := newLineCollector(onLine)
	cmd := exec.CommandContext(ctx, c.bin, args...)
	cmd.Stdout = collector
	cmd.Stderr = collector

	err := cmd.Run()
	collector.Flush()
	text := collector.String()
	if err != nil {
		msg := strings.TrimSpace(text)
		// 已经流式展示给用户时，不再在错误里重复附带完整输出。
		if msg == "" || onLine != nil {
			return text, err
		}
		return text, fmt.Errorf("%w\n%s", err, msg)
	}
	return text, nil
}

// lineCollector 按行切分输出，并发安全（stdout/stderr 可能同时写入）。
type lineCollector struct {
	mu   sync.Mutex
	part bytes.Buffer
	all  bytes.Buffer
	on   func(string)
}

func newLineCollector(on func(string)) *lineCollector {
	return &lineCollector{on: on}
}

func (w *lineCollector) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(p)
	w.all.Write(p)
	for {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			w.part.Write(p)
			break
		}
		w.part.Write(p[:i])
		w.emit(w.part.String())
		w.part.Reset()
		p = p[i+1:]
	}
	return n, nil
}

func (w *lineCollector) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.part.Len() > 0 {
		w.emit(w.part.String())
		w.part.Reset()
	}
}

func (w *lineCollector) emit(line string) {
	if w.on == nil {
		return
	}
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	w.on(line)
}

func (w *lineCollector) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.all.String()
}
