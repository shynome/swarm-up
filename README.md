# swarm-up

一个用于维护 Docker Swarm service 镜像版本的 TUI 工具。

选中 service、修改 tag，swarm-up 会依次执行 `docker pull` → `docker service update`，并把实际使用的镜像（含 digest）写回 `docker-compose.yaml`。

```
选中 service → 修改 tag → 确认 pull → 确认 update → 确认写入 compose
```

## 功能

- 列出并搜索 swarm service，显示当前实际镜像（`repo:tag@sha256:...`）
- 与 compose 文件比对，标记 `✓ 同步` / `⚠ 不一致` / `⊘ 未记录`
- 三步确认：pull 镜像 → 执行 update → 写入 compose，每一步都可确认或取消
- 自动解析 digest，写入完整镜像引用
- 只修改 `services.<name>.image`，保留空行、注释、缩进与其他字段
- 支持 stack 前缀匹配（`vtb_songs` → `songs`）与镜像仓库唯一匹配
- `--dry-run` 只展示将执行的命令，不做任何改动
- 静态编译（CGO 关闭），支持 linux amd64 / arm64

## 安装

### 下载 Release

从 [Releases](https://github.com/shynome/swarm-up/releases) 下载对应架构的包：

```bash
tar -xzf swarm-up_v0.0.1_linux_amd64.tar.gz   # 或 linux_arm64
sudo install -m 0755 swarm-up /usr/local/bin/
```

### 从源码构建

```bash
make build
# 或
CGO_ENABLED=0 go build -trimpath -o swarm-up .
```

## 使用

```bash
swarm-up docker-compose.yaml
swarm-up --dry-run docker-compose.yaml
swarm-up --version
```

`file` 参数用于校验与回写版本记录；不传时仍可更新 service，但不会写入文件。

### 快捷键

| 页面 | 按键 | 说明 |
| --- | --- | --- |
| 列表 | `/` | 搜索服务名或镜像 |
| 列表 | `↑` / `↓`、`j` / `k` | 移动 |
| 列表 | `enter` | 编辑 tag（默认填入当前 tag） |
| 列表 | `r` | 刷新 |
| 列表 | `q` / `ctrl+c` | 退出 |
| 编辑 | `enter` | 下一步 |
| 确认 | `←` / `→`、`tab` | 切换「执行 / 取消」按钮 |
| 确认 | `enter` | 触发当前按钮 |
| 执行中 | `esc` | 取消 |

### 执行流程

1. 确认 `docker pull <repo>:<新 tag>`
2. pull 完成后展示带真实 digest 的 `docker service update` 命令，确认后执行
3. update 成功后展示要写入的 `services.<key>.image`，确认后写入（`esc` 可跳过）

任一步失败都会停留在结果页并显示日志，compose 文件不会被修改。

## compose 文件匹配

服务名按以下顺序匹配：

1. `services` 下同名 key
2. 唯一后缀：`vtb_songs` → `songs`（取最长后缀）
3. 镜像仓库唯一匹配（忽略 tag 与 digest；多个候选则不匹配）

写入规则：

- 只修改已有服务的 `image` 字段
- 文件不存在或 `services` 为空时，在 `services` 下新增条目
- 已有服务但匹配不到时报错，不写入

## 开发

```bash
make test    # go test ./...
make vet     # go vet ./...
make dist    # 生成 linux/amd64、linux/arm64 发布包到 dist/
```

## 发布

推送 `v*` tag 会触发 GitHub Actions 执行测试、构建两个架构的静态二进制并创建 Release：

```bash
git tag v0.0.1
git push origin v0.0.1
```
