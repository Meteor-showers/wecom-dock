package dockerx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeUp struct {
	snap     snapshot
	pulledID string
	failAt   string
	steps    []string
}

func (f *fakeUp) step(name string) error {
	f.steps = append(f.steps, name)
	if f.failAt == name {
		return errors.New(name + " failed")
	}
	return nil
}

func (f *fakeUp) inspect(context.Context, string) (snapshot, error) {
	return f.snap, f.step("inspect")
}
func (f *fakeUp) pull(context.Context, string) error { return f.step("pull") }
func (f *fakeUp) imageID(context.Context, string) (string, error) {
	if err := f.step("imageID"); err != nil {
		return "", err
	}
	if f.pulledID != "" {
		return f.pulledID, nil
	}
	return f.snap.ImageID, nil
}
func (f *fakeUp) rename(context.Context, string, string) error { return f.step("rename") }
func (f *fakeUp) stop(context.Context, string) error           { return f.step("stop") }
func (f *fakeUp) createFrom(context.Context, string, string) (string, error) {
	return "new", f.step("create")
}
func (f *fakeUp) start(context.Context, string) error     { return f.step("start") }
func (f *fakeUp) remove(context.Context, string) error    { return f.step("remove") }
func (f *fakeUp) waitReady(context.Context, string) error { return f.step("wait") }

func TestRunUpdatePaths(t *testing.T) {
	base := snapshot{ID: "abc123456789", Name: "/nginx", ImageRef: "nginx:latest", ImageID: "sha256:old"}
	cases := []struct {
		name     string
		pulled   string
		failAt   string
		wantErr  string
		wantMsg  string
		wantStep string
	}{
		{name: "current", pulled: "sha256:old", wantMsg: "已经是最新镜像", wantStep: "inspect pull imageID"},
		{name: "ok", pulled: "sha256:new", wantMsg: "已更新", wantStep: "inspect pull imageID rename stop create start wait remove"},
		{name: "stop fails", pulled: "sha256:new", failAt: "stop", wantErr: "停止旧容器失败", wantStep: "inspect pull imageID rename stop rename start"},
		{name: "create fails", pulled: "sha256:new", failAt: "create", wantErr: "创建新容器失败", wantStep: "inspect pull imageID rename stop create rename start"},
		{name: "wait fails", pulled: "sha256:new", failAt: "wait", wantErr: "已回滚", wantStep: "inspect pull imageID rename stop create start wait stop remove rename start"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeUp{snap: base, pulledID: tc.pulled, failAt: tc.failAt}
			msg, err := runUpdate(context.Background(), f, base.ID, backupName(base.ID))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if msg != tc.wantMsg {
					t.Fatalf("msg %q", msg)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err %v", err)
			}
			if got := strings.Join(f.steps, " "); got != tc.wantStep {
				t.Fatalf("steps\n got %s\nwant %s", got, tc.wantStep)
			}
		})
	}
}

func TestBackupName(t *testing.T) {
	if got := backupName("sha256:abcdef1234567890"); got != "wecomdock-bak-abcdef123456" {
		t.Fatal(got)
	}
}
