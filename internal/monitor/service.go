package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pluto/wecom-dock/internal/dockerx"
	"github.com/pluto/wecom-dock/internal/image"
	"github.com/pluto/wecom-dock/internal/registry"
)

// Report is the result of checking one container.
type Report struct {
	ID           string
	Name         string
	Image        string
	State        string
	Kind         string
	Detail       string
	LocalDigest  string
	RemoteDigest string
}

// Result is the outcome of an upgrade request.
type Result struct {
	Name    string
	Message string
	OK      bool
}

// Service checks registries and asks Docker to recreate containers.
type Service struct {
	Docker         *dockerx.Client
	Reg            *registry.Client
	IncludeStopped bool
	OnlyLabeled    bool
	HealthTimeout  time.Duration
}

// Reports checks every selected container.
func (s *Service) Reports(ctx context.Context) ([]Report, error) {
	list, err := s.Docker.List(ctx, s.IncludeStopped)
	if err != nil {
		return nil, err
	}
	out := make([]Report, 0, len(list))
	for _, ct := range list {
		out = append(out, s.one(ctx, ct))
	}
	return out, nil
}

// UpgradeNamed pulls and recreates one container.
func (s *Service) UpgradeNamed(ctx context.Context, name string) (Result, error) {
	list, err := s.Docker.List(ctx, true)
	if err != nil {
		return Result{}, err
	}
	ct, err := Find(list, name)
	if err != nil {
		return Result{Message: err.Error()}, nil
	}
	res := Result{Name: ct.Name}
	if truthy(ct.Labels[LabelSkip]) {
		res.Message = "这个容器标记了 wecomdock.skip，不会通过机器人更新"
		return res, nil
	}
	ref, err := image.Parse(ct.Image)
	if err != nil {
		res.Message = err.Error()
		return res, nil
	}
	if ref.Pinned {
		res.Message = "镜像已钉死 digest，没有可跟随的标签"
		return res, nil
	}
	auth, err := s.Reg.DockerAuth(ct.Image)
	if err != nil {
		return Result{}, err
	}
	msg, err := s.Docker.Upgrade(ctx, ct.ID, auth, s.HealthTimeout)
	res.Message = msg
	res.OK = err == nil
	if err != nil {
		if msg != "" {
			res.Message = msg
		}
		return res, err
	}
	return res, nil
}

func (s *Service) one(ctx context.Context, ct dockerx.Container) Report {
	r := Report{ID: ct.ID, Name: ct.Name, Image: ct.Image, State: ct.State}
	if len(ct.Digests) > 0 {
		r.LocalDigest = ct.Digests[0]
	}
	if skip, reason := ClassifyLabel(ct.Labels, s.OnlyLabeled); skip {
		r.Kind = KindSkipped
		r.Detail = reason
		return r
	}
	ref, err := image.Parse(ct.Image)
	if err != nil {
		r.Kind = KindError
		r.Detail = err.Error()
		return r
	}
	if ref.Pinned {
		r.Kind = KindPinned
		return r
	}
	if len(ct.Digests) == 0 {
		r.Kind = KindSkipped
		r.Detail = "没有仓库摘要，可能是本地构建"
		return r
	}
	osName, arch := ct.OS, ct.Arch
	if osName == "" {
		osName = "linux"
	}
	man, err := s.Reg.Lookup(ctx, ref, osName, arch, ct.Variant)
	if err != nil {
		r.Kind = KindError
		r.Detail = err.Error()
		return r
	}
	r.RemoteDigest = man.Digest
	if IsCurrent(ct.Digests, man.Digest, man.PlatformDigest) {
		r.Kind = KindCurrent
		return r
	}
	r.Kind = KindOutdated
	return r
}

// Find picks one container by exact name, or by a unique prefix.
func Find(list []dockerx.Container, query string) (dockerx.Container, error) {
	q := strings.TrimPrefix(strings.TrimSpace(query), "/")
	if q == "" {
		return dockerx.Container{}, fmt.Errorf("要写容器名")
	}
	var exact, prefix []dockerx.Container
	for _, ct := range list {
		if strings.EqualFold(ct.Name, q) {
			exact = append(exact, ct)
			continue
		}
		if strings.HasPrefix(strings.ToLower(ct.Name), strings.ToLower(q)) {
			prefix = append(prefix, ct)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) > 1:
		return dockerx.Container{}, fmt.Errorf("有多个同名容器：%s", joinNames(exact))
	case len(prefix) == 1:
		return prefix[0], nil
	case len(prefix) > 1:
		return dockerx.Container{}, fmt.Errorf("匹配到多个容器：%s", joinNames(prefix))
	default:
		return dockerx.Container{}, fmt.Errorf("没有找到容器 %s", q)
	}
}

func joinNames(list []dockerx.Container) string {
	names := make([]string, 0, len(list))
	for _, ct := range list {
		names = append(names, ct.Name)
	}
	return strings.Join(names, "、")
}
