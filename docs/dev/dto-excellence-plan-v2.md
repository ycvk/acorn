# DTO 层卓越化重构计划 v2.0

> 基于对大型 Go 项目（Kubernetes、Docker、Grafana、Google go-github、xpay、go-arch、supernova 等）DTO/Model 映射模式、代码生成工具生态和 JSON 序列化最佳实践的调研。

---

## 一、调研结论：什么是"最佳实践"

### 1.1 DTO 文件组织（已调研 8 个大型 Go 项目）

| 项目 | 模式 | DTO 位置 | 映射方式 |
|------|------|----------|----------|
| **xpay** | 按领域分文件 | `internal/server/dto/{domain}.go` | 手写函数 |
| **platform-go** | 混合 Model+DTO | `internal/domain/` 同时包含 | 零映射（直接复用） |
| **supernova** | Protobuf 生成 | `api/proto/` → 代码生成 | protoc-gen-go |
| **go-github** | 按 API 端点分文件 | `github/{resource}.go` | 无 DTO（直接 struct） |
| **go-arch** | 垂直切片 | `internal/modules/{x}/dto.go` | 手写函数 |
| **GoDev Kit** | Clean Architecture | `internal/domain/dto.go` | 手写函数 |

**共识**：手写映射函数是 Go 社区主流（5/8 项目），因为：
- Go 缺乏 Java MapStruct / Rust derive 的成熟生态
- 反射方案（copier、gmapper）有 50x-5000x 运行时开销
- 代码生成工具（goverter、dtogen、sesame）要么维护不活跃，要么对条件逻辑支持有限
- Protobuf 方案适合 gRPC，不适合 JSON-first REST API

### 1.2 手写 Mapping 的优化模式

从 `go-github`（58k stars）和 Google 内部 Go 风格指南学到的模式：

**模式 A：Package-level 纯函数**（推荐）
```go
func ThreadDTOFromDomain(t app.Thread) ThreadDTO
func ThreadDTOsFromDomain(items []app.Thread) []ThreadDTO
```
- 优点：零状态依赖、容易测试、可独立复用
- 缺点：如果函数太多，文件会变大

**模式 B：Mapper struct 方法**（大型项目）
```go
type Mapper struct{}
func (m Mapper) ThreadFromDomain(t app.Thread) ThreadDTO
```
- 优点：可注入配置、可 mock
- 缺点：Go 的 struct 方法无继承，不如 interface 灵活

**模式 C：Converter interface + 代码生成**（goverter 模式）
```go
type Converter interface {
    ThreadDTOFromDomain(app.Thread) ThreadDTO
}
// 生成 ConverterImpl{}
```
- 优点：类型安全、零运行时开销
- 缺点：工具链依赖、对复杂逻辑支持差

**Acorn 决策**：保持模式 A（package-level 函数），因为：
- 当前 DTO/Model 差异包含大量条件逻辑（`MarshalJSON` 的 switch-case、`PendingActionDecisionDTO` 的 JSON 解析）
- 映射函数是幂等纯函数，无状态，无需 struct 封装
- 测试已基于函数级别，迁移成本低

### 1.3 自定义 JSON 序列化的最佳实践

从 Go 官方 `encoding/json/v2` 实验项目、Google go-github 和多个博客学到的模式：

**反模式**：一个 `MarshalJSON` 方法超过 50 行
**最佳实践**：
1. 每个 kind 一个独立辅助函数
2. 使用 `type Alias MyType` 避免递归
3. 测试每个 kind 的 JSON 输出（而非只测 Go struct）

### 1.4 测试文件对应关系

Go 标准：`*_test.go` 必须与 `*.go` 一一对应。

**反模式**：`client_dto_test.go` 测试 `message_dto.go` 中的函数
**正确做法**：拆分为 `message_dto_test.go`

---

## 二、当前差距（精确到行号）

### 差距 1：测试文件未对齐

`internal/web/client_dto_test.go`（64 行）测试的是 `message_dto.go` 中的函数：
- 第 12 行：`messageDTOFromDomain`
- 第 40 行：`messageDTOFromDomain`

违反 Go 测试文件命名规范。

### 差距 2：`message_dto.go` 过大

266 行，其中 `MessagePartDTO.MarshalJSON()` 占 93 行（第 62-154 行）。

这个方法的复杂度：
- 6 个 case（text, reasoning, work_status, decision, result, tool_call）
- 每个 case 构造一个匿名 struct
- 包含 `nonNilStrings` 的条件调用

### 差距 3：映射函数命名不一致

部分函数以 DTO 类型命名：
- `threadDTOFromDomain` ✅
- `messageDTOFromDomain` ✅
- `runDTOFromDomain` ✅

部分函数以操作命名：
- `pendingActionDecisionDTOFromDomain` ✅
- `pendingActionDecisionFields` ❌（返回多个值，不是 DTO）
- `clientSettingsDTOFromConfig` ✅
- `inboxDTOFromDomain` ✅
- `systemStatusDTOFromSnapshot` ✅

命名总体一致，但 `pendingActionDecisionFields` 这个辅助函数应该更名或提取。

### 差距 4：`nonNilStrings` 辅助函数位置

当前在 `message_dto.go` 第 261 行，但也被 `client_dto_test.go` 间接使用（通过 `messageDTOFromDomain`）。

这是一个通用的 slice 辅助函数，应该放在更通用的位置，或者内联到需要的地方。

---

## 三、重构方案（分 3 个步骤）

### 步骤 1：测试文件拆分（30 分钟）

**目标**：将 `client_dto_test.go` 拆分为对应的领域测试文件

**变更**：

1. **创建 `internal/web/message_dto_test.go`**：
   - 迁移 `TestMessageDTOFromDomainEmitsResultArrays`
   - 迁移 `TestMessageDTOFromDomainEmitsReasoningPart`

2. **删除 `internal/web/client_dto_test.go`**

3. **（可选）创建其他测试文件**：
   如果其他 DTO 文件中的函数没有对应测试，暂时不创建空测试文件。

### 步骤 2：`MarshalJSON` 提取重构（60 分钟）

**目标**：将 `MessagePartDTO.MarshalJSON()` 提取到独立文件，降低 `message_dto.go` 复杂度

**方案 A（推荐）：提取到 `message_part_json.go`**

创建 `internal/web/message_part_json.go`：
```go
package web

import "encoding/json"

// MessagePartDTO.MarshalJSON implements custom JSON serialization.
// Each kind emits only its relevant fields.
func (part MessagePartDTO) MarshalJSON() ([]byte, error) {
    switch part.Kind {
    case "text":
        return marshalTextPart(part)
    case "reasoning":
        return marshalReasoningPart(part)
    // ...
    }
}

func marshalTextPart(part MessagePartDTO) ([]byte, error) {
    return json.Marshal(struct {
        Kind string `json:"kind"`
        Text string `json:"text"`
    }{Kind: part.Kind, Text: part.Text})
}
// ...
```

**方案 B：重构为 struct 组合**
```go
func (part MessagePartDTO) MarshalJSON() ([]byte, error) {
    type Alias MessagePartDTO
    
    switch part.Kind {
    case "text":
        return json.Marshal(textPartJSON{Kind: part.Kind, Text: part.Text})
    // ...
    }
}

type textPartJSON struct {
    Kind string `json:"kind"`
    Text string `json:"text"`
}
```

**建议**：选方案 A（提取函数），因为：
- 每个 kind 的 JSON 结构不同，无法共享一个 struct
- 函数提取后，`message_dto.go` 回到 ~170 行（健康范围）

### 步骤 3：通用辅助函数整理（15 分钟）

**目标**：整理 `nonNilStrings` 和其他跨文件辅助函数

**变更**：

1. **移动 `nonNilStrings` 到 `util.go` 或保持原位**：
   因为目前只有 `message_dto.go` 使用，可以保持原位。如果未来更多文件使用，再提取。

2. **重命名 `pendingActionDecisionFields`**：
   这个函数从 `events.PendingActionRecord.DecisionJSON` 解析出 3 个字段，语义是"解析决策 payload"。
   建议更名为 `parsePendingActionDecision` 或 `pendingActionDecisionFromJSON`。

---

## 四、文件变更汇总

### 新增文件

| 文件 | 内容 | 行数估计 |
|------|------|---------|
| `internal/web/message_dto_test.go` | 从 client_dto_test.go 迁移的测试 | ~65 |
| `internal/web/message_part_json.go` | MessagePartDTO.MarshalJSON 提取 | ~95 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `internal/web/message_dto.go` | 删除 MarshalJSON 方法（-93 行） |
| `internal/web/pending_action_dto.go` | 重命名 `pendingActionDecisionFields` |
| `internal/web/client_dto_test.go` | 删除（内容已迁移） |

---

## 五、验收标准

- [ ] `client_dto_test.go` 已删除
- [ ] `message_dto_test.go` 包含所有 Message DTO 相关测试
- [ ] `message_dto.go` < 180 行
- [ ] `message_part_json.go` 包含所有 kind 的序列化逻辑
- [ ] 每个 kind 的 JSON 输出都有测试覆盖
- [ ] `go build ./...` 通过
- [ ] `go test ./internal/web/...` 通过
- [ ] `make lint` 无新增问题
- [ ] `make format-check` 无新增问题
- [ ] `python3 mobile/tool/generate_openapi_client.py --check` 通过

---

*文档版本：v2.0*
*调研日期：2026-05-24*
*基于项目：xpay, platform-go, supernova, go-github, go-arch, go-dev-kit, entigo, dtogen 等*
