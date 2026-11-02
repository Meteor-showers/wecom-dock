package wecom

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Incoming is a text message or a card click from the long connection.
type Incoming struct {
	ReqID    string
	MsgID    string
	ChatID   string
	ChatType int
	UserID   string
	Text     string
	Event    string
	EventKey string
	TaskID   string
}

type callbackBody struct {
	MsgID    string     `json:"msgid"`
	ChatID   string     `json:"chatid"`
	ChatType flexString `json:"chattype"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Event json.RawMessage `json:"event"`
}

func parseBody(reqID string, raw json.RawMessage) (Incoming, error) {
	var body callbackBody
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			return Incoming{}, err
		}
	}
	in := Incoming{
		ReqID:    reqID,
		MsgID:    body.MsgID,
		UserID:   body.From.UserID,
		Text:     strings.TrimSpace(body.Text.Content),
		ChatType: 1,
		ChatID:   body.From.UserID,
	}
	if strings.EqualFold(string(body.ChatType), "group") || string(body.ChatType) == "2" {
		in.ChatType = 2
		if body.ChatID != "" {
			in.ChatID = body.ChatID
		}
	} else if body.ChatID != "" && body.From.UserID == "" {
		in.ChatID = body.ChatID
	}
	if len(body.Event) > 0 && string(body.Event) != "null" {
		eventType, key, task := parseEvent(body.Event)
		in.Event = eventType
		in.EventKey = key
		in.TaskID = task
	}
	return in, nil
}

func parseEvent(raw json.RawMessage) (eventType, key, task string) {
	var ev struct {
		EventType string `json:"eventtype"`
		EventKey  string `json:"event_key"`
		TaskID    string `json:"task_id"`
		Nested    struct {
			EventKey string `json:"event_key"`
			TaskID   string `json:"task_id"`
		} `json:"template_card_event"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return "", "", ""
	}
	key = ev.EventKey
	task = ev.TaskID
	if key == "" {
		key = ev.Nested.EventKey
	}
	if task == "" {
		task = ev.Nested.TaskID
	}
	return ev.EventType, key, task
}

// flexString accepts a JSON string or number. WeCom documents chattype as a string,
// and some callbacks send 1 or 2.
type flexString string

func (s *flexString) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		*s = flexString(v)
		return nil
	}
	*s = flexString(raw)
	return nil
}
