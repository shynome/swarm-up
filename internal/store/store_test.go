package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "images.yaml")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}

func TestSetImagePreservesCommentsOrderAndExtraFields(t *testing.T) {
	const orig = `# 团队镜像记录
version: "3.8"
services:
  web_api:
    image: acme/api:v0.0.1@sha256:aaa
    note: 保留我
  web_admin: { image: acme/admin:v0.0.1 }
`
	path := writeFile(t, orig, 0o640)

	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}
	if !st.Exists() {
		t.Fatal("Exists() 应为 true")
	}
	if got, ok := st.Lookup("web_api", "acme/api:v0.0.1@sha256:aaa"); !ok || got != "acme/api:v0.0.1@sha256:aaa" {
		t.Fatalf("Lookup(web_api) = %q, %v", got, ok)
	}

	if err := st.SetImage("web_api", "acme/api:v0.0.2@sha256:bbb"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}
	if err := st.SetImage("web_admin", "acme/admin:v0.0.2@sha256:ccc"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}

	got := readFile(t, path)
	for _, want := range []string{
		"# 团队镜像记录",
		"note: 保留我",
		"acme/api:v0.0.2@sha256:bbb",
		"acme/admin:v0.0.2@sha256:ccc",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "web_api:") > strings.Index(got, "web_admin:") {
		t.Fatalf("键顺序被改变:\n%s", got)
	}
	// 行内 flow mapping 应保持原风格（yaml.v3 可能给含冒号的标量加引号）
	var flowLine string
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "web_admin:") {
			flowLine = line
		}
	}
	if !strings.Contains(flowLine, "{") || !strings.HasSuffix(strings.TrimSpace(flowLine), "}") {
		t.Fatalf("flow mapping 未保留: %q\n%s", flowLine, got)
	}
	if !strings.Contains(flowLine, "acme/admin:v0.0.2@sha256:ccc") {
		t.Fatalf("flow mapping 值未更新: %q", flowLine)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("权限 = %o, want 640", fi.Mode().Perm())
	}

	// 重新加载后内容仍然正确
	st2, err := Load(path)
	if err != nil {
		t.Fatalf("重新 Load 报错: %v", err)
	}
	if got, _ := st2.Lookup("web_admin", "acme/admin:v0.0.2@sha256:ccc"); got != "acme/admin:v0.0.2@sha256:ccc" {
		t.Fatalf("重新加载后 Lookup = %q", got)
	}
}

func TestSetImageAppendsToEmptyServices(t *testing.T) {
	path := writeFile(t, "services: {}\n", 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}
	if err := st.SetImage("web_new", "acme/new:v2@sha256:ddd"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}

	st2, err := Load(path)
	if err != nil {
		t.Fatalf("重新 Load 报错: %v", err)
	}
	if got, ok := st2.Lookup("web_new", "acme/new:v2@sha256:ddd"); !ok || got != "acme/new:v2@sha256:ddd" {
		t.Fatalf("Lookup = %q, %v", got, ok)
	}
}

func TestSetImagePreservesComments(t *testing.T) {
	const orig = `# 顶部备注
version: "3.8"

services:
  songs:
    # 服务说明
    image: acme/songs:v1 # 行内备注

    ports:
      - "8080:8080" # 端口备注

  web:
    image: acme/web:v1
`
	path := writeFile(t, orig, 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	if err := st.SetImage("songs", "acme/songs:v2@sha256:new"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}

	want := strings.Replace(orig, "acme/songs:v1", "acme/songs:v2@sha256:new", 1)
	if got := readFile(t, path); got != want {
		t.Fatalf("除 image 值外文件内容应完全不变\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetImagePreservesQuotedStyle(t *testing.T) {
	const orig = "services:\n  songs:\n    image: \"acme/songs:v1\" # 备注\n"
	path := writeFile(t, orig, 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	if err := st.SetImage("songs", "acme/songs:v2"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}

	want := strings.Replace(orig, `"acme/songs:v1"`, `"acme/songs:v2"`, 1)
	if got := readFile(t, path); got != want {
		t.Fatalf("引号风格或注释未保留\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetImageRejectsNonComposeFile(t *testing.T) {
	const orig = "web_api:\n  image: acme/api:v1\n"
	path := writeFile(t, orig, 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}
	if err := st.SetImage("web_api", "acme/api:v2"); err == nil {
		t.Fatal("非 compose 文件应该报错")
	}
	if got := readFile(t, path); got != orig {
		t.Fatalf("失败时文件不应被修改:\n%s", got)
	}
}

func TestSetImageReplacesNullValue(t *testing.T) {
	path := writeFile(t, "services:\n  web_api:\n", 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}
	if _, ok := st.Lookup("web_api", "acme/api:v1"); ok {
		t.Fatal("空值不应被 Lookup 命中")
	}
	if err := st.SetImage("web_api", "acme/api:v1"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}
	st2, err := Load(path)
	if err != nil {
		t.Fatalf("重新 Load 报错: %v", err)
	}
	if got, ok := st2.Lookup("web_api", "acme/api:v1"); !ok || got != "acme/api:v1" {
		t.Fatalf("Lookup = %q, %v", got, ok)
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-exist.yaml")
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}
	if st.Exists() {
		t.Fatal("Exists() 应为 false")
	}
	if _, ok := st.Lookup("anything", "x:y"); ok {
		t.Fatal("空文件不应命中任何记录")
	}
	if err := st.SetImage("web_api", "acme/api:v1@sha256:eee"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}
	got := readFile(t, path)
	for _, want := range []string{"services:", "web_api:", "image: acme/api:v1@sha256:eee"} {
		if !strings.Contains(got, want) {
			t.Fatalf("文件未创建正确（缺少 %q）:\n%s", want, got)
		}
	}
}

func TestLoadRejectsNonMappingRoot(t *testing.T) {
	path := writeFile(t, "- a\n- b\n", 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("根节点是列表时应该报错")
	}
}

func TestOrder(t *testing.T) {
	path := writeFile(t, "services:\n  web_admin:\n    image: a\n  web_api:\n    image: b\n  web_cron:\n    image: c\n", 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	got := st.Order()
	want := []string{"web_admin", "web_api", "web_cron"}
	if len(got) != len(want) {
		t.Fatalf("Order() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Order() = %v, want %v", got, want)
		}
	}
}

func TestOrderMissingFile(t *testing.T) {
	st, err := Load(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}
	if got := st.Order(); len(got) != 0 {
		t.Fatalf("不存在文件时 Order() = %v, want empty", got)
	}
}

const composeSrc = `version: "3.8"
services:
  songs:
    image: shynome/songs-api:v0.0.12@sha256:old
    ports:
      - "8080:8080"
  web:
    image: acme/web:v1
networks:
  default:
`

func TestComposeMatch(t *testing.T) {
	path := writeFile(t, composeSrc, 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	// stack 前缀后缀匹配
	e, ok := st.Match("vtb_songs", "shynome/songs-api:v0.0.12@sha256:old")
	if !ok || e.Key != "songs" {
		t.Fatalf("后缀匹配 = %+v, %v", e, ok)
	}

	// 名称完全不同时按镜像仓库匹配
	e, ok = st.Match("totally-different", "shynome/songs-api:v0.0.12@sha256:old")
	if !ok || e.Key != "songs" {
		t.Fatalf("镜像匹配 = %+v, %v", e, ok)
	}

	// 同名优先
	e, ok = st.Match("web", "acme/web:v1")
	if !ok || e.Key != "web" {
		t.Fatalf("同名匹配 = %+v, %v", e, ok)
	}

	// 顺序为 services 下的定义顺序
	got := st.Order()
	want := []string{"songs", "web"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Order() = %v, want %v", got, want)
	}
}

func TestComposeSetImage(t *testing.T) {
	path := writeFile(t, composeSrc, 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	const newImage = "shynome/songs-api:v0.0.13@sha256:new"
	if err := st.SetImage("vtb_songs", newImage); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}

	got := readFile(t, path)
	for _, want := range []string{
		"version: \"3.8\"",
		"services:",
		"  songs:",
		"    image: " + newImage,
		"    ports:",
		"8080:8080",
		"networks:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "vtb_songs:") {
		t.Fatalf("不应在顶层追加 vtb_songs:\n%s", got)
	}
}

func TestComposeUnknownServiceRefusesToAppend(t *testing.T) {
	path := writeFile(t, composeSrc, 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	if err := st.SetImage("ghost", "acme/ghost:v1"); err == nil {
		t.Fatal("compose 中没有匹配条目时应该报错")
	}
	if got := readFile(t, path); got != composeSrc {
		t.Fatalf("失败时文件不应被修改:\n%s", got)
	}
}

func TestComposeImageMatchAmbiguous(t *testing.T) {
	const src = "services:\n  a:\n    image: acme/api:v1\n  b:\n    image: acme/api:v2\n"
	path := writeFile(t, src, 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}

	if e, ok := st.Match("other", "acme/api:v3"); ok {
		t.Fatalf("多个镜像候选时不应匹配: %+v", e)
	}
	if e, ok := st.Match("a", "acme/api:v3"); !ok || e.Key != "a" {
		t.Fatalf("同名仍应匹配: %+v, %v", e, ok)
	}
}
