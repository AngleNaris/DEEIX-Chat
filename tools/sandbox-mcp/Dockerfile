# deeix-sandbox-mcp —— DEEIX 多用户沙箱 MCP 服务镜像
# 需要挂载宿主 /var/run/docker.sock（唯一持该权限的服务）。
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sandbox-mcp .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /out/sandbox-mcp /usr/local/bin/sandbox-mcp
# 非 root 运行（仅需 Docker socket 读写）
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/sandbox-mcp"]
