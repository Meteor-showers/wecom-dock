package registry

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pluto/wecom-dock/internal/image"
)

func TestParseBearer(t *testing.T) {
	realm, service, scope := parseBearer(`Bearer realm="https://auth.example/token",service="registry",scope="repository:library/nginx:pull"`)
	if realm != "https://auth.example/token" || service != "registry" || scope != "repository:library/nginx:pull" {
		t.Fatalf("got %s %s %s", realm, service, scope)
	}
}

func TestCanonHost(t *testing.T) {
	if canonHost("https://index.docker.io/v1/") != "docker.io" {
		t.Fatal(canonHost("https://index.docker.io/v1/"))
	}
	if canonHost("GHCR.io") != "ghcr.io" {
		t.Fatal()
	}
}

func TestLookupAnonymous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/library/nginx/manifests/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		w.Header().Set("Docker-Content-Digest", "sha256:abc")
		_, _ = w.Write([]byte(`{"mediaType":"application/vnd.docker.distribution.manifest.v2+json"}`))
	}))
	defer srv.Close()

	ref := image.Ref{Registry: "localhost", Repository: "library/nginx", Tag: "latest"}
	// Point BaseURL at the test server by using a custom client method via the server host.
	c := &Client{HTTP: srv.Client(), creds: map[string]credential{}}
	got, err := c.lookupURL(context.Background(), srv.URL, ref, "linux", "arm64", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != "sha256:abc" {
		t.Fatal(got)
	}
}

func TestLookupBearerAndPlatform(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"token":"tok"}`))
		case r.URL.Path == "/v2/org/app/manifests/main":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.Header().Set("Www-Authenticate", `Bearer realm="`+srv.URL+`/token",service="reg",scope="repository:org/app:pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			w.Header().Set("Docker-Content-Digest", "sha256:index")
			_, _ = w.Write([]byte(`{
				"manifests": [
					{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}},
					{"digest":"sha256:att","platform":{"os":"unknown","architecture":"unknown"}}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ref := image.Ref{Registry: "ghcr.io", Repository: "org/app", Tag: "main"}
	c := &Client{HTTP: srv.Client(), creds: map[string]credential{}}
	got, err := c.lookupURL(context.Background(), srv.URL, ref, "linux", "arm64", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != "sha256:index" || got.PlatformDigest != "sha256:arm" {
		t.Fatalf("%+v", got)
	}
}

func TestLoadDockerConfig(t *testing.T) {
	dir := t.TempDir()
	auth := base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	raw := []byte(`{"auths":{"https://index.docker.io/v1/":{"auth":"` + auth + `"},"ghcr.io":{"username":"bob","password":"pw"}}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New(dir, []Auth{{Host: "ghcr.io", Username: "carol", Password: "override"}})
	if err != nil {
		t.Fatal(err)
	}
	user, pass, ok := c.credential("docker.io")
	if !ok || user != "alice" || pass != "s3cret" {
		t.Fatalf("docker hub %s %s %v", user, pass, ok)
	}
	user, pass, ok = c.credential("ghcr.io")
	if !ok || user != "carol" || pass != "override" {
		t.Fatalf("override %s %s", user, pass)
	}
	encoded, err := c.DockerAuth("ghcr.io/org/app:main")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !strings.Contains(string(decoded), "carol") {
		t.Fatalf("%s", decoded)
	}
}

func (c *Client) lookupURL(ctx context.Context, base string, ref image.Ref, osName, arch, variant string) (Manifest, error) {
	endpoint := strings.TrimRight(base, "/") + "/v2/" + ref.Repository + "/manifests/" + ref.Tag
	status, hdr, body, err := c.do(ctx, endpoint, "")
	if err != nil {
		return Manifest{}, err
	}
	if status == http.StatusUnauthorized {
		token, terr := c.token(ctx, ref, hdr.Get("Www-Authenticate"))
		if terr != nil {
			return Manifest{}, terr
		}
		status, hdr, body, err = c.do(ctx, endpoint, token)
		if err != nil {
			return Manifest{}, err
		}
	}
	if status < 200 || status >= 300 {
		return Manifest{}, errStatus(status)
	}
	out := Manifest{Digest: hdr.Get("Docker-Content-Digest"), MediaType: hdr.Get("Content-Type")}
	if isIndex(out.MediaType, body) {
		out.PlatformDigest = platformDigest(body, osName, arch, variant)
	}
	return out, nil
}

func errStatus(code int) error {
	return &statusErr{code}
}

type statusErr struct{ code int }

func (e *statusErr) Error() string { return http.StatusText(e.code) }
