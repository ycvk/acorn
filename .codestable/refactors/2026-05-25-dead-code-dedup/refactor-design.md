# Refactor Design: Dead Code Elimination & Duplication Removal

## 目标

消除 Acorn 项目中两个最高优先级的架构拖累：
1. **删除 `internal/runprojection/` 纯死代码包**（~1,200 行）
2. **消除 `internal/toolfactory/` 与 `internal/runtime/` 之间的三重代码重复**（~800 行）

---

## 问题 A：删除 `runprojection` 死代码包

### 现状分析

| 指标 | 数值 |
|------|------|
| 非测试文件 | 7 个 |
| 测试文件 | 1 个（`stream_projection_roundtrip_test.go`, 969 行） |
| 总行数 | ~1,200 行 |
| 外部 import 数 | **0** |
| 外部引用数 | **0** |
| 是否有 `init()` | **无** |
| 是否有反射 | **无** |
| 是否有 interface 实现 | **无**（未被任何 interface 类型引用） |

**重复关系**：`runprojection` 中几乎所有导出函数都在 `runtime/stream/` 中有等价实现：

| `runprojection` 函数 | `runtime/stream/` 等价函数 | 差异 |
|----------------------|---------------------------|------|
| `AppendStreamItem` | `AppendStreamItem` | 参数类型：`EventAppender` vs `api.EventAppender` |
| `ProjectStreamItemToEvent` | `ProjectStreamItemToEvent` | 内部调用：`StreamKindToEventKind` vs `streamKindToEventKind`（导出 vs 未导出） |
| `ProjectEventToStreamItem` | `projectEventToStreamItem`（未导出） | 包名不同，逻辑等价 |
| `BuildTrace` | `BuildTrace` | **完全一致** |
| `StreamKindToEventKind` | `streamKindToEventKind`（未导出） | 导出 vs 未导出 |
| `StreamPayloadMap` | `streamPayloadMap`（未导出） | 导出 vs 未导出 |

### 删除策略

采用 **Pure Deletion（纯删除）**，无需 Parallel Change。

#### 删除顺序

```
1. 删除测试文件：runprojection/stream_projection_roundtrip_test.go
2. 删除源码文件（按依赖从内到外）：
   - helpers.go（基础辅助函数，无外部依赖）
   - runstream_alias.go（别名定义）
   - stream_message.go（消息类型）
   - stream_payload_decode.go（payload 解码）
   - stream_projection.go（核心投影逻辑）
   - trace_projector.go（trace 投影）
   - test_helpers.go（测试辅助，仅在包内使用）
3. 删除空目录：internal/runprojection/
4. 验证编译和测试
```

#### 风险评估

| 风险 | 等级 | 缓解措施 |
|------|------|----------|
| 误删仍有间接引用的代码 | 低 | 已用 `grep -r "runprojection"` 确认 0 外部引用；检查 `init()` 和反射 |
| 删除后编译失败（如某文件 import runprojection） | 极低 | `go build ./...` 验证 |
| 删除测试导致覆盖率下降 | 低 | 保留的 `runtime/stream/` 已有等价测试；`runtime/stream_projection_roundtrip_test.go` 提供覆盖 |

#### 回滚策略

- 使用 git branch：`git checkout -b refactor/remove-runprojection`
- 单次 commit 包含全部删除
- 回滚：`git revert HEAD` 或 `git checkout main -- internal/runprojection/`

#### 验证清单

- [ ] `go build ./...` 通过
- [ ] `go test ./internal/runtime/...` 通过（确保 runtime/stream 测试仍覆盖等价逻辑）
- [ ] `go test ./...` 全量通过
- [ ] `make lint` 通过
- [ ] `git diff --stat` 确认仅删除了预期文件

---

## 问题 B：消除 `toolfactory` ↔ `runtime` 代码重复

### 现状分析

| 重复对 | 文件 A | 文件 B | 重复行数 | 重复类型 |
|--------|--------|--------|----------|----------|
| Toolset 包装器 | `toolfactory/toolset.go` (47行) | `runtime/runner_toolset.go` | ~45 行 | **几乎完全一致** |
| Memory 工具构建 | `toolfactory/memory_tools.go` (320行) | `runtime/memory_tools.go` | ~200 行 | **开头相同，runtime 扩展更多** |
| 工具集构建逻辑 | `toolfactory/builder.go` (230行) | `runtime/runner_toolset_build.go` (664行) | ~150 行 | **逻辑重叠，runtime 版本更复杂** |

**重复本质**：两个包都实现了"从配置构建工具集"的能力，但：
- `toolfactory` 是面向静态/预构建场景的生产者
- `runtime` 是面向动态运行场景的消费者，却重复实现了生产逻辑

**设计原则**：`runtime` 应该**消费** `toolfactory` 的产出，而不是重复生产。

### 重构策略

采用 **Extract + Redirect（提取公共逻辑 + 重定向引用）**，分三步执行。

#### 步骤 1：提取公共 Toolset 包装器（低风险）

**范围**：`toolfactory/toolset.go` ↔ `runtime/runner_toolset.go`

**操作**：
1. 保留 `toolfactory/toolset.go` 作为**唯一**的 `Toolset` 定义
2. 删除 `runtime/runner_toolset.go` 中的重复定义
3. 在 `runtime` 中通过类型别名或重新导出暴露 `Toolset`：
   ```go
   // runtime/runner_toolset.go
   import "github.com/ycvk/acorn/internal/toolfactory"

   type Toolset = toolfactory.Toolset  // 类型别名，零成本适配
   ```

**验证**：编译通过 + `runtime` 包测试通过。

#### 步骤 2：提取公共 Memory 工具构建（中风险）

**范围**：`toolfactory/memory_tools.go` ↔ `runtime/memory_tools.go`

**分析**：
- `toolfactory/memory_tools.go` (320 行) 定义 `BuildMemoryFileTools` 函数
- `runtime/memory_tools.go` 定义了相同的 `BuildMemoryFileTools` 函数，外加额外的 memory 工具类型（`memoryNamespacedTool`、`memorySearchTool` 等）

**操作**：
1. 将 `BuildMemoryFileTools` 函数保留在 `toolfactory` 中
2. 删除 `runtime/memory_tools.go` 中的重复 `BuildMemoryFileTools`
3. `runtime` 直接调用 `toolfactory.BuildMemoryFileTools(...)`
4. `runtime` 中特有的 memory 工具类型（`memoryNamespacedTool` 等）保留在 `runtime/memory_tools.go` 中

**验证**：
- `go build ./...`
- `go test ./internal/toolfactory/...`
- `go test ./internal/runtime/...`

#### 步骤 3：提取公共工具集构建逻辑（高风险，需仔细）

**范围**：`toolfactory/builder.go` ↔ `runtime/runner_toolset_build.go`

**分析**：
- `toolfactory/builder.go` (230 行)：面向 `Builder` 模式，暴露 `Build()` 方法
- `runtime/runner_toolset_build.go` (664 行)：面向 `RunnerFactory` 内部方法，包含 web/browser 服务构建、工具加载等

**核心发现**：两段代码包含**完全相同的 web/browser 服务构建代码块**：
```go
// 两段代码中都出现的相同模式：
webFetchService, err := webaccess.NewFetchService(webaccess.FetchConfig{...})
webSearchService, err := webaccess.NewSearchService(webaccess.SearchConfig{...})
browserService, err := browser.NewService(browser.Config{...})
```

**操作**：
1. 在 `toolfactory` 中新增一个**内部辅助函数** `buildWebAndBrowserServices(cfg)` 封装公共构建逻辑
2. `toolfactory/builder.go` 和 `runtime/runner_toolset_build.go` 都调用此辅助函数
3. 如果 `runtime` 版本包含额外的运行时特有逻辑（如动态工具加载、context 感知），保留在 `runtime` 中

**关键决策**：不试图完全统一两个包的工具集构建，只提取**真正重复**的技术基础设施代码（web/browser 服务构建）。保留各自入口的语义包装（`Builder.Build()` vs `RunnerFactory.buildToolset()`）。

### 依赖与循环依赖风险评估

| 检查项 | 结果 |
|--------|------|
| `runtime` 当前是否 import `toolfactory` | **否**（需要新增 import） |
| `toolfactory` 当前是否 import `runtime` | **否** |
| 引入后是否产生循环依赖 | **否**（单向：runtime → toolfactory） |
| `toolfactory` 的依赖是否干净 | 是，依赖 `tooling`、`memorymodule`、`workspace` 等基础包 |

**结论**：安全，无循环依赖风险。

### 回滚策略

- 使用 git branch：`git checkout -b refactor/dedup-toolfactory-runtime`
- 每个步骤独立 commit：
  - `commit 1: extract toolfactory.Toolset as canonical, remove runtime duplicate`
  - `commit 2: extract BuildMemoryFileTools to toolfactory, runtime delegates`
  - `commit 3: extract web/browser service builder helper`
- 回滚：可逐 commit `git revert` 或整 branch 丢弃

### 验证清单

- [ ] `go build ./...` 通过
- [ ] `go test ./internal/toolfactory/...` 通过
- [ ] `go test ./internal/runtime/...` 通过
- [ ] `go test ./...` 全量通过
- [ ] `make lint` 通过
- [ ] `make format-check` 通过
- [ ] 检查 `runtime` 的公开 API 是否变化（不应变化，因为 `Toolset` 是类型别名）

---

## 执行顺序

```
Phase 1: 问题 A（runprojection 删除）
  └─ 低风险、纯删除、可独立验证
  └─ 预期减少 ~1,200 行

Phase 2: 问题 B（toolfactory/runtime 重复消除）
  ├─ Step 1: Toolset 包装器提取（低风险）
  ├─ Step 2: Memory 工具构建提取（中风险）
  └─ Step 3: 工具集构建逻辑提取（高风险）
  └─ 预期减少 ~400-600 行（删除重复 + 新增公共函数）
```

**为什么先 A 后 B？**
- A 是纯删除，零行为改变，快速获得正向反馈
- B 需要新增 import 和接口调整，风险更高，放在后面

---

## 总体风险评估

| 维度 | 评估 |
|------|------|
| 行为改变风险 | **极低到中等** — A 纯删除零行为改变；B 提取公共逻辑需验证等价性 |
| 编译风险 | **低** — 每次步骤后都编译验证 |
| 测试覆盖 | **高** — 两个包都有大量测试，可自证行为等价 |
| 回滚难度 | **低** — git branch + 独立 commit |
| 预估工作量 | A: 30 分钟；B: 2-3 小时 |

---

## 批准前确认

**请用户确认以下决策：**

1. ✅ 是否接受 Phase 1（runprojection 删除）立即执行？
2. ✅ 是否接受 Phase 2 Step 1（Toolset 提取）的执行策略（类型别名）？
3. ✅ 是否接受 Phase 2 Step 3 的**保守策略**（只提取 web/browser 公共代码，不强行统一整个构建流程）？
4. ✅ 是否需要我在执行每一步后立即汇报结果并等待下一步确认？

**批准后开始执行。**
