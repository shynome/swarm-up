# swarm-up 设计文档

一个帮助维护 Docker Swarm service 镜像版本的 TUI 工具。

## 1. 解决的问题

现有手工流程：

1. `docker pull image:v0.0.x`
2. `docker service update --image image:v0.0.x@sha256:xxx service_name`
3. 在 `xx.yaml` 中记录 `service_name: { image: image:v0.0.x@sha256:xxx }`

痛点：要来回切终端、手动拼 digest、容易漏记 yaml。

工具目标：**搜索 service → 输入新 tag → 自动 pull / 解析 digest / update service / 回写 yaml**。

## 2. 使用方式

```bash
swarm-up [file]
swarm-up --dry-run [file]   # 只打印将执行的命令，不实际改动
```

- `[file]`：镜像记录文件（yaml）。存在则读取用于校验与回写。
- 无 `file` 参数：TUI 顶部显示警告「未指定记录文件，更新结果不会写入 yaml」，操作仍可执行。
- `file` 指定但不存在：警告「文件不存在，成功后自动创建」，操作仍可执行。

## 3. 交互流程

```
┌─────────────────────────────────────────────────────────────┐
│ swarm-up                        记录文件: ./images.yaml ✓  │
│ 筛选: web_                                                   │
├─────────────────────────────────────────────────────────────┤
│ > web_api    ghcr.io/acme/api:v0.0.2@sha256:aa11…  ✓ 同步   │
│   web_admin  ghcr.io/acme/admin:v0.0.2@sha256:bb22… ⚠ 不一致│
│   web_cron   ghcr.io/acme/cron:v0.0.1@sha256:cc33… ⊘ 无记录 │
├─────────────────────────────────────────────────────────────┤
│ 状态图标: ✓ 与实际一致  ⚠ yaml 记录与实际不一致  ⊘ yaml 无记录│
│ / 筛选  ↑↓ 选择  enter 编辑  r 刷新  q 退出                 │
└─────────────────────────────────────────────────────────────┘
```

1. **列表页**：启动时并行拉取 `docker service ls` + `docker service inspect`，显示服务名与实际镜像。输入即筛选（匹配服务名或镜像，忽略大小写）。
2. **编辑页**：显示服务名 / 当前镜像 / 新 tag 输入框。输入 `v0.0.3` 这类 tag，`enter` 进入确认。
3. **确认页**：列出将要执行的完整命令（pull / service update / yaml 写入），`enter` 执行，`esc` 返回。
4. **执行页**：显示 pull 阶段 spinner 与输出；成功后执行 update；最后写 yaml。失败则原地显示错误与实际命令，`esc` 返回列表。
5. 执行成功回到列表并刷新该服务行。

按键：`↑/↓` 或 `j/k` 移动，`/` 聚焦筛选，`enter` 确认，`esc` 返回上一页，`r` 刷新列表，`q` / `ctrl+c` 退出。

## 4. 核心逻辑

### 4.1 解析当前镜像

`docker service inspect --format '{{.Spec.Name}}\t{{.Spec.TaskTemplate.ContainerSpec.Image}}' <ids...>`

得到形如 `repo:tag@sha256:digest` 的引用，拆分为：

```
repo  = 去掉 tag 与 digest 的部分（正确处理 registry:port/repo 与多级路径）
tag   = 原 tag（可能为 latest 缺省）
digest= 原 digest
```

### 4.2 生成新镜像引用

用户只输入新 tag。新引用 = `repo:新tag`（保留原 digest 待 pull 后替换）。

pull 后解析 digest：

1. 首选 `docker pull repo:tag` 输出中的 `Digest: sha256:...`；
2. 回退 `docker image inspect repo:tag --format '{{json .RepoDigests}}'`，取以 `repo@` 开头的条目；
3. 都拿不到 → 报错中止（不 update、不写 yaml）。

> 本地已有该 tag 时 `docker pull` 会输出 `Image is up to date`，此时走回退方案 2。

### 4.3 更新 service

```
docker service update --image repo:tag@sha256:digest --quiet <service>
```

- 不使用 `--with-registry-auth`（v1 假定全部公开镜像）。
- 失败时展示 stderr，并提示可执行 `docker service update --rollback <service>`。

### 4.4 记录文件读写

格式（服务名 → 镜像）：

```yaml
web_api:
  image: ghcr.io/acme/api:v0.0.2@sha256:aa11...
web_admin:
  image: ghcr.io/acme/admin:v0.0.2@sha256:bb22...
```

- 解析使用 `yaml.v3` 的 `yaml.Node`，**只精确修改目标 service 的 `image` 字段**，保留键顺序、注释与其他字段。
- 目标 service 不存在时新增条目（追加到文件末尾）。
- `image` 值一律写入完整 `repo:tag@sha256:digest`。
- 写入前先写临时文件再 `rename`，避免中断损坏文件。
- 文件权限沿用原文件；新建时 `0644`。

### 4.5 一致性校验

列表加载后，对每个 service：

- yaml 有记录且 `记录 == 实际镜像` → `✓`
- yaml 有记录但 `记录 != 实际镜像` → `⚠`（悬浮/详情显示两者差异）
- yaml 无记录 → `⊘`

无记录/不一致**不阻止**更新，仅在界面与退出时给出警告。

## 5. 技术选型

| 模块 | 选择 |
| --- | --- |
| 语言 | Go 1.27 |
| TUI | `github.com/charmbracelet/bubbletea` + `bubbles`（list / textinput / spinner）+ `lipgloss` |
| YAML | `gopkg.in/yaml.v3`（Node 级编辑） |
| Docker 交互 | **调用 docker CLI**（不引 Docker SDK）：尊重 `DOCKER_HOST` / context 配置，体积小、行为与手工流程完全一致 |

模块名：`github.com/shynome/swarm-up`

## 6. 目录结构

```
swarm-up/
├── main.go                  # 参数解析、启动 TUI
├── design.md
├── internal/
│   ├── docker/              # 镜像引用解析 + docker CLI 封装
│   │   ├── ref.go           # repo/tag/digest 拆分与拼装
│   │   ├── ref_test.go
│   │   ├── client.go        # ListServices / Pull / InspectDigest / ServiceUpdate
│   │   └── client_test.go   # 用假 docker 脚本测试
│   ├── store/
│   │   ├── store.go         # yaml Node 读写
│   │   └── store_test.go
│   └── tui/                 # Bubble Tea 界面（列表 / 编辑 / 确认 / 执行）
└── Makefile                 # build / test / install
```

## 7. 非目标（v1 不做）

- 从 registry 拉取 tag 列表（手动输入 tag）
- 多 service 批量更新
- 私有 registry 认证与 `--with-registry-auth`
- 自动回滚、更新历史、并发多平台镜像处理
- 脱离 docker CLI 直接访问 Docker API

## 8. 验收标准

1. `swarm-up` 在无 yaml 文件时仍能正常列出、更新服务，并给出「不会记录」警告。
2. `swarm-up images.yaml` 能显示每个服务 `✓ / ⚠ / ⊘` 状态。
3. 更新成功后：service 实际镜像为 `repo:新tag@sha256:digest`，yaml 对应项同步为同一字符串。
4. yaml 中其他条目、注释、键顺序保持不变。
5. pull 失败或 update 失败时，yaml 不被修改，界面显示错误与失败命令。
6. `go test ./...` 覆盖镜像引用解析、yaml 精确修改、一致性判定。
