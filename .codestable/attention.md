# Acorn 注意力文件

本文件记录当前项目的关键上下文，供所有 AI 协作任务参考。

## 项目概况

- **名称**: Acorn
- **语言**: Go 1.26.2
- **定位**: Single-user self-hosted agent backend
- **核心架构**: `Executor -> RunnerFactory/runBuilder -> ContextPlane + OrchestrationPlane -> ContextSession -> SQLite/file-backed memory`

## 当前架构真相

- 31 个 internal/ 子模块
- 111,026 行生产代码
- 91,232 行测试代码（45%）
- 118 个接口定义（30+ 单实现）
- 779 个结构体定义

## 当前工作

- **Refactor**: `consolidate-modules` - 合并微型模块 + 删除单实现接口
- **范围**: P0 高优先级架构简化
- **约束**: 接受破坏性重构，行为等价是底线

## 关键依赖

- Eino (CloudWeGo) - LLM agent 框架
- Bleve + FAISS - 语义检索
- SQLite - 持久化存储
- Chrome DevTools Protocol - 浏览器自动化

## 验证要求

- `make lint` 必须通过
- `make format-check` 必须通过
- 相关包测试必须通过
