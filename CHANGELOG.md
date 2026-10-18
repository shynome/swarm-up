# Changelog

本项目遵循 [Semantic Versioning](https://semver.org/) 与 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

## [Unreleased]

## [v0.0.1] - 2026-10-18

首个版本。

### Added

- TUI 列表：搜索 Docker Swarm service，显示实际镜像与 compose 记录状态（`✓` / `⚠` / `⊘`）
- 更新流程：pull → 解析 digest → service update，每一步都可确认或取消
- 自动写入 `docker-compose.yaml` 的 `services.<name>.image`，保留空行、注释与其他字段
- 服务匹配支持同名、stack 前缀后缀（`vtb_songs` → `songs`）、镜像仓库唯一匹配
- `--dry-run` 模式仅展示命令，不做任何改动
- `--version` 输出版本号（构建时注入 git tag）
- 静态编译（`CGO_ENABLED=0`），发布 linux/amd64 与 linux/arm64
