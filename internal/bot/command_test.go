package bot

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in     string
		action string
		arg    string
	}{
		{"帮助", "help", ""},
		{"@机器人 状态", "status", ""},
		{"/check", "check", ""},
		{"我的id", "whoami", ""},
		{"更新 nginx", "update", "nginx"},
		{"UPDATE Redis", "update", "Redis"},
		{"更新全部", "updateall", ""},
		{"更新 all", "updateall", ""},
		{"忽略 nginx", "ignore", "nginx"},
		{"取消忽略 nginx", "unignore", "nginx"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Parse(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != tc.action || got.Arg != tc.arg {
				t.Fatalf("%+v", got)
			}
		})
	}
	if _, err := Parse("更新"); err == nil {
		t.Fatal("expected missing name")
	}
	if _, err := Parse("随便说"); err == nil {
		t.Fatal("expected unknown")
	}
}
