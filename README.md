# wecom-dock

监控本机 Docker 容器的镜像。发现新版本后，通过企业微信智能机器人推一条消息；在会话里回复指令，或点卡片上的按钮，再拉取镜像并按原来的配置重建。健康检查失败会把旧容器拉回来。

用的是企业微信智能机器人的长连接，不需要公网回调地址。

镜像是 linux/amd64，用于 x86_64 的机器。

镜像主要用 Docker Hub：`plutosdocker/wecom-dock:latest`

备用用 GitHub 容器仓库：`ghcr.io/meteor-showers/wecom-dock:latest`。Docker Hub 拉不到时，把下面 compose 里的 `image` 换成这个地址。

## 准备企业微信

1. 打开企业微信管理后台，进入应用管理，创建一个智能机器人。
2. API 模式选择「长连接」，不要选「接收消息 URL」。
3. 复制 BotID 和 Secret。Secret 是长连接专用的，和回调模式的 Token 不是同一个。
4. 把机器人拉进群，或自己打开它的单聊，先发一句「帮助」。没发过消息的会话，机器人不能主动推送。
5. 发「我的id」，把回显的 userid 填进 `WECOM_ADMINS`。不填的话，任何能给这个机器人发消息的人都能更新容器。

同一个机器人同时只能有一条长连接。不要起两个 wecom-dock。

## 运行

```bash
cp .env.example .env
# 填 WECOM_BOT_ID、WECOM_SECRET、WECOM_ADMINS

docker compose pull
docker compose up -d
docker compose logs -f
```

本机直接跑时，程序会读当前目录的 `.env`。已经 export 的环境变量优先。

```bash
go run ./cmd/wecom-dock
```

默认每 30 分钟查一次。改 `CHECK_INTERVAL`，例如 `1h`。状态文件在 `data/state.json`。

国内服务器访问 Docker Hub 时，在 `.env` 里加上这台机器能访问到的代理，然后 `docker compose up -d`：

```bash
HTTP_PROXY=http://192.168.1.10:7897
HTTPS_PROXY=http://192.168.1.10:7897
NO_PROXY=localhost,127.0.0.1
```

这个代理只给本程序查版本用。`docker pull` 走宿主机的 Docker 引擎，还要在 Docker 上配镜像加速或代理。容器里的 `127.0.0.1` 指向容器自己，不要填本机回环地址，除非用了 `network_mode: host`。

## 指令

| 发送 | 作用 |
|---|---|
| 帮助 | 看指令 |
| 状态 | 列出容器，以及有没有新版本 |
| 检查 | 马上再查一次，并给可更新的容器发卡片 |
| 更新 nginx | 拉取并重建这个容器 |
| 更新全部 | 只更新这次查出新版本的容器 |
| 忽略 nginx | 这个远端版本不再提醒，再变一次还会提醒 |
| 取消忽略 nginx | 恢复提醒 |
| 我的id | 查看 userid |

群里要 @ 机器人，消息前面的 @ 会被去掉。容器名可以写前缀，但必须只能对上一个。

卡片有两个按钮：「立即更新」和「忽略本版本」。点下去会先把卡片改成处理中，拉镜像可能要几分钟，结果另外发一条消息。同一张卡片只能处理一次。

## 更新时保留什么

重建用的是容器原来的配置：镜像标签、环境变量、挂载、端口、网络、重启策略。名字不变。

顺序是先拉取，成功后再停旧容器。新容器没起来，或者健康检查失败，会删掉新的、把旧的改回原名并重新启动。没有健康检查时，新容器连续运行超过 3 秒就视为成功。

下面两种不会自动跟，手动「更新」也会拒绝：

- 镜像引用里带了 `@sha256:`，标签不会再动
- 容器有标签 `wecomdock.skip=true`

本地 `docker build` 出来、没有仓库摘要的镜像不会自动提醒。可以对它发「更新 容器名」，那会交给 Docker 引擎按标签去拉。

只想盯一部分容器时，设置 `ONLY_LABELED=true`，并给要盯的容器加 `wecomdock.enable=true`。

## 私有仓库

查版本是这个进程自己访问仓库，更新是 Docker 引擎去拉，两边都要有登录信息。

- 在 `config.yaml` 的 `registries` 里写 `host`、`username`、`password`
- 或者把含有 `auths` 的 `config.json` 所在目录设为 `DOCKER_CONFIG`

macOS 上 Docker Desktop 的 `credsStore` 钥匙串，容器里读不到。这种登录要改成配置文件里的账号密码。

## 配置

环境变量覆盖 `config.yaml`。

| 变量 | 默认 | 含义 |
|---|---|---|
| `WECOM_BOT_ID` | | 智能机器人 BotID |
| `WECOM_SECRET` | | 长连接 Secret |
| `WECOM_ADMINS` | 空 | userid，逗号分隔。空表示不限制 |
| `WECOM_WS_URL` | `wss://openws.work.weixin.qq.com` | 一般不用改 |
| `CHECK_INTERVAL` | `30m` | 检查间隔，最短 1 分钟 |
| `HEALTH_TIMEOUT` | `60s` | 等多久健康检查 |
| `PULL_TIMEOUT` | `10m` | 单次拉取超时 |
| `INCLUDE_STOPPED` | `false` | 是否也检查已停止的容器 |
| `ONLY_LABELED` | `false` | 只检查 `wecomdock.enable=true` |
| `DATA_DIR` | `data` | 状态目录 |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker 引擎 |
| `DOCKER_CONFIG` | | 含 `config.json` 的目录 |
| `CONFIG` | `config.yaml`（文件存在才读） | 配置文件路径 |

示例见 `config.example.yaml`。

## 安全

这个程序挂载了 `/var/run/docker.sock`，等于拿着宿主机 Docker 的控制权。只跑在你自己的机器上，把 `WECOM_ADMINS` 设上，Secret 放在 `.env` 里，不要提交到 git。

wecom-dock 自己的 compose 加了 `wecomdock.skip=true`，避免它把自己重建掉。升级它用：

```bash
docker compose pull
docker compose up -d
```

## 开发

```bash
go test ./...
```
