# MDImg2Hub - Markdown 图片上传工具

## 功能特点

- 自动上传 Markdown 文件中引用的本地图片到 GitHub 仓库
- 自动替换 Markdown 文件中的本地图片链接为 GitHub 链接
- 支持使用 jsDelivr CDN 加速图片访问
- 简单易用的 Web 界面
- 支持 Docker 部署

## 使用方法

### 准备工作

1. 创建一个 GitHub 仓库作为图床
2. 生成 GitHub 个人访问令牌 (Personal Access Token)，需要有 `repo` 权限

### 使用 Docker 部署

```bash
docker run -d \
  --name mdimg2hub \
  -p 8080:8080 \
  -e GITHUB_OWNER=your-github-username \
  -e GITHUB_REPO=your-image-repo \
  -e GITHUB_TOKEN=your-github-token \
  -e USE_CDN=true \
  huihuidehui/mdimg2hub:latest
```

### 环境变量说明

| 环境变量 | 必填 | 默认值 | 说明 |
|---------|------|-------|------|
| GITHUB_OWNER | 是 | - | GitHub 仓库所有者用户名 |
| GITHUB_REPO | 是 | - | GitHub 仓库名称 |
| GITHUB_TOKEN | 是 | - | GitHub 个人访问令牌 |
| GITHUB_BRANCH | 否 | main | GitHub 仓库分支 |
| IMAGES_PATH | 否 | images | 图片在仓库中的存储路径 |
| USE_CDN | 否 | true | 是否使用 jsDelivr CDN 加速图片访问 |
| PORT | 否 | 8080 | Web 服务器监听端口 |

### 使用示例

1. 访问 `http://your-server:8080`
2. 将您的 Markdown 文件和其引用的图片打包成 ZIP 压缩文件
3. 上传 ZIP 文件
4. 等待处理完成后，下载处理后的 Markdown 文件

## 注意事项

- 每个 ZIP 压缩包中只能包含一个 Markdown 文件和其引用的图片
- 已经是网络图片链接的引用不会被修改
- GitHub Token 包含敏感信息，请妥善保管

## 本地开发

### 前提条件

- Go 1.19 或更高版本
- Git

### 构建步骤

```bash
# 克隆仓库
git clone https://github.com/lalaking666/mdimg2hub.git
cd mdimg2hub

# 安装依赖
go mod tidy

# 构建
go build -o mdimg2hub .

# 运行
./mdimg2hub -token=your-github-token -owner=your-github-username -repo=your-image-repo
```

## 构建 Docker 镜像

```bash
docker build -t mdimg2hub .
```

## 贡献

欢迎提交 Pull Request 或 Issue 来帮助改进这个项目！

