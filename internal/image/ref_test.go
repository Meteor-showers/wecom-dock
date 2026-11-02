package image

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in       string
		registry string
		repo     string
		tag      string
		pinned   bool
		apiHost  string
		wantBase string
	}{
		{"nginx", "docker.io", "library/nginx", "latest", false, "registry-1.docker.io", "https://registry-1.docker.io"},
		{"nginx:1.27", "docker.io", "library/nginx", "1.27", false, "registry-1.docker.io", "https://registry-1.docker.io"},
		{"library/nginx:1.27", "docker.io", "library/nginx", "1.27", false, "registry-1.docker.io", "https://registry-1.docker.io"},
		{"user/app", "docker.io", "user/app", "latest", false, "registry-1.docker.io", "https://registry-1.docker.io"},
		{"ghcr.io/org/app:main", "ghcr.io", "org/app", "main", false, "ghcr.io", "https://ghcr.io"},
		{"localhost:5000/foo/bar:dev", "localhost:5000", "foo/bar", "dev", false, "localhost:5000", "http://localhost:5000"},
		{"nginx@sha256:abc", "docker.io", "library/nginx", "latest", true, "registry-1.docker.io", "https://registry-1.docker.io"},
		{"nginx:1.27@sha256:abc", "docker.io", "library/nginx", "1.27", true, "registry-1.docker.io", "https://registry-1.docker.io"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Parse(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Registry != tc.registry || got.Repository != tc.repo || got.Tag != tc.tag || got.Pinned != tc.pinned {
				t.Fatalf("got %+v", got)
			}
			if got.RegistryAPIHost() != tc.apiHost || got.BaseURL() != tc.wantBase {
				t.Fatalf("host %s base %s", got.RegistryAPIHost(), got.BaseURL())
			}
		})
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse("  "); err == nil {
		t.Fatal("expected error")
	}
}
