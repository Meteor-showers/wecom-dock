package monitor

import "strings"

const (
	LabelSkip   = "wecomdock.skip"
	LabelEnable = "wecomdock.enable"

	KindCurrent  = "current"
	KindOutdated = "outdated"
	KindIgnored  = "ignored"
	KindSkipped  = "skipped"
	KindPinned   = "pinned"
	KindError    = "error"
)

// ClassifyLabel decides whether a container is left out of scans.
func ClassifyLabel(labels map[string]string, onlyLabeled bool) (bool, string) {
	if truthy(labels[LabelSkip]) {
		return true, "标签 wecomdock.skip"
	}
	if onlyLabeled && !truthy(labels[LabelEnable]) {
		return true, "未标记 wecomdock.enable"
	}
	return false, ""
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// IsCurrent reports whether the local image already matches the tag.
func IsCurrent(local []string, remote, platform string) bool {
	if remote != "" && containsDigest(local, remote) {
		return true
	}
	if platform != "" && containsDigest(local, platform) {
		return true
	}
	return false
}

func containsDigest(local []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, item := range local {
		item = strings.TrimSpace(item)
		if item == want || strings.HasSuffix(item, "@"+want) {
			return true
		}
	}
	return false
}

// ShortDigest renders a digest for a chat message.
func ShortDigest(digest string) string {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return "未知"
	}
	hexPart := digest
	if i := strings.LastIndex(digest, ":"); i >= 0 {
		hexPart = digest[i+1:]
	}
	if len(hexPart) > 12 {
		hexPart = hexPart[:12]
	}
	return "sha256:" + hexPart
}
