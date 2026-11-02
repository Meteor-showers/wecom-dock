package bot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pluto/wecom-dock/internal/monitor"
	"github.com/pluto/wecom-dock/internal/store"
	"github.com/pluto/wecom-dock/internal/wecom"
)

type fakeMsg struct {
	mu      sync.Mutex
	replies []string
	sent    []string
	cards   int
}

func (f *fakeMsg) ReplyMarkdown(_ context.Context, _, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, content)
	return nil
}
func (f *fakeMsg) ReplyWelcome(context.Context, string, string) error   { return nil }
func (f *fakeMsg) UpdateCard(context.Context, string, wecom.Card) error { return nil }
func (f *fakeMsg) SendMarkdown(_ context.Context, _ string, _ int, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, content)
	return nil
}
func (f *fakeMsg) SendCard(context.Context, string, int, wecom.Card) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cards++
	return nil
}

type fakeCtl struct {
	mu      sync.Mutex
	reports []monitor.Report
	updated []string
}

func (f *fakeCtl) Reports(context.Context) ([]monitor.Report, error) { return f.reports, nil }
func (f *fakeCtl) UpgradeNamed(_ context.Context, name string) (monitor.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated = append(f.updated, name)
	return monitor.Result{Name: name, OK: true, Message: "已更新"}, nil
}

func TestRejectsUnknownAdmin(t *testing.T) {
	msg := &fakeMsg{}
	b := &Bot{Msg: msg, Admins: []string{"admin"}, Store: openStore(t)}
	b.OnMessage(context.Background(), wecom.Incoming{ReqID: "r", UserID: "guest", ChatID: "guest", ChatType: 1, Text: "状态"})
	if len(msg.replies) != 1 || !strings.Contains(msg.replies[0], "guest") {
		t.Fatal(msg.replies)
	}
	if len(b.Store.Sessions()) != 0 {
		t.Fatal("unauthorized chat was stored")
	}
}

func TestUpdateUsesResolvedName(t *testing.T) {
	msg := &fakeMsg{}
	ctl := &fakeCtl{reports: []monitor.Report{{
		Name: "nginx", Image: "nginx:latest", Kind: monitor.KindOutdated, RemoteDigest: "sha256:new",
	}}}
	b := &Bot{Msg: msg, Ctrl: ctl, Store: openStore(t)}
	b.OnMessage(context.Background(), wecom.Incoming{ReqID: "r", UserID: "u", ChatID: "u", ChatType: 1, Text: "更新 ng"})
	deadline := time.Now().Add(2 * time.Second)
	for {
		ctl.mu.Lock()
		n := len(ctl.updated)
		name := ""
		if n > 0 {
			name = ctl.updated[0]
		}
		ctl.mu.Unlock()
		if n == 1 && name == "nginx" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("updated %#v replies %#v", ctl.updated, msg.replies)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStatusRepliesBeforeTheScanFinishes(t *testing.T) {
	msg := &fakeMsg{}
	ctl := &fakeCtl{reports: []monitor.Report{{
		Name: "nginx", Image: "nginx:latest", Kind: monitor.KindCurrent,
	}}}
	b := &Bot{Msg: msg, Ctrl: ctl, Store: openStore(t)}
	b.OnMessage(context.Background(), wecom.Incoming{ReqID: "r", UserID: "u", ChatID: "u", ChatType: 1, Text: "状态"})
	msg.mu.Lock()
	gotAck := len(msg.replies) == 1 && strings.Contains(msg.replies[0], "正在查看")
	msg.mu.Unlock()
	if !gotAck {
		t.Fatal(msg.replies)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		msg.mu.Lock()
		n := len(msg.sent)
		body := ""
		if n > 0 {
			body = msg.sent[0]
		}
		msg.mu.Unlock()
		if n == 1 && strings.Contains(body, "nginx") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sent %#v", msg.sent)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNotifyNeedsASession(t *testing.T) {
	msg := &fakeMsg{}
	ctl := &fakeCtl{reports: []monitor.Report{{
		Name: "nginx", Kind: monitor.KindOutdated, RemoteDigest: "sha256:new",
	}}}
	b := &Bot{Msg: msg, Ctrl: ctl, Store: openStore(t)}
	b.Notify(context.Background())
	if len(msg.sent) != 0 {
		t.Fatal(msg.sent)
	}
	b.OnMessage(context.Background(), wecom.Incoming{ReqID: "r", UserID: "u", ChatID: "u", ChatType: 1, Text: "帮助"})
	b.Notify(context.Background())
	if len(msg.sent) != 1 || msg.cards != 1 {
		t.Fatalf("sent %#v cards %d", msg.sent, msg.cards)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.PathFor(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
