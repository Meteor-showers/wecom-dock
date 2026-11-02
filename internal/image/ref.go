package image

import (
	"fmt"
	"net"
	"strings"
)

// Ref is a parsed container image reference.
type Ref struct {
	Original   string
	Registry   string
	Repository string
	Tag        string
	Digest     string
	Pinned     bool
}

// Parse splits an image reference the way the Docker daemon does.
// A digest pin (image@sha256:...) is not a moving tag, so Pinned is set.
func Parse(raw string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Ref{}, fmt.Errorf("空镜像名")
	}
	ref := Ref{Original: raw}
	name := raw
	if i := strings.LastIndex(name, "@"); i >= 0 {
		ref.Digest = name[i+1:]
		ref.Pinned = ref.Digest != ""
		name = name[:i]
	}
	lastSlash := strings.LastIndex(name, "/")
	lastColon := strings.LastIndex(name, ":")
	if lastColon > lastSlash {
		ref.Tag = name[lastColon+1:]
		name = name[:lastColon]
	} else {
		ref.Tag = "latest"
	}
	if name == "" || ref.Tag == "" {
		return Ref{}, fmt.Errorf("无法解析镜像 %q", raw)
	}

	first, rest, hasSlash := strings.Cut(name, "/")
	switch {
	case !hasSlash:
		ref.Registry = "docker.io"
		ref.Repository = "library/" + name
	case first == "localhost" || strings.ContainsAny(first, ".:"):
		ref.Registry = first
		ref.Repository = rest
	default:
		ref.Registry = "docker.io"
		ref.Repository = name
	}
	return ref, nil
}

// RegistryAPIHost is the registry HTTP host. Docker Hub's API host is not docker.io.
func (r Ref) RegistryAPIHost() string {
	switch r.Registry {
	case "docker.io", "index.docker.io":
		return "registry-1.docker.io"
	default:
		return r.Registry
	}
}

// BaseURL is the registry origin, using http only for loopback.
func (r Ref) BaseURL() string {
	host := r.RegistryAPIHost()
	bare := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		bare = h
	}
	scheme := "https"
	if bare == "localhost" || bare == "127.0.0.1" || bare == "::1" {
		scheme = "http"
	}
	return scheme + "://" + host
}
