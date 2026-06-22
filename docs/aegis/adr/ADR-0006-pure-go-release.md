# ADR-0006: Release 简化为纯 Go build

Date: 2026-06-21
Status: Accepted
Supersedes: (none)

## Context

Acorn release build 原有 FAISS C 库依赖：`CGO_ENABLED=1` + `-tags "bleve_faiss vectors"` + FAISS artifact 下载 + cross-compile lib linking + OpenBLAS runtime + `libgomp1`。`build-faiss-artifacts.sh` 9.9KB，`install-release.sh` 安装 FAISS lib + OpenBLAS + semantic rebuild。整个 release 流程是项目最复杂的部分。

## Decision

Release 变为纯 Go build：

- `build-release.sh`：`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`，无 build tags，无 FAISS libs
- `install-release.sh`：安装 binary + skills seed pack + systemd unit，不安装 FAISS/OpenBLAS/libgomp1，不跑 semantic rebuild
- Makefile：删除 `dev-faiss-artifacts` / `dev-build-faiss` / `dev-serve-faiss` target
- 删除 `scripts/build-faiss-artifacts.sh` / `scripts/run-with-faiss-artifacts.sh` / `deploy/faiss.version`

## Consequences

- **正面**：release 流程极简（纯 Go 交叉编译），编译时间大幅降低，无 cross-compile FAISS 痛点，Linux arm64 + amd64 零障碍
- **负面**：无——FAISS 的语义检索能力已由 embedding + SQLite 替代（ADR-0003）
- **风险**：无 CGO 依赖意味着不能使用任何 CGO 库——当前项目无此需求

## Baseline Sync

- `Makefile` 已重写（纯 `go build`）
- `scripts/build-release.sh` 已重写（`CGO_ENABLED=0`）
- `scripts/install-release.sh` 已重写（无 FAISS/OpenBLAS）
- `deploy/faiss.version` 已删除
- `CGO_ENABLED=0 go build ./...` 通过
