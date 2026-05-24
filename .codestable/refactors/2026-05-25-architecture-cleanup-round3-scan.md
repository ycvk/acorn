# Round 3 架构扫描：深层设计债务

扫描日期：2026-05-25
基于 Round 2 已完成成果继续深入。

---

## 问题 1：contextplane 测试耦合具体 SQLite 实现（高优先级）

**现状**：
- `internal/contextplane/contextplane_test.go` 和 `contextplane_graph_test.go` 直接导入 `internal/store/sqlite`
- 测试用 `store/sqlite.Store` 作为 `ContextPlane` 的输入，而不是接口
- 这迫使 `store/sqlite` 导入 `orchestrationmode`，而 `orchestration` 导入 `contextplane`，形成循环依赖风险

**为什么这是拖累**：
- 测试应该测试行为，不应该测试具体实现
- 任何 `store/sqlite` 的改动都会触发 `contextplane` 测试重新编译
- 阻止了 `orchestrationmode` 合并到 `orchestration`（因为合并后 `store/sqlite` 会依赖 `orchestration`，而 `orchestration` 依赖 `contextplane`，`contextplane` 测试依赖 `store/sqlite` → 循环）

**根因**：
- `contextplane_test` 需要一个"真实"的 store 来测试，但应该用内存 fake 或接口实现

---

## 问题 2：orchestrationmode 微型包的归属不明确（中优先级）

**现状**：
- `internal/orchestrationmode/mode.go` 仅 18 行
- 被 23 个文件/10+ 包导入
- `orchestration` 包也导入它
- 零内部依赖，完全独立

**为什么这是拖累**：
- 目录和 import 路径的维护成本 > 18 行代码的认知价值
- 每个看到这个包的开发者都会疑惑"为什么一个 Mode 类型要独立成包"
- 社区惯例：基础类型应放在被最多消费者导入的包中

**根因**：
- `Mode` 是一个基础枚举类型，理论上应该放在 `internal/events` 或 `internal/model` 这种更基础的包中，因为 events、runstream、store 这些底层包都依赖它

---

## 问题 3：runtime 包边界是否要进一步拆分（低优先级，已降级）

**现状**：
- Round 2 已将 `runner.go` 和 `plan.go` 按领域拆分为 10 个文件
- 最大文件 795 行（`plan_evidence.go`），全部 < 1000 行
- `internal/runtime/` 共约 31 个非测试 Go 文件

**评估结论**：
- Go 标准库 `net/http` 有 50+ 文件，`database/sql` 有 30+ 文件
- Kubernetes `pkg/` 下大量 20-50 文件的包
- **当前文件级拆分已经足够**。进一步拆分为 `runtime/build`、`runtime/plan` 等子包会引入：
  - 跨包循环依赖（runner 创建 PlanNode，PlanNode 依赖 RunnerFactory）
  - 大量类型需要导出（当前很多是包内私有的）
  - 接口膨胀问题
- **建议：暂时不拆分子包**。如果未来 runtime 增长到 50+ 文件再考虑

---

## 问题 4：store/sqlite 导入 orchestrationmode 的间接耦合（中优先级）

**现状**：
- `internal/store/sqlite/mode.go` 导入 `orchestrationmode` 仅用于 `orchestrationmode.Normalize()` 和两个常量
- `store/sqlite` 作为底层持久化层，不应该依赖任何上层业务概念

**为什么这是拖累**：
- 持久化层依赖业务层的 orchestration mode 概念，违反了分层架构
- 如果未来 `orchestrationmode` 移动或改名，`store/sqlite` 需要跟着改

**根因**：
- `Normalize()` 函数应该放在 `store/sqlite` 内部或 `internal/events` 中，而不是在 `orchestrationmode` 包中

---

## 优先级排序

1. **问题 1（高）**：contextplane 测试解耦 → 解除循环依赖风险
2. **问题 4（中）**：store/sqlite 不再导入 orchestrationmode → 分层清晰
3. **问题 2（中）**：orchestrationmode 移动到更合适的位置（如 `internal/events`）→ 消除微型包

**问题 3（低，本次不处理）**：runtime 子包拆分 → 已有文件级拆分足够
