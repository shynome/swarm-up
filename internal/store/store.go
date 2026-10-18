package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// Order 返回文件中服务条目的定义顺序。
func (s *Store) Order() []string {
	if s == nil || s.root == nil {
		return nil
	}
	names := make([]string, 0, len(s.root.Content)/2)
	for i := 0; i+1 < len(s.root.Content); i += 2 {
		names = append(names, s.root.Content[i].Value)
	}
	return names
}

// Lookup 返回服务记录中的镜像。
func (s *Store) Lookup(service string) (string, bool) {
	key, value := s.find(service)
	if key == nil || value == nil {
		return "", false
	}
	img := mappingValue(value, "image")
	if img == nil || img.Kind != yaml.ScalarNode {
		return "", false
	}
	return img.Value, true
}

// SetImage 设置服务的 image 字段（不存在则追加条目），并立即写盘。
func (s *Store) SetImage(service, image string) error {
	if s == nil {
		return errors.New("记录文件为空")
	}
	if service == "" {
		return errors.New("服务名为空")
	}

	key, value := s.find(service)
	switch {
	case key == nil:
		s.root.Content = append(s.root.Content,
			scalar(service),
			&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
				scalar("image"),
				scalar(image),
			}},
		)
	case value.Kind != yaml.MappingNode:
		// 兼容 `service:`（null）或 `service: 旧值` 的写法。
		*value = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			scalar("image"),
			scalar(image),
		}}
	default:
		if img := mappingValue(value, "image"); img != nil {
			img.Kind = yaml.ScalarNode
			img.Tag = "!!str"
			img.Value = image
		} else {
			value.Content = append(value.Content, scalar("image"), scalar(image))
		}
	}

	return s.Save()
}

// Save 原子写入文件，保留原文件权限。
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
	if _, err := tmp.Write(buf.Bytes()); err != nil {
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

	s.source = buf.Bytes()
	s.exists = true
	return nil
}

func (s *Store) find(service string) (key, value *yaml.Node) {
	if s == nil || s.root == nil {
		return nil, nil
	}
	for i := 0; i+1 < len(s.root.Content); i += 2 {
		if s.root.Content[i].Value == service {
			return s.root.Content[i], s.root.Content[i+1]
		}
	}
	return nil, nil
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
