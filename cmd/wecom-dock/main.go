package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pluto/wecom-dock/internal/bot"
	"github.com/pluto/wecom-dock/internal/config"
	"github.com/pluto/wecom-dock/internal/dockerx"
	"github.com/pluto/wecom-dock/internal/monitor"
	"github.com/pluto/wecom-dock/internal/registry"
	"github.com/pluto/wecom-dock/internal/store"
	"github.com/pluto/wecom-dock/internal/wecom"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("配置无效", "err", err)
		os.Exit(1)
	}
	if len(cfg.Admins) == 0 {
		log.Warn("没有设置 WECOM_ADMINS，任何能给机器人发消息的人都能更新容器")
	}

	reg, err := registry.New(cfg.DockerConfig, cfg.Registries)
	if err != nil {
		log.Error("读取镜像仓库登录信息失败", "err", err)
		os.Exit(1)
	}
	if cs := reg.CredsStore(); cs != "" {
		log.Warn("Docker 配置使用了 credsStore，这个进程读不到里面的密钥。私有仓库请在配置里写账号密码")
	}

	dk, err := dockerx.New(cfg.DockerHost, cfg.PullTimeout)
	if err != nil {
		log.Error("连接 Docker 失败", "err", err)
		os.Exit(1)
	}
	defer dk.Close()

	st, err := store.Open(store.PathFor(cfg.DataDir))
	if err != nil {
		log.Error("打开数据目录失败", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := dk.Ping(ctx); err != nil {
		log.Warn("Docker 暂时不可用，指令会失败直到引擎恢复", "err", err)
	}

	svc := &monitor.Service{
		Docker:         dk,
		Reg:            reg,
		IncludeStopped: cfg.IncludeStopped,
		OnlyLabeled:    cfg.OnlyLabeled,
		HealthTimeout:  cfg.HealthTimeout,
	}
	wc := &wecom.Client{
		BotID:    cfg.BotID,
		Secret:   cfg.Secret,
		Endpoint: cfg.WSURL,
		Log:      log,
	}
	talk := &bot.Bot{
		Root:   ctx,
		Ctrl:   svc,
		Msg:    wc,
		Store:  st,
		Admins: cfg.Admins,
		Log:    log,
	}
	wc.Handler = talk

	log.Info("wecom-dock 已启动", "schedule", scheduleLabel(cfg), "admins", len(cfg.Admins))
	go wc.Run(ctx)
	go schedule(ctx, cfg, talk)

	<-ctx.Done()
	log.Info("正在退出")
}

func scheduleLabel(cfg config.Config) string {
	if cfg.DailyAt != nil {
		return "每天 " + cfg.DailyAt.String() + " " + time.Local.String()
	}
	return "每 " + cfg.Interval.String()
}

func schedule(ctx context.Context, cfg config.Config, talk *bot.Bot) {
	if cfg.DailyAt != nil {
		scheduleDaily(ctx, *cfg.DailyAt, talk)
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(8 * time.Second):
	}
	talk.Notify(ctx)
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			talk.Notify(ctx)
		}
	}
}

func scheduleDaily(ctx context.Context, at config.DailyAt, talk *bot.Bot) {
	for {
		next := nextDaily(time.Now(), at.Hour, at.Minute, time.Local)
		slog.Info("下次检查", "at", next.Format(time.RFC3339))
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			talk.Notify(ctx)
		}
	}
}

// nextDaily is the next occurrence of hour:minute in loc, strictly after now.
func nextDaily(now time.Time, hour, minute int, loc *time.Location) time.Time {
	local := now.In(loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
