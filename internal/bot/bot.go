package bot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pluto/wecom-dock/internal/monitor"
	"github.com/pluto/wecom-dock/internal/store"
	"github.com/pluto/wecom-dock/internal/wecom"
)

const helpText = `**Docker 更新**

发现镜像新版本后，我会推到这个会话。先发一条消息，之后才能收到主动提醒。

- 状态：查看容器
- 检查：立刻再查一次
- 更新 容器名：拉取并按原配置重建
- 更新全部：只更新已经发现新版本的容器
- 忽略 容器名：这个版本不再提醒
- 取消忽略 容器名
- 我的id：查看 userid，填到管理员名单

重建会保留挂载、端口、环境变量和网络。有健康检查且失败时会自动回滚。带 wecomdock.skip=true 的容器不会动。`

// Controller checks images and recreates containers.
type Controller interface {
	Reports(ctx context.Context) ([]monitor.Report, error)
	UpgradeNamed(ctx context.Context, name string) (monitor.Result, error)
}

// Messenger sends WeCom replies and pushes.
type Messenger interface {
	ReplyMarkdown(ctx context.Context, reqID, content string) error
	ReplyWelcome(ctx context.Context, reqID, content string) error
	UpdateCard(ctx context.Context, reqID string, card wecom.Card) error
	SendMarkdown(ctx context.Context, chatID string, chatType int, content string) error
	SendCard(ctx context.Context, chatID string, chatType int, card wecom.Card) error
}

// Bot turns chat messages into container actions.
type Bot struct {
	Root   context.Context
	Ctrl   Controller
	Msg    Messenger
	Store  *store.Store
	Admins []string
	Log    *slog.Logger
}

// OnMessage handles a user text message.
func (b *Bot) OnMessage(ctx context.Context, in wecom.Incoming) {
	if !b.allowed(in.UserID) {
		b.reply(ctx, in, "没有操作权限。\n你的 userid 是 `"+in.UserID+"`")
		return
	}
	b.remember(in)
	cmd, err := Parse(in.Text)
	if err != nil {
		b.reply(ctx, in, err.Error())
		return
	}
	switch cmd.Action {
	case "help":
		b.reply(ctx, in, helpText)
	case "whoami":
		b.reply(ctx, in, "你的 userid 是 `"+in.UserID+"`")
	case "status":
		b.replyReports(ctx, in, false)
	case "check":
		b.replyReports(ctx, in, true)
	case "update":
		b.updateOne(ctx, in, cmd.Arg)
	case "updateall":
		b.updateAll(ctx, in)
	case "ignore":
		b.ignore(ctx, in, cmd.Arg, true)
	case "unignore":
		b.ignore(ctx, in, cmd.Arg, false)
	default:
		b.reply(ctx, in, "不认识的指令。发「帮助」看用法")
	}
}

// OnEvent handles enter-chat and template-card clicks.
func (b *Bot) OnEvent(ctx context.Context, in wecom.Incoming) {
	switch in.Event {
	case "enter_chat":
		if !b.allowed(in.UserID) {
			_ = b.Msg.ReplyWelcome(ctx, in.ReqID, "没有操作权限。你的 userid 是 "+in.UserID)
			return
		}
		b.remember(in)
		_ = b.Msg.ReplyWelcome(ctx, in.ReqID, "我是 Docker 更新助手。发「帮助」看指令。")
	case "template_card_event":
		if !b.allowed(in.UserID) {
			b.reply(ctx, in, "没有操作权限。\n你的 userid 是 `"+in.UserID+"`")
			return
		}
		b.remember(in)
		b.onCard(ctx, in)
	}
}

// Notify pushes newly discovered updates to every known chat.
func (b *Bot) Notify(ctx context.Context) {
	reports, err := b.Ctrl.Reports(ctx)
	if err != nil {
		b.log().Warn("检查容器失败", "err", err)
		return
	}
	fresh := b.fresh(reports)
	if len(fresh) == 0 {
		return
	}
	sessions := b.Store.Sessions()
	if len(sessions) == 0 {
		b.log().Warn("有镜像更新，但还没有企微会话。先给机器人发一条「帮助」")
		return
	}
	text := formatUpdates(fresh)
	delivered := false
	for _, sess := range sessions {
		if err := b.Msg.SendMarkdown(ctx, sess.ChatID, sess.ChatType, text); err != nil {
			b.log().Warn("推送更新失败", "chat", sess.ChatID, "err", err)
			continue
		}
		delivered = true
		if len(fresh) <= 5 {
			b.sendCards(ctx, sess.ChatID, sess.ChatType, fresh)
		}
	}
	if !delivered {
		return
	}
	for _, r := range fresh {
		_ = b.Store.MarkNotified(r.Name, r.RemoteDigest)
	}
}

func (b *Bot) onCard(ctx context.Context, in wecom.Incoming) {
	task, ok := b.Store.TakeTask(in.TaskID)
	if !ok {
		b.reply(ctx, in, "这条通知已经处理过了。发「检查」可以再看一遍。")
		return
	}
	switch in.EventKey {
	case "ignore":
		_ = b.Store.Ignore(task.Name, task.RemoteDigest)
		_ = b.Msg.UpdateCard(ctx, in.ReqID, busyCard(task, "已忽略此版本", "已忽略"))
		_ = b.Msg.SendMarkdown(ctx, in.ChatID, in.ChatType, fmt.Sprintf("已忽略 `%s` 的这个版本。远端再变一次还会提醒。", task.Name))
	case "update":
		_ = b.Msg.UpdateCard(ctx, in.ReqID, busyCard(task, "正在更新", "更新中"))
		go b.finishUpgrade(in, task.Name)
	default:
		_ = b.Msg.SendMarkdown(ctx, in.ChatID, in.ChatType, "收到。")
	}
}

func (b *Bot) updateOne(ctx context.Context, in wecom.Incoming, name string) {
	reports, err := b.load(ctx)
	if err != nil {
		b.reply(ctx, in, "检查失败："+err.Error())
		return
	}
	r, err := matchReport(reports, name)
	if err != nil {
		b.reply(ctx, in, err.Error())
		return
	}
	if msg, blocked := blocked(r); blocked {
		b.reply(ctx, in, msg)
		return
	}
	b.reply(ctx, in, "开始更新 `"+r.Name+"`")
	go b.finishUpgrade(in, r.Name)
}

func (b *Bot) updateAll(ctx context.Context, in wecom.Incoming) {
	reports, err := b.load(ctx)
	if err != nil {
		b.reply(ctx, in, "检查失败："+err.Error())
		return
	}
	var names []string
	for _, r := range reports {
		if r.Kind == monitor.KindOutdated {
			names = append(names, r.Name)
		}
	}
	if len(names) == 0 {
		b.reply(ctx, in, "没有可更新的容器。")
		return
	}
	b.reply(ctx, in, fmt.Sprintf("开始更新 %d 个容器。", len(names)))
	go func() {
		var buf strings.Builder
		buf.WriteString("**更新结果**\n")
		for _, name := range names {
			res, err := b.Ctrl.UpgradeNamed(b.root(), name)
			fmt.Fprintf(&buf, "- %s\n", resultText(name, res, err))
		}
		_ = b.Msg.SendMarkdown(b.root(), in.ChatID, in.ChatType, buf.String())
	}()
}

func (b *Bot) ignore(ctx context.Context, in wecom.Incoming, name string, ignore bool) {
	reports, err := b.load(ctx)
	if err != nil {
		b.reply(ctx, in, "检查失败："+err.Error())
		return
	}
	r, err := matchReport(reports, name)
	if err != nil {
		b.reply(ctx, in, err.Error())
		return
	}
	if !ignore {
		if err := b.Store.ClearIgnore(r.Name); err != nil {
			b.reply(ctx, in, "取消忽略失败："+err.Error())
			return
		}
		b.reply(ctx, in, "已恢复 `"+r.Name+"` 的更新提醒。")
		return
	}
	if r.Kind != monitor.KindOutdated || r.RemoteDigest == "" {
		b.reply(ctx, in, "`"+r.Name+"` 现在没有可忽略的新版本。")
		return
	}
	if err := b.Store.Ignore(r.Name, r.RemoteDigest); err != nil {
		b.reply(ctx, in, "忽略失败："+err.Error())
		return
	}
	b.reply(ctx, in, "已忽略 `"+r.Name+"` 的这个版本。远端再变一次还会提醒。")
}

func (b *Bot) finishUpgrade(in wecom.Incoming, name string) {
	res, err := b.Ctrl.UpgradeNamed(b.root(), name)
	_ = b.Msg.SendMarkdown(b.root(), in.ChatID, in.ChatType, resultText(name, res, err))
}

func (b *Bot) replyReports(ctx context.Context, in wecom.Incoming, withCards bool) {
	reports, err := b.load(ctx)
	if err != nil {
		b.reply(ctx, in, "检查失败："+err.Error())
		return
	}
	b.reply(ctx, in, formatReports(reports))
	if !withCards {
		return
	}
	var fresh []monitor.Report
	for _, r := range reports {
		if r.Kind == monitor.KindOutdated {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) > 0 && len(fresh) <= 5 {
		b.sendCards(ctx, in.ChatID, in.ChatType, fresh)
	}
	for _, r := range fresh {
		_ = b.Store.MarkNotified(r.Name, r.RemoteDigest)
	}
}

func (b *Bot) sendCards(ctx context.Context, chatID string, chatType int, reports []monitor.Report) {
	for _, r := range reports {
		id := newTaskID()
		task := store.Task{
			ID:           id,
			Name:         r.Name,
			ContainerID:  r.ID,
			Image:        r.Image,
			RemoteDigest: r.RemoteDigest,
		}
		if err := b.Store.PutTask(task); err != nil {
			b.log().Warn("保存卡片失败", "err", err)
			continue
		}
		card := wecom.Card{
			TaskID: id,
			Title:  "镜像有新版本",
			Desc:   r.Name,
			Rows: [][2]string{
				{"镜像", r.Image},
				{"当前", monitor.ShortDigest(r.LocalDigest)},
				{"可用", monitor.ShortDigest(r.RemoteDigest)},
			},
			Buttons: []wecom.Button{
				{Text: "立即更新", Key: "update", Style: 1},
				{Text: "忽略本版本", Key: "ignore", Style: 2},
			},
		}
		if err := b.Msg.SendCard(ctx, chatID, chatType, card); err != nil {
			_, _ = b.Store.TakeTask(id)
			b.log().Warn("发送卡片失败", "err", err)
		}
	}
}

func (b *Bot) load(ctx context.Context) ([]monitor.Report, error) {
	reports, err := b.Ctrl.Reports(ctx)
	if err != nil {
		return nil, err
	}
	return applyIgnore(reports, b.Store), nil
}

func (b *Bot) fresh(reports []monitor.Report) []monitor.Report {
	reports = applyIgnore(reports, b.Store)
	var fresh []monitor.Report
	for _, r := range reports {
		if r.Kind == monitor.KindOutdated && b.Store.Notified(r.Name) != r.RemoteDigest {
			fresh = append(fresh, r)
		}
	}
	return fresh
}

func (b *Bot) remember(in wecom.Incoming) {
	if in.ChatID == "" {
		return
	}
	_ = b.Store.Remember(store.Session{ChatID: in.ChatID, ChatType: in.ChatType, UserID: in.UserID})
}

func (b *Bot) allowed(user string) bool {
	if len(b.Admins) == 0 {
		return true
	}
	for _, admin := range b.Admins {
		if admin == user {
			return true
		}
	}
	return false
}

func (b *Bot) reply(ctx context.Context, in wecom.Incoming, content string) {
	if in.ReqID != "" {
		if err := b.Msg.ReplyMarkdown(ctx, in.ReqID, content); err != nil {
			b.log().Warn("回复失败", "err", err, "req", in.ReqID)
		} else {
			return
		}
	}
	if in.ChatID == "" {
		b.log().Warn("没有会话，回复发不出去", "req", in.ReqID)
		return
	}
	if err := b.Msg.SendMarkdown(ctx, in.ChatID, in.ChatType, content); err != nil {
		b.log().Warn("主动回复失败", "err", err, "chat", in.ChatID)
	}
}

func (b *Bot) root() context.Context {
	if b.Root != nil {
		return b.Root
	}
	return context.Background()
}

func (b *Bot) log() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

func applyIgnore(reports []monitor.Report, st *store.Store) []monitor.Report {
	out := make([]monitor.Report, len(reports))
	copy(out, reports)
	for i, r := range out {
		if r.Kind == monitor.KindOutdated && r.RemoteDigest != "" && st.Ignored(r.Name) == r.RemoteDigest {
			out[i].Kind = monitor.KindIgnored
		}
	}
	return out
}

func blocked(r monitor.Report) (string, bool) {
	if strings.Contains(r.Detail, "wecomdock.skip") {
		return "`" + r.Name + "` 标记了 wecomdock.skip，不会通过机器人更新。", true
	}
	if r.Kind == monitor.KindPinned {
		return "`" + r.Name + "` 的镜像钉死了 digest，没有可跟随的标签。", true
	}
	return "", false
}

func resultText(name string, res monitor.Result, err error) string {
	shown := name
	if res.Name != "" {
		shown = res.Name
	}
	if err != nil {
		msg := err.Error()
		if res.Message != "" {
			msg = res.Message
		}
		return fmt.Sprintf("`%s` 更新失败：%s", shown, msg)
	}
	if res.Message == "" {
		res.Message = "完成"
	}
	return fmt.Sprintf("`%s` %s", shown, res.Message)
}

func formatReports(reports []monitor.Report) string {
	if len(reports) == 0 {
		return "没有看到容器。"
	}
	var buf strings.Builder
	buf.WriteString("**容器**\n")
	for _, r := range reports {
		fmt.Fprintf(&buf, "- `%s` `%s` — %s\n", r.Name, r.Image, describe(r))
	}
	return buf.String()
}

func formatUpdates(reports []monitor.Report) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "**%d 个容器可以更新**\n", len(reports))
	for _, r := range reports {
		fmt.Fprintf(&buf, "- `%s` `%s`\n  %s → %s\n", r.Name, r.Image, monitor.ShortDigest(r.LocalDigest), monitor.ShortDigest(r.RemoteDigest))
	}
	if len(reports) > 5 {
		buf.WriteString("\n数量有点多，没有逐个发卡片。回复 `更新 容器名` 或 `更新全部`。")
	} else {
		buf.WriteString("\n可以点卡片，或回复 `更新 容器名`。")
	}
	return buf.String()
}

func describe(r monitor.Report) string {
	switch r.Kind {
	case monitor.KindCurrent:
		return "已是最新"
	case monitor.KindOutdated:
		return fmt.Sprintf("可更新 %s → %s", monitor.ShortDigest(r.LocalDigest), monitor.ShortDigest(r.RemoteDigest))
	case monitor.KindIgnored:
		return "已忽略此版本"
	case monitor.KindPinned:
		return "已钉死 digest"
	case monitor.KindSkipped:
		return "跳过：" + r.Detail
	case monitor.KindError:
		return "检查失败：" + r.Detail
	default:
		if r.Detail != "" {
			return r.Detail
		}
		return r.State
	}
}

func matchReport(list []monitor.Report, query string) (monitor.Report, error) {
	q := strings.TrimPrefix(strings.TrimSpace(query), "/")
	if q == "" {
		return monitor.Report{}, fmt.Errorf("要写容器名")
	}
	var exact, prefix []monitor.Report
	for _, r := range list {
		if strings.EqualFold(r.Name, q) {
			exact = append(exact, r)
			continue
		}
		if strings.HasPrefix(strings.ToLower(r.Name), strings.ToLower(q)) {
			prefix = append(prefix, r)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) > 1:
		return monitor.Report{}, fmt.Errorf("有多个同名容器")
	case len(prefix) == 1:
		return prefix[0], nil
	case len(prefix) > 1:
		return monitor.Report{}, fmt.Errorf("匹配到多个容器")
	default:
		return monitor.Report{}, fmt.Errorf("没有找到容器 %s", q)
	}
}

func busyCard(task store.Task, title, button string) wecom.Card {
	return wecom.Card{
		TaskID: task.ID,
		Title:  title,
		Desc:   task.Name,
		Buttons: []wecom.Button{{
			Text:  button,
			Key:   "busy",
			Style: 2,
		}},
	}
}

func newTaskID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "t" + hex.EncodeToString(b[:])
}
