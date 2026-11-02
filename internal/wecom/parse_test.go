package wecom

import "testing"

func TestParseTextAndGroup(t *testing.T) {
	raw := []byte(`{
		"msgid":"m1",
		"chatid":"wr123",
		"chattype":"group",
		"from":{"userid":"zhang"},
		"msgtype":"text",
		"text":{"content":"@机器人 状态"}
	}`)
	got, err := parseBody("req1", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatType != 2 || got.ChatID != "wr123" || got.UserID != "zhang" || got.Text != "@机器人 状态" || got.ReqID != "req1" {
		t.Fatalf("%+v", got)
	}
}

func TestParseCardEventShapes(t *testing.T) {
	nested := []byte(`{"from":{"userid":"u"},"event":{"eventtype":"template_card_event","template_card_event":{"event_key":"update","task_id":"t1"}}}`)
	got, err := parseBody("r", nested)
	if err != nil {
		t.Fatal(err)
	}
	if got.Event != "template_card_event" || got.EventKey != "update" || got.TaskID != "t1" || got.ChatID != "u" {
		t.Fatalf("%+v", got)
	}
	flat := []byte(`{"from":{"userid":"u"},"event":{"eventtype":"template_card_event","event_key":"ignore","task_id":"t2"}}`)
	got, err = parseBody("r", flat)
	if err != nil {
		t.Fatal(err)
	}
	if got.EventKey != "ignore" || got.TaskID != "t2" {
		t.Fatalf("%+v", got)
	}
}

func TestParseEnterChat(t *testing.T) {
	raw := []byte(`{"from":{"userid":"u"},"msgtype":"event","event":{"eventtype":"enter_chat"}}`)
	got, err := parseBody("r", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Event != "enter_chat" || got.UserID != "u" {
		t.Fatalf("%+v", got)
	}
}
