package store

import "testing"

func TestRememberIgnoreAndTask(t *testing.T) {
	s, err := Open(PathFor(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Remember(Session{ChatID: "u1", ChatType: 1, UserID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Remember(Session{ChatID: "u1", ChatType: 1, UserID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Sessions(); len(got) != 1 {
		t.Fatalf("sessions %d", len(got))
	}
	if err := s.Ignore("nginx", "sha256:abc"); err != nil {
		t.Fatal(err)
	}
	if s.Ignored("nginx") != "sha256:abc" {
		t.Fatal(s.Ignored("nginx"))
	}
	if err := s.ClearIgnore("nginx"); err != nil {
		t.Fatal(err)
	}
	if s.Ignored("nginx") != "" {
		t.Fatal("still ignored")
	}
	if err := s.PutTask(Task{ID: "t1", Name: "nginx", RemoteDigest: "sha256:def"}); err != nil {
		t.Fatal(err)
	}
	task, ok := s.TakeTask("t1")
	if !ok || task.Name != "nginx" {
		t.Fatalf("%+v %v", task, ok)
	}
	if _, ok := s.TakeTask("t1"); ok {
		t.Fatal("task reused")
	}
	if err := s.MarkNotified("nginx", "sha256:def"); err != nil {
		t.Fatal(err)
	}
	reopen, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopen.Notified("nginx") != "sha256:def" {
		t.Fatal(reopen.Notified("nginx"))
	}
}
