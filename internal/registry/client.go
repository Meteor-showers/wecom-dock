package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pluto/wecom-dock/internal/image"
)

const acceptManifests = "application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json"

// Manifest is what a tag currently points at.
type Manifest struct {
	Digest         string
	PlatformDigest string
	MediaType      string
}

// Auth is a static registry login.
type Auth struct {
	Host     string
	Username string
	Password string
}

type credential struct {
	Username string
	Password string
}

// Client looks up tags and turns registry logins into Docker pull auth.
type Client struct {
	HTTP       *http.Client
	creds      map[string]credential
	credsStore string
}

// New reads ~/.docker/config.json (or dockerConfigDir) and extra accounts.
// A missing docker config is not an error. credsStore logins are not readable here.
func New(dockerConfigDir string, extra []Auth) (*Client, error) {
	c := &Client{
		HTTP:  &http.Client{Timeout: 30 * time.Second},
		creds: map[string]credential{},
	}
	loaded, credsStore, err := loadDockerConfig(dockerConfigDir)
	if err != nil {
		return nil, err
	}
	for host, cred := range loaded {
		c.creds[canonHost(host)] = cred
	}
	for _, a := range extra {
		if a.Host == "" || a.Username == "" {
			continue
		}
		c.creds[canonHost(a.Host)] = credential{Username: a.Username, Password: a.Password}
	}
	c.credsStore = credsStore
	return c, nil
}

// CredsStore reports whether the docker config delegates secrets to a helper.
func (c *Client) CredsStore() string { return c.credsStore }

// Lookup returns the tag digest. osName/arch/variant select a platform manifest inside an index.
func (c *Client) Lookup(ctx context.Context, ref image.Ref, osName, arch, variant string) (Manifest, error) {
	if ref.Pinned {
		return Manifest{}, fmt.Errorf("镜像已钉死 digest")
	}
	if osName == "" {
		osName = "linux"
	}
	body, media, digest, err := c.getManifest(ctx, ref)
	if err != nil {
		return Manifest{}, err
	}
	if digest == "" {
		return Manifest{}, fmt.Errorf("仓库没有返回 Docker-Content-Digest")
	}
	out := Manifest{Digest: digest, MediaType: media}
	if isIndex(media, body) {
		out.PlatformDigest = platformDigest(body, osName, arch, variant)
	}
	return out, nil
}

// DockerAuth is the base64 RegistryAuth header value for a docker pull. Empty means anonymous.
func (c *Client) DockerAuth(imageRef string) (string, error) {
	ref, err := image.Parse(imageRef)
	if err != nil {
		return "", err
	}
	user, pass, ok := c.credential(ref.Registry)
	if !ok {
		return "", nil
	}
	payload, err := json.Marshal(map[string]string{
		"username":      user,
		"password":      pass,
		"serveraddress": ref.Registry,
	})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(payload), nil
}

func (c *Client) credential(registry string) (string, string, bool) {
	cred, ok := c.creds[canonHost(registry)]
	if !ok || cred.Username == "" {
		return "", "", false
	}
	return cred.Username, cred.Password, true
}

func (c *Client) getManifest(ctx context.Context, ref image.Ref) ([]byte, string, string, error) {
	endpoint := ref.BaseURL() + "/v2/" + ref.Repository + "/manifests/" + url.PathEscape(ref.Tag)
	status, hdr, body, err := c.do(ctx, endpoint, "")
	if err != nil {
		return nil, "", "", err
	}
	if status == http.StatusUnauthorized {
		token, terr := c.token(ctx, ref, hdr.Get("Www-Authenticate"))
		if terr != nil {
			return nil, "", "", terr
		}
		status, hdr, body, err = c.do(ctx, endpoint, token)
		if err != nil {
			return nil, "", "", err
		}
	}
	if status == http.StatusNotFound {
		return nil, "", "", fmt.Errorf("仓库里没有 %s:%s", ref.Repository, ref.Tag)
	}
	if status == http.StatusTooManyRequests {
		return nil, "", "", fmt.Errorf("仓库限流")
	}
	if status < 200 || status >= 300 {
		return nil, "", "", fmt.Errorf("读取清单失败 HTTP %d", status)
	}
	return body, hdr.Get("Content-Type"), hdr.Get("Docker-Content-Digest"), nil
}

func (c *Client) do(ctx context.Context, endpoint, token string) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Accept", acceptManifests)
	req.Header.Set("User-Agent", "wecom-dock")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, resp.Header, nil, err
	}
	return resp.StatusCode, resp.Header.Clone(), body, nil
}

func (c *Client) token(ctx context.Context, ref image.Ref, challenge string) (string, error) {
	realm, service, scope := parseBearer(challenge)
	if realm == "" {
		return "", fmt.Errorf("仓库要求认证，但没有给出 Bearer 地址")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if service != "" {
		q.Set("service", service)
	}
	if scope == "" {
		scope = "repository:" + ref.Repository + ":pull"
	}
	q.Set("scope", scope)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if user, pass, ok := c.credential(ref.Registry); ok {
		req.SetBasicAuth(user, pass)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("获取仓库令牌失败 HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if parsed.Token != "" {
		return parsed.Token, nil
	}
	if parsed.AccessToken != "" {
		return parsed.AccessToken, nil
	}
	return "", fmt.Errorf("仓库令牌响应是空的")
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func canonHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimSuffix(h, "/")
	if i := strings.Index(h, "/"); i >= 0 {
		h = h[:i]
	}
	switch h {
	case "docker.io", "index.docker.io", "registry-1.docker.io":
		return "docker.io"
	default:
		return h
	}
}

func parseBearer(header string) (realm, service, scope string) {
	h := strings.TrimSpace(header)
	if len(h) < len("bearer ") || !strings.EqualFold(h[:len("bearer ")], "bearer ") {
		return "", "", ""
	}
	for _, part := range splitComma(h[len("bearer "):]) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "realm":
			realm = v
		case "service":
			service = v
		case "scope":
			scope = v
		}
	}
	return realm, service, scope
}

func splitComma(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			b.WriteRune(r)
		case r == ',' && !quoted:
			if part := strings.TrimSpace(b.String()); part != "" {
				out = append(out, part)
			}
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if part := strings.TrimSpace(b.String()); part != "" {
		out = append(out, part)
	}
	return out
}

type indexDoc struct {
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			Variant      string `json:"variant"`
		} `json:"platform"`
	} `json:"manifests"`
}

func isIndex(media string, body []byte) bool {
	m := strings.ToLower(media)
	if strings.Contains(m, "manifest.list") || strings.Contains(m, "image.index") {
		return true
	}
	var probe struct {
		Manifests []json.RawMessage `json:"manifests"`
	}
	if json.Unmarshal(body, &probe) == nil && len(probe.Manifests) > 0 {
		return true
	}
	return false
}

func platformDigest(body []byte, osName, arch, variant string) string {
	var doc indexDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	var fallback string
	for _, m := range doc.Manifests {
		p := m.Platform
		if p.OS == "" || p.OS == "unknown" || p.Architecture == "" || p.Architecture == "unknown" {
			continue
		}
		if p.OS != osName || p.Architecture != arch {
			continue
		}
		if variant != "" && p.Variant != "" && p.Variant != variant {
			if fallback == "" {
				fallback = m.Digest
			}
			continue
		}
		return m.Digest
	}
	return fallback
}

func loadDockerConfig(dir string) (map[string]credential, string, error) {
	path, err := dockerConfigPath(dir)
	if err != nil {
		return nil, "", err
	}
	if path == "" {
		return nil, "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	var parsed struct {
		Auths map[string]struct {
			Auth     string `json:"auth"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"auths"`
		CredsStore string `json:"credsStore"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", fmt.Errorf("解析 %s: %w", path, err)
	}
	out := map[string]credential{}
	for host, item := range parsed.Auths {
		user, pass := item.Username, item.Password
		if item.Auth != "" && user == "" {
			decoded, err := base64.StdEncoding.DecodeString(item.Auth)
			if err != nil {
				continue
			}
			user, pass, _ = strings.Cut(string(decoded), ":")
		}
		if user == "" {
			continue
		}
		out[host] = credential{Username: user, Password: pass}
	}
	return out, parsed.CredsStore, nil
}

func dockerConfigPath(dir string) (string, error) {
	if dir == "" {
		dir = os.Getenv("DOCKER_CONFIG")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil
		}
		dir = filepath.Join(home, ".docker")
	}
	if strings.HasSuffix(dir, "config.json") {
		return dir, nil
	}
	path := filepath.Join(dir, "config.json")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return path, nil
}
