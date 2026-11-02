package monitor

import "testing"

func TestClassifyLabel(t *testing.T) {
	skip, _ := ClassifyLabel(map[string]string{LabelSkip: "true"}, false)
	if !skip {
		t.Fatal("skip")
	}
	skip, reason := ClassifyLabel(map[string]string{}, true)
	if !skip || reason == "" {
		t.Fatal(reason)
	}
	skip, _ = ClassifyLabel(map[string]string{LabelEnable: "yes"}, true)
	if skip {
		t.Fatal("enabled")
	}
}

func TestIsCurrent(t *testing.T) {
	local := []string{"nginx@sha256:index", "nginx@sha256:arm"}
	if !IsCurrent(local, "sha256:index", "") {
		t.Fatal("index")
	}
	if !IsCurrent(local, "sha256:other", "sha256:arm") {
		t.Fatal("platform")
	}
	if IsCurrent(local, "sha256:new", "sha256:newplat") {
		t.Fatal("should be outdated")
	}
}

func TestShortDigest(t *testing.T) {
	if got := ShortDigest("sha256:0123456789abcdef"); got != "sha256:0123456789ab" {
		t.Fatal(got)
	}
	if got := ShortDigest(""); got != "未知" {
		t.Fatal(got)
	}
}
