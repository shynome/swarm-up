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
	if got, ok := st.Lookup("web_api"); !ok || got != "acme/api:v0.0.1@sha256:aaa" {
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
		if strings.HasPrefix(line, "web_admin:") {
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
	if got, _ := st2.Lookup("web_admin"); got != "acme/admin:v0.0.2@sha256:ccc" {
		t.Fatalf("重新加载后 Lookup = %q", got)
	}
}

func TestSetImageAppendsNewService(t *testing.T) {
	path := writeFile(t, "web_api:\n  image: acme/api:v1\n", 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}
	if err := st.SetImage("web_new", "acme/new:v2@sha256:ddd"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "web_new:\n  image: acme/new:v2@sha256:ddd") {
		t.Fatalf("新条目格式不对:\n%s", got)
	}
}

func TestSetImageReplacesNullValue(t *testing.T) {
	path := writeFile(t, "web_api:\n", 0o644)
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load 报错: %v", err)
	}
	if _, ok := st.Lookup("web_api"); ok {
		t.Fatal("空值不应被 Lookup 命中")
	}
	if err := st.SetImage("web_api", "acme/api:v1"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}
	st2, err := Load(path)
	if err != nil {
		t.Fatalf("重新 Load 报错: %v", err)
	}
	if got, ok := st2.Lookup("web_api"); !ok || got != "acme/api:v1" {
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
	if _, ok := st.Lookup("anything"); ok {
		t.Fatal("空文件不应命中任何记录")
	}
	if err := st.SetImage("web_api", "acme/api:v1@sha256:eee"); err != nil {
		t.Fatalf("SetImage 报错: %v", err)
	}
	if got := readFile(t, path); !strings.Contains(got, "web_api:") {
		t.Fatalf("文件未创建正确:\n%s", got)
	}
}

func TestLoadRejectsNonMappingRoot(t *testing.T) {
	path := writeFile(t, "- a\n- b\n", 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("根节点是列表时应该报错")
	}
}

func TestOrder(t *testing.T) {
	path := writeFile(t, "web_admin:\n  image: a\nweb_api:\n  image: b\nweb_cron:\n  image: c\n", 0o644)
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
