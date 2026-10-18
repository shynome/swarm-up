package docker

import (
	"strings"
	"testing"
)

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseRef(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Ref
	}{
		{"默认 latest", "nginx", Ref{Repo: "nginx", Tag: "latest"}},
		{"带 tag", "nginx:1.25", Ref{Repo: "nginx", Tag: "1.25"}},
		{"带 digest", "nginx@" + testDigest, Ref{Repo: "nginx", Tag: "latest", Digest: testDigest}},
		{"tag 与 digest", "nginx:1.25@" + testDigest, Ref{Repo: "nginx", Tag: "1.25", Digest: testDigest}},
		{"私有 registry 端口", "registry.local:5000/acme/api", Ref{Repo: "registry.local:5000/acme/api", Tag: "latest"}},
		{"私有 registry 端口与 tag", "registry.local:5000/acme/api:v1", Ref{Repo: "registry.local:5000/acme/api", Tag: "v1"}},
		{"多级路径", "ghcr.io/acme/team/api:v0.0.2@" + testDigest, Ref{Repo: "ghcr.io/acme/team/api", Tag: "v0.0.2", Digest: testDigest}},
		{"空白", "  nginx:1  ", Ref{Repo: "nginx", Tag: "1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRef(tc.in)
			if err != nil {
				t.Fatalf("ParseRef(%q) 报错: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseRef(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseRefInvalid(t *testing.T) {
	for _, in := range []string{"", "   ", "nginx@", "@" + testDigest, ":"} {
		if _, err := ParseRef(in); err == nil {
			t.Fatalf("ParseRef(%q) 应该报错", in)
		}
	}
}

func TestRefString(t *testing.T) {
	ref := Ref{Repo: "acme/api", Tag: "v1", Digest: testDigest}
	if got, want := ref.String(), "acme/api:v1@"+testDigest; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if got, want := ref.RepoTag(), "acme/api:v1"; got != want {
		t.Fatalf("RepoTag() = %q, want %q", got, want)
	}
	if got, want := (Ref{Repo: "acme/api"}).String(), "acme/api:latest"; got != want {
		t.Fatalf("空 tag 的 String() = %q, want %q", got, want)
	}
}

func TestWithTagDropsDigest(t *testing.T) {
	ref := Ref{Repo: "acme/api", Tag: "v1", Digest: testDigest}
	next := ref.WithTag("v2")
	if next.Digest != "" {
		t.Fatalf("WithTag 应丢弃旧 digest, got %q", next.Digest)
	}
	if got, want := next.RepoTag(), "acme/api:v2"; got != want {
		t.Fatalf("RepoTag() = %q, want %q", got, want)
	}
}

func TestValidateTag(t *testing.T) {
	for _, tag := range []string{"v0.0.3", "1.25", "latest", "sha-abc_1"} {
		if err := ValidateTag(tag); err != nil {
			t.Fatalf("ValidateTag(%q) 不应报错: %v", tag, err)
		}
	}
	for _, tag := range []string{"", "-bad", ".bad", "a b", "a/b", strings.Repeat("x", 129)} {
		if err := ValidateTag(tag); err == nil {
			t.Fatalf("ValidateTag(%q) 应该报错", tag)
		}
	}
}
