package docker

import (
	"fmt"
	"regexp"
	"strings"
)

// Ref 是解析后的镜像引用，形如 repo:tag@sha256:digest。
type Ref struct {
	Repo   string
	Tag    string
	Digest string
}

var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// ParseRef 解析镜像引用，tag 与 digest 均允许缺省。
func ParseRef(raw string) (Ref, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Ref{}, fmt.Errorf("镜像引用为空")
	}

	var ref Ref
	if i := strings.LastIndex(s, "@"); i >= 0 {
		ref.Digest = s[i+1:]
		s = s[:i]
		if ref.Digest == "" {
			return Ref{}, fmt.Errorf("镜像引用 %q 的 digest 为空", raw)
		}
	}

	// 只有位于最后一个 '/' 之后的冒号才是 tag，避免把 registry 端口当作 tag。
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		ref.Tag = s[i+1:]
		s = s[:i]
	}
	ref.Repo = s
	if ref.Repo == "" {
		return Ref{}, fmt.Errorf("无法从 %q 解析出仓库名", raw)
	}
	if ref.Tag == "" {
		ref.Tag = "latest"
	}
	return ref, nil
}

// RepoTag 返回不带 digest 的引用，缺省 tag 视为 latest。
func (r Ref) RepoTag() string {
	tag := r.Tag
	if tag == "" {
		tag = "latest"
	}
	return r.Repo + ":" + tag
}

func (r Ref) String() string {
	if r.Digest == "" {
		return r.RepoTag()
	}
	return r.RepoTag() + "@" + r.Digest
}

// WithTag 替换 tag；digest 属于旧版本，必须丢弃并等 pull 后重新解析。
func (r Ref) WithTag(tag string) Ref {
	return Ref{Repo: r.Repo, Tag: tag}
}

// WithDigest 返回 repo:tag@digest。
func (r Ref) WithDigest(digest string) Ref {
	return Ref{Repo: r.Repo, Tag: r.Tag, Digest: digest}
}

// ValidateTag 校验用户输入的 tag。
func ValidateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("tag 不能为空")
	}
	if !tagPattern.MatchString(tag) {
		return fmt.Errorf("tag %q 不合法（允许字母、数字、下划线、点、短横线，且不能以点或短横线开头）", tag)
	}
	return nil
}
