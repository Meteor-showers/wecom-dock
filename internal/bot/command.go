package bot

import (
	"fmt"
	"strings"
)

// Command is a user instruction.
type Command struct {
	Action string
	Arg    string
}

// Parse reads a chat message. Mentions and a leading slash are ignored.
func Parse(input string) (Command, error) {
	line := normalize(input)
	if line == "" {
		return Command{}, fmt.Errorf("空指令，发「帮助」看用法")
	}
	low := strings.ToLower(line)
	switch low {
	case "help", "帮助", "?", "h":
		return Command{Action: "help"}, nil
	case "status", "状态", "ps", "列表":
		return Command{Action: "status"}, nil
	case "check", "检查", "scan":
		return Command{Action: "check"}, nil
	case "whoami", "我的id", "id":
		return Command{Action: "whoami"}, nil
	case "更新全部", "全部更新", "update all", "upgrade all", "更新 全部", "updateall":
		return Command{Action: "updateall"}, nil
	}
	if arg, ok := cut(line, "取消忽略"); ok {
		return needArg("unignore", arg)
	}
	if arg, ok := cut(line, "unignore"); ok {
		return needArg("unignore", arg)
	}
	if arg, ok := cut(line, "忽略"); ok {
		return needArg("ignore", arg)
	}
	if arg, ok := cut(line, "ignore"); ok {
		return needArg("ignore", arg)
	}
	if arg, ok := cut(line, "更新"); ok {
		return needArg("update", arg)
	}
	if arg, ok := cut(line, "update"); ok {
		return needArg("update", arg)
	}
	if arg, ok := cut(line, "upgrade"); ok {
		return needArg("update", arg)
	}
	return Command{}, fmt.Errorf("不认识的指令。发「帮助」看用法")
}

func needArg(action, arg string) (Command, error) {
	if arg == "" {
		return Command{}, fmt.Errorf("还要写容器名")
	}
	if action == "update" && (arg == "全部" || strings.EqualFold(arg, "all")) {
		return Command{Action: "updateall"}, nil
	}
	return Command{Action: action, Arg: arg}, nil
}

func normalize(input string) string {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return ""
	}
	if strings.HasPrefix(fields[0], "@") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ""
	}
	fields[0] = strings.TrimPrefix(fields[0], "/")
	return strings.Join(fields, " ")
}

func cut(line, prefix string) (string, bool) {
	if len(line) < len(prefix) {
		return "", false
	}
	head := line[:len(prefix)]
	if !strings.EqualFold(head, prefix) {
		return "", false
	}
	rest := line[len(prefix):]
	if rest == "" {
		return "", true
	}
	if !strings.HasPrefix(rest, " ") {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
