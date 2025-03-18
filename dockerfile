FROM golang:1.19-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制源代码
COPY . .

# 初始化 Go 模块（如果需要）并下载依赖
RUN go mod tidy

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -o mdimg2hub .

# 使用轻量级的 alpine 镜像作为最终镜像
FROM alpine:3.17

# 安装 CA 证书，用于 HTTPS 请求
RUN apk --no-cache add ca-certificates

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/mdimg2hub .

# 复制模板文件
COPY --from=builder /app/templates ./templates

# 创建非 root 用户
RUN adduser -D -u 1000 appuser
RUN chown -R appuser:appuser /app
USER appuser

# 设置环境变量，提供默认值（注意：敏感信息应通过运行时参数传递，而不是构建时）
ENV PORT=8080 \
    GITHUB_OWNER="" \
    GITHUB_REPO="" \
    GITHUB_BRANCH="main" \
    IMAGES_PATH="images" \
    USE_CDN=true \
    GITHUB_TOKEN=""

# 暴露端口
EXPOSE ${PORT}

# 创建启动脚本
RUN echo '#!/bin/sh' > /app/start.sh && \
    echo './mdimg2hub -port=${PORT} -token=${GITHUB_TOKEN} -owner=${GITHUB_OWNER} -repo=${GITHUB_REPO} -branch=${GITHUB_BRANCH} -images-path=${IMAGES_PATH} -use-cdn=${USE_CDN}' >> /app/start.sh && \
    chmod +x /app/start.sh

# 使用启动脚本
ENTRYPOINT ["/app/start.sh"]
