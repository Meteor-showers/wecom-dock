package wecom

// Card is a button card. Values are truncated to WeCom's field limits.
type Card struct {
	TaskID  string
	Title   string
	Desc    string
	Rows    [][2]string
	Buttons []Button
}

// Button is one action on a card.
type Button struct {
	Text  string
	Key   string
	Style int
}

func cardPayload(card Card) map[string]any {
	buttons := make([]map[string]any, 0, len(card.Buttons))
	for _, b := range card.Buttons {
		style := b.Style
		if style == 0 {
			style = 1
		}
		buttons = append(buttons, map[string]any{
			"text":  trunc(b.Text, 20),
			"style": style,
			"key":   trunc(b.Key, 128),
		})
	}
	payload := map[string]any{
		"card_type": "button_interaction",
		"main_title": map[string]string{
			"title": trunc(card.Title, 36),
			"desc":  trunc(card.Desc, 44),
		},
		"task_id":     card.TaskID,
		"button_list": buttons,
	}
	if len(card.Rows) > 0 {
		rows := make([]map[string]string, 0, len(card.Rows))
		for _, row := range card.Rows {
			rows = append(rows, map[string]string{
				"keyname": trunc(row[0], 5),
				"value":   trunc(row[1], 30),
			})
		}
		payload["horizontal_content_list"] = rows
	}
	return payload
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
