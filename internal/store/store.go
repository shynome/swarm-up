package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Store 维护「服务名 → { image: ... }」的 yaml 记录文件。
//
// 读写都基于 yaml.Node，只修改目标条目的 image 字段，
// 因此键顺序、注释以及其他字段都会被保留。
type Store struct {
	path   string
	doc    *yaml.Node
	root   *yaml.Node
	source []byte
	exists bool
}

// Load 读取记录文件。文件不存在时返回空 Store（Exists=false），不视为错误。
func Load(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("未指定记录文件")
	}

	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
		}
		s.root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		s.doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{s.root}}
		return s, nil
	}

	s.exists = true
	s.source = b
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	if len(doc.Content) == 0 {
		s.root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		s.doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{s.root}}
		return s, nil
	}

	s.doc = &doc
	s.root = doc.Content[0]
	if s.root.Kind == yaml.DocumentNode && len(s.root.Content) > 0 {
		s.root = s.root.Content[0]
	}
	if s.root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s 的根节点必须是映射（服务名 → image）", path)
	}
	return s, nil
}

func (s *Store) Path() string { return s.path }

// Exists 表示文件在加载时是否已存在。
func (s *Store) Exists() bool { return s.exists }

// Entry 是 compose 文件中与某个 service 对应的记录。
type Entry struct {
	Key   string // services 下的服务名
	Image string
}

type entry struct {
	key  string
	node *yaml.Node
}

// servicesNode 返回 compose 文件中的 services 映射。
func (s *Store) servicesNode() *yaml.Node {
	if s == nil || s.root == nil {
		return nil
	}
	v := mappingValue(s.root, "services")
	if v == nil || v.Kind != yaml.MappingNode {
		return nil
	}
	return v
}

func (s *Store) entries() []entry {
	services := s.servicesNode()
	if services == nil {
		return nil
	}
	list := make([]entry, 0, len(services.Content)/2)
	for i := 0; i+1 < len(services.Content); i += 2 {
		list = append(list, entry{key: services.Content[i].Value, node: services.Content[i+1]})
	}
	return list
}

func (e entry) image() string {
	if e.node == nil || e.node.Kind != yaml.MappingNode {
		return ""
	}
	if img := mappingValue(e.node, "image"); img != nil && img.Kind == yaml.ScalarNode {
		return img.Value
	}
	return ""
}

// matchEntry 找到与 service 对应的条目。
//
// 匹配顺序：同名 → 唯一后缀（vtb_songs → songs）→ 镜像仓库。
func (s *Store) matchEntry(service, image string) (entry, bool) {
	list := s.entries()
	if len(list) == 0 || service == "" {
		return entry{}, false
	}

	for _, e := range list {
		if e.key == service {
			return e, true
		}
	}

	// 取最长的后缀 key，避免 api_songs / songs 这类歧义
	best := -1
	for i, e := range list {
		if !strings.HasSuffix(service, "_"+e.key) {
			continue
		}
		if best == -1 || len(e.key) > len(list[best].key) {
			best = i
		}
	}
	if best >= 0 {
		return list[best], true
	}

	repo := repoOf(image)
	if repo == "" {
		return entry{}, false
	}
	hit := -1
	for i, e := range list {
		if repoOf(e.image()) != repo {
			continue
		}
		if hit >= 0 {
			return entry{}, false // 多个候选，无法确定
		}
		hit = i
	}
	if hit >= 0 {
		return list[hit], true
	}
	return entry{}, false
}

// Match 返回与服务对应的记录。
func (s *Store) Match(service, image string) (Entry, bool) {
	e, ok := s.matchEntry(service, image)
	if !ok {
		return Entry{}, false
	}
	return Entry{Key: e.key, Image: e.image()}, true
}

// Order 返回 compose 中 services 条目的定义顺序。
func (s *Store) Order() []string {
	list := s.entries()
	names := make([]string, 0, len(list))
	for _, e := range list {
		names = append(names, e.key)
	}
	return names
}

// Lookup 返回服务记录中的镜像。
func (s *Store) Lookup(service, image string) (string, bool) {
	e, ok := s.matchEntry(service, image)
	if !ok {
		return "", false
	}
	img := e.image()
	return img, img != ""
}

// SetImage 设置 compose 中服务的 image 字段并立即写盘。
//
// 文件不存在或 services 为空时会在 services 下新增条目；
// 已有服务条目但匹配不到时返回错误，避免写错位置。
func (s *Store) SetImage(service, image string) error {
	if s == nil {
		return errors.New("记录文件为空")
	}
	if service == "" {
		return errors.New("服务名为空")
	}

	e, ok := s.matchEntry(service, image)
	if ok {
		if e.node.Kind != yaml.MappingNode {
			// 兼容 `service:`（null）或 `service: 旧值` 的写法。
			*e.node = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
				scalar("image"),
				scalar(image),
			}}
		} else if img := mappingValue(e.node, "image"); img != nil {
			// 优先只替换该行文本，保留空行、注释与其他格式。
			patched := s.patchImageScalar(img, image)
			img.Kind = yaml.ScalarNode
			img.Tag = "!!str"
			img.Value = image
			if patched {
				return s.writePatched()
			}
		} else {
			e.node.Content = append(e.node.Content, scalar("image"), scalar(image))
		}
		return s.Save()
	}

	services := s.servicesNode()
	if services == nil {
		if len(s.root.Content) > 0 {
			return fmt.Errorf("%s 不是 docker-compose 文件（缺少 services）", s.path)
		}
		// 新文件：创建 services 映射
		services = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		s.root.Content = append(s.root.Content, scalar("services"), services)
	} else if len(services.Content) > 0 {
		return fmt.Errorf("compose 中找不到与服务 %s（镜像 %s）匹配的条目", service, image)
	}

	services.Content = append(services.Content,
		scalar(service),
		&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			scalar("image"),
			scalar(image),
		}},
	)
	return s.Save()
}

func repoOf(image string) string {
	s := strings.TrimSpace(image)
	if s == "" {
		return ""
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		s = s[:i]
	}
	return s
}

var imageLinePattern = regexp.MustCompile(`^(\s*image:\s*)(.*)$`)

// patchImageScalar 只替换 image 值所在行的值，保留空行、注释与其余格式。
// 无法安全定位（多行标量、flow 风格等）时返回 false，由调用方回退到整体重新序列化。
func (s *Store) patchImageScalar(node *yaml.Node, newValue string) bool {
	if s.source == nil || node.Line <= 0 {
		return false
	}
	if node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return false
	}

	lines := bytes.Split(s.source, []byte("\n"))
	idx := node.Line - 1
	if idx < 0 || idx >= len(lines) {
		return false
	}
	m := imageLinePattern.FindSubmatch(lines[idx])
	if m == nil {
		return false
	}

	suffix, ok := imageValueSuffix(string(m[2]), node)
	if !ok {
		return false
	}

	value := newValue
	switch {
	case node.Style&yaml.DoubleQuotedStyle != 0:
		value = `"` + strings.ReplaceAll(newValue, `"`, `\"`) + `"`
	case node.Style&yaml.SingleQuotedStyle != 0:
		value = "'" + strings.ReplaceAll(newValue, "'", "''") + "'"
	}

	line := make([]byte, 0, len(m[1])+len(value)+len(suffix))
	line = append(line, m[1]...)
	line = append(line, value...)
	line = append(line, suffix...)

	newLines := make([][]byte, len(lines))
	copy(newLines, lines)
	newLines[idx] = line
	s.source = bytes.Join(newLines, []byte("\n"))
	return true
}

// imageValueSuffix 返回 image 值之后的内容（如行内注释），并校验解析结果与 node.Value 一致。
func imageValueSuffix(rest string, node *yaml.Node) (string, bool) {
	switch {
	case node.Style&yaml.DoubleQuotedStyle != 0:
		if !strings.HasPrefix(rest, `"`) {
			return "", false
		}
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\\' {
				i++
				continue
			}
			if rest[i] == '"' {
				if rest[1:i] != node.Value {
					return "", false
				}
				return rest[i+1:], true
			}
		}
		return "", false
	case node.Style&yaml.SingleQuotedStyle != 0:
		if !strings.HasPrefix(rest, "'") {
			return "", false
		}
		for i := 1; i < len(rest); i++ {
			if rest[i] != '\'' {
				continue
			}
			if i+1 < len(rest) && rest[i+1] == '\'' {
				i++
				continue
			}
			if strings.ReplaceAll(rest[1:i], "''", "'") != node.Value {
				return "", false
			}
			return rest[i+1:], true
		}
		return "", false
	default:
		value := rest
		suffix := ""
		if i := strings.Index(rest, " #"); i >= 0 {
			value = rest[:i]
			suffix = rest[i:]
		}
		if strings.HasSuffix(value, "\r") {
			value = strings.TrimSuffix(value, "\r")
			suffix = "\r" + suffix
		}
		if strings.TrimSpace(value) != node.Value {
			return "", false
		}
		return suffix, true
	}
}

// Save 重新序列化并原子写入文件。
func (s *Store) Save() error {
	if s == nil {
		return errors.New("记录文件为空")
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s.doc); err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", s.path, err)
	}
	if err := enc.Close(); err != nil {
		return err
	}

	if err := s.writeBytes(buf.Bytes()); err != nil {
		return err
	}
	s.source = append([]byte(nil), buf.Bytes()...)
	s.exists = true
	return nil
}

// writePatched 写入局部修改后的内容（保留原文件其余字节）。
func (s *Store) writePatched() error {
	if err := s.writeBytes(s.source); err != nil {
		return err
	}
	s.exists = true
	return nil
}

func (s *Store) writeBytes(data []byte) error {
	dir := filepath.Dir(s.path)
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(s.path); err == nil {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".swarm-up-*.yaml")
	if err != nil {
		return fmt.Errorf("无法在 %s 创建临时文件: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", s.path, err)
	}
	return nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}
