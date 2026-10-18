package docker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

const fakeDocker = `#!/bin/sh
if [ "$1" = "service" ] && [ "$2" = "ls" ]; then
  echo "id1"
  echo "id2"
  exit 0
fi
if [ "$1" = "service" ] && [ "$2" = "inspect" ]; then
  printf 'id1\tweb_api\tacme/api:v0.0.1@%s\n' "$DIGEST_A"
  printf 'id2\tweb_admin\tacme/admin:latest@%s\n' "$DIGEST_B"
  exit 0
fi
if [ "$1" = "service" ] && [ "$2" = "update" ]; then
  echo "updated: $*"
  exit 0
fi
if [ "$1" = "pull" ]; then
  if [ "$FAKE_PULL" = "up-to-date" ]; then
    echo "Image is up to date for $2"
  else
    echo "v0.0.2: Pulling from acme/api"
    echo "Digest: $FAKE_DIGEST"
  fi
  exit 0
fi
if [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
  printf '["acme/api@%s"]\n' "$FAKE_DIGEST"
  exit 0
fi
echo "unexpected args: $*" >&2
exit 1
`

func useFakeDocker(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(path, []byte(fakeDocker), 0o755); err != nil {
		t.Fatalf("写入假 docker 失败: %v", err)
	}
	t.Setenv("SWARM_UP_DOCKER", path)
	t.Setenv("DIGEST_A", digestA)
	t.Setenv("DIGEST_B", digestB)
}

func TestListServices(t *testing.T) {
	useFakeDocker(t)

	svcs, err := NewClient().ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices 报错: %v", err)
	}
	if len(svcs) != 2 {
		t.Fatalf("期望 2 个 service, got %d: %+v", len(svcs), svcs)
	}
	if svcs[0].Name != "web_admin" || svcs[1].Name != "web_api" {
		t.Fatalf("结果未按名称排序: %+v", svcs)
	}
	if svcs[1].Ref.Repo != "acme/api" || svcs[1].Ref.Tag != "v0.0.1" || svcs[1].Ref.Digest != digestA {
		t.Fatalf("镜像解析错误: %+v", svcs[1])
	}
}

func TestPullDigestFromOutput(t *testing.T) {
	useFakeDocker(t)
	t.Setenv("FAKE_DIGEST", digestA)

	var lines []string
	digest, err := NewClient().Pull(context.Background(), Ref{Repo: "acme/api", Tag: "v0.0.2"}, func(l string) {
		lines = append(lines, l)
	})
	if err != nil {
		t.Fatalf("Pull 报错: %v", err)
	}
	if digest != digestA {
		t.Fatalf("digest = %q, want %q", digest, digestA)
	}
	if len(lines) < 2 {
		t.Fatalf("期望收到 pull 输出行, got %v", lines)
	}
}

func TestPullFallbackToImageInspect(t *testing.T) {
	useFakeDocker(t)
	t.Setenv("FAKE_PULL", "up-to-date")
	t.Setenv("FAKE_DIGEST", digestB)

	digest, err := NewClient().Pull(context.Background(), Ref{Repo: "acme/api", Tag: "v0.0.2"}, nil)
	if err != nil {
		t.Fatalf("Pull 报错: %v", err)
	}
	if digest != digestB {
		t.Fatalf("digest = %q, want %q", digest, digestB)
	}
}

func TestUpdateServiceArgs(t *testing.T) {
	useFakeDocker(t)

	var out []string
	image := "acme/api:v0.0.2@" + digestA
	if err := NewClient().UpdateService(context.Background(), "web_api", image, func(l string) {
		out = append(out, l)
	}); err != nil {
		t.Fatalf("UpdateService 报错: %v", err)
	}
	joined := strings.Join(out, "\n")
	for _, want := range []string{"--image", image, "--quiet", "web_api"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("输出缺少 %q: %s", want, joined)
		}
	}
	if got, want := strings.Join(UpdateArgs("web_api", image), " "), "service update --image "+image+" --quiet web_api"; got != want {
		t.Fatalf("UpdateArgs = %q, want %q", got, want)
	}
}
