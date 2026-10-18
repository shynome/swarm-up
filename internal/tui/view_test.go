package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shynome/swarm-up/internal/docker"
	"github.com/shynome/swarm-up/internal/store"
)

func TestBuildItemsStatuses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "images.yaml")
	content := "services:\n  web_api:\n    image: acme/api:v1@sha256:aaa\n  web_admin:\n    image: acme/admin:v1@sha256:old\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	st, err := store.Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	services := []docker.Service{
		{Name: "web_api", Image: "acme/api:v1@sha256:aaa"},
		{Name: "web_admin", Image: "acme/admin:v2@sha256:new"},
		{Name: "web_cron", Image: "acme/cron:v1@sha256:ccc"},
	}
	items := buildItems(services, st)
	if len(items) != 3 {
		t.Fatalf("items 数量 = %d, want 3", len(items))
	}

	want := map[string]syncStatus{
		"web_api":   statusInSync,
		"web_admin": statusMismatch,
		"web_cron":  statusMissing,
	}
	for _, it := range items {
		if got := it.status; got != want[it.svc.Name] {
			t.Fatalf("%s 状态 = %v, want %v", it.svc.Name, got, want[it.svc.Name])
		}
	}

	if got := items[1].Description(); got == "" {
		t.Fatal("Description 不应为空")
	}
}

func TestBuildItemsWithoutStore(t *testing.T) {
	items := buildItems([]docker.Service{{Name: "web_api", Image: "acme/api:v1"}}, nil)
	if len(items) != 1 || items[0].status != statusMissing {
		t.Fatalf("无记录文件时应全部为 statusMissing: %+v", items)
	}
}

func TestBuildItemsFollowsFileOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "images.yaml")
	content := "services:\n  web_cron:\n    image: acme/cron:v1\n  web_api:\n    image: acme/api:v1\n  ghost:\n    image: acme/ghost:v1\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	st, err := store.Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	services := []docker.Service{
		{Name: "web_api", Image: "acme/api:v1"},
		{Name: "web_admin", Image: "acme/admin:v1"},
		{Name: "web_cron", Image: "acme/cron:v1"},
		{Name: "aaa_svc", Image: "acme/aaa:v1"},
	}
	items := buildItems(services, st)

	var got []string
	for _, it := range items {
		got = append(got, it.svc.Name)
	}
	want := []string{"web_cron", "web_api", "aaa_svc", "web_admin"}
	if len(got) != len(want) {
		t.Fatalf("顺序 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序 = %v, want %v", got, want)
		}
	}
}

func TestShortImage(t *testing.T) {
	cases := map[string]string{
		"acme/api:v1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": "acme/api:v1#01234567",
		"acme/api:v1": "acme/api:v1",
		"acme/api":    "acme/api:latest",
	}
	for in, want := range cases {
		if got := shortImage(in); got != want {
			t.Fatalf("shortImage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWrapText(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want []string
	}{
		{"aaa bbb ccc", 7, []string{"aaa bbb", "ccc"}},
		{"abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"服务 新标签", 7, []string{"服务", "新标签"}},
		{"short", 20, []string{"short"}},
	}
	for _, tc := range cases {
		got := wrapText(tc.in, tc.w)
		if len(got) != len(tc.want) {
			t.Fatalf("wrapText(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("wrapText(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
			}
		}
	}
}
