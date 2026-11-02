package wecom

import "testing"

func TestCardPayloadTruncates(t *testing.T) {
	card := Card{
		TaskID: "t1",
		Title:  "镜像有新版本",
		Desc:   "nginx",
		Rows:   [][2]string{{"镜像", "nginx:1.27-this-name-is-longer-than-thirty-runes"}},
		Buttons: []Button{
			{Text: "立即更新", Key: "update", Style: 1},
			{Text: "忽略本版本", Key: "ignore", Style: 2},
		},
	}
	payload := cardPayload(card)
	rows := payload["horizontal_content_list"].([]map[string]string)
	if len([]rune(rows[0]["value"])) != 30 {
		t.Fatal(rows[0]["value"])
	}
	if payload["task_id"] != "t1" {
		t.Fatal(payload["task_id"])
	}
	buttons := payload["button_list"].([]map[string]any)
	if buttons[1]["key"] != "ignore" {
		t.Fatal(buttons)
	}
}
