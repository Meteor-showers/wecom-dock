package dockerx

import (
	"context"
	"fmt"
	"strings"
)

type snapshot struct {
	ID       string
	Name     string
	ImageRef string
	ImageID  string
}

type updater interface {
	inspect(ctx context.Context, id string) (snapshot, error)
	pull(ctx context.Context, ref string) error
	imageID(ctx context.Context, ref string) (string, error)
	rename(ctx context.Context, id, name string) error
	stop(ctx context.Context, id string) error
	createFrom(ctx context.Context, oldID, name string) (string, error)
	start(ctx context.Context, id string) error
	remove(ctx context.Context, id string) error
	waitReady(ctx context.Context, id string) error
}

func backupName(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		id = id[:12]
	}
	return "wecomdock-bak-" + id
}

func cleanName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "/")
}

// runUpdate pulls the image and recreates the container with the same config.
// The old container is stopped only after the pull succeeds, and started again if the new one fails.
func runUpdate(ctx context.Context, u updater, id, bakName string) (string, error) {
	snap, err := u.inspect(ctx, id)
	if err != nil {
		return "", fmt.Errorf("查看容器失败: %w", err)
	}
	original := cleanName(snap.Name)
	if original == "" {
		return "", fmt.Errorf("容器没有名字")
	}
	if snap.ImageRef == "" {
		return "", fmt.Errorf("容器没有镜像引用")
	}
	if err := u.pull(ctx, snap.ImageRef); err != nil {
		return "", fmt.Errorf("拉取镜像失败: %w", err)
	}
	newImage, err := u.imageID(ctx, snap.ImageRef)
	if err != nil {
		return "", fmt.Errorf("查看新镜像失败: %w", err)
	}
	if newImage == snap.ImageID {
		return "已经是最新镜像", nil
	}

	if err := u.rename(ctx, snap.ID, bakName); err != nil {
		return "", fmt.Errorf("给旧容器改名失败: %w", err)
	}
	if err := u.stop(ctx, snap.ID); err != nil {
		_ = restore(ctx, u, snap.ID, original)
		return "", fmt.Errorf("停止旧容器失败: %w", err)
	}
	newID, err := u.createFrom(ctx, snap.ID, original)
	if err != nil {
		_ = restore(ctx, u, snap.ID, original)
		return "", fmt.Errorf("创建新容器失败: %w", err)
	}
	if err := u.start(ctx, newID); err != nil {
		_ = u.remove(ctx, newID)
		_ = restore(ctx, u, snap.ID, original)
		return "", fmt.Errorf("启动新容器失败: %w", err)
	}
	if err := u.waitReady(ctx, newID); err != nil {
		_ = u.stop(ctx, newID)
		_ = u.remove(ctx, newID)
		if rerr := restore(ctx, u, snap.ID, original); rerr != nil {
			return "", fmt.Errorf("新容器未就绪（%v），回滚也失败: %w", err, rerr)
		}
		return "", fmt.Errorf("新容器未就绪，已回滚: %w", err)
	}
	if err := u.remove(ctx, snap.ID); err != nil {
		return "已更新，但旧容器 " + bakName + " 没删掉：" + err.Error(), nil
	}
	return "已更新", nil
}

func restore(ctx context.Context, u updater, bakID, original string) error {
	if err := u.rename(ctx, bakID, original); err != nil {
		return err
	}
	return u.start(ctx, bakID)
}
