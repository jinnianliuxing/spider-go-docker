# ---------- 构建阶段 ----------
FROM golang:1.25-alpine AS builder

# 国内网络可加速依赖下载，不需要的话删掉这行即可
ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOOS=linux

WORKDIR /build

# 先单独拷贝 go.mod/go.sum，利用 Docker 缓存层：
# 只要依赖没变，重新构建时就不用重新下载依赖
COPY go.mod go.sum ./
RUN go mod download

# 拷贝其余源码并编译成单个静态二进制
COPY . .
RUN go build -ldflags="-s -w" -o spider-go .

# ---------- 运行阶段 ----------
FROM alpine:3.20

# ca-certificates: 程序要请求教务系统等 https 接口，必须有
# tzdata: 保证容器内时间/时区是对的
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=builder /build/spider-go .
# 把 config 目录一起拷进去（里面要有 config.dev.yaml / config.production.yaml）
COPY config ./config

ENV GO_ENV=production
EXPOSE 8080

ENTRYPOINT ["./spider-go"]
