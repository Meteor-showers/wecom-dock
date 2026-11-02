package monitor

import (
	"testing"

	"github.com/pluto/wecom-dock/internal/dockerx"
)

func TestFind(t *testing.T) {
	list := []dockerx.Container{{Name: "nginx"}, {Name: "nginx-proxy"}, {Name: "redis"}}
	got, err := Find(list, "redis")
	if err != nil || got.Name != "redis" {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = Find(list, "nginx")
	if err != nil || got.Name != "nginx" {
		t.Fatalf("exact %+v %v", got, err)
	}
	if _, err = Find(list, "ng"); err == nil {
		t.Fatal("expected ambiguous prefix")
	}
	if _, err = Find(list, "missing"); err == nil {
		t.Fatal("expected missing")
	}
}
