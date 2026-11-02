FROM golang:1.27-alpine AS build

# 本机默认走国内镜像。GitHub Actions 在海外，构建时传入 USE_CN_MIRROR=0。
ARG USE_CN_MIRROR=1
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn

RUN if [ "$USE_CN_MIRROR" = "1" ]; then \
      sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories; \
    fi

WORKDIR /src
COPY go.mod go.sum ./
ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB}
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/wecom-dock ./cmd/wecom-dock

FROM alpine:3.22
ARG USE_CN_MIRROR=1
RUN if [ "$USE_CN_MIRROR" = "1" ]; then \
      sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories; \
    fi \
    && apk add --no-cache ca-certificates tzdata
COPY --from=build /out/wecom-dock /usr/local/bin/wecom-dock
# 需要读 docker.sock，默认以 root 运行。
ENTRYPOINT ["wecom-dock"]
