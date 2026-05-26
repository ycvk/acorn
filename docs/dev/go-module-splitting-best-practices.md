# Go 大型模块拆分最佳实践研究报告

## 1. Google Go Style Guide 关于 internal/ 子包和类型别名的建议

### 1.1 internal/ 子包：编译器强制的边界

Go 官方文档（`go.dev/doc/modules/layout`）明确推荐将支持包放入 `internal/` 目录：

> "Larger packages or commands may benefit from splitting off some functionality into supporting packages. Initially, it's recommended placing such packages into a directory named `internal`; this prevents other modules from depending on packages we don't necessarily want to expose and support for external uses. Since other projects cannot import code from our `internal` directory, we're free to refactor its API and generally move things around without breaking external users."

**核心原则：**
- `internal/` 是**编译器强制执行**的边界，不是约定或 linter 规则
- 位于 `internal/` 下的包只能被其父目录树内的代码导入
- 例如 `services/orders/internal/domain` 只能被 `services/orders/` 下的代码导入

**Google Style Guide 的具体建议：**
- 避免无意义的包名：`util`, `common`, `misc`, `models`, `helper` —— 这些会诱使用户重命名导入
- 包名应简短、全小写、有意义（如 `tabwriter` 而非 `tab_writer`）
- 好的包名不需要重命名导入
- 接口应在**消费者**包中定义，而非实现者包中（"Accept Interfaces, Return Concrete Types"）

### 1.2 类型别名：为大规模重构而生

Go 1.9 引入类型别名（`type T1 = T2`），专门解决大规模代码库中的渐进式修复问题。

**Google/Go 团队的设计意图：**

> "The primary motivation is to enable gradual code repair during large-scale refactorings, in particular moving a type from one package to another in such a way that code referring to the old name interoperates with code referring to the new name."

类型别名不是新类型，只是现有类型的**替代拼写**：

```go
// 旧包中的别名声明
package oldpkg

import "newpkg"

// Deprecated: use newpkg.MyType instead.
type MyType = newpkg.MyType
```

**关键特性：**
- `oldpkg.MyType` 和 `newpkg.MyType` 是**完全相同的类型**
- 可以互相赋值、传递，无需转换
- 支持泛型参数（Go 1.24+）

---

## 2. Uber/Shopify 的 Go Monorepo 大模块拆分案例

### 2.1 Uber：Bazel + Gazelle 管理超大规模代码库

**规模：**
- 超过 70,000 个 Go 文件
- 近 3,000 个微服务
- 每天超过 1,000 次提交
- 500,000 次提交分析显示：1.4% 影响超过 100 个服务，0.3% 影响超过 1,000 个服务

**拆分策略：**

1. **从多仓库到 Monorepo**
   - 消除内部依赖版本管理问题
   - 单一提交即可更新所有依赖方
   - 避免数百个仓库的重复更新工作

2. **Bazel 构建系统**
   - 增量式、密封式构建
   - 分布式构建支持
   - Gazelle 自动生成 Go 和 Protobuf 的 Bazel 规则
   - 远程缓存避免重复构建

3. **动态分片 CI**
   - 计算"根目标"（无其他目标依赖的节点）
   - 按根目标分配构建分片，减少重复工作
   - 共享远程 Bazel 缓存存储构建产物和测试结果

4. **跨服务部署编排**
   - 服务分层（Tier 0-5，0 最关键）
   - 渐进式部署：先部署低优先级服务，成功后解锁下一层
   - 失败阈值控制：超过阈值则停止部署并通知作者

### 2.2 Shopify：模块化单体（Modular Monolith）

**背景：**
- 2017 年启动 "Componentization" 项目
- 核心应用仍作为单一单元部署
- 使用 Packwerk 定义和强制执行包边界

**拆分步骤：**

1. **代码重组（Code Re-Organization）**
   - 从按技术层（models/views/controllers）改为按业务领域（orders/shipping/inventory/billing）
   - 每个组件作为独立的 mini app，使用命名空间隔离
   - 使用自动化脚本移动文件（代价是丢失部分 Git 历史）

2. **依赖隔离（Isolating Dependencies）**
   - 每个组件提供清晰的公共 API
   - 数据所有权与组件绑定
   - 内部工具跟踪每个组件向隔离目标的进展

3. **边界强制执行（Enforcing Boundaries）**
   - 组件只加载显式声明的依赖
   - 运行时错误：访问未声明依赖的组件代码
   - 运行时错误：通过非公共 API 访问组件

**关键教训：**
- 不要过早拆分数据库（操作复杂度高、新增故障模式）
- 先模块化代码，保留选择权，仅在扩展需要时才拆分数据库
- 有界上下文不直接共享领域模型，跨上下文通信必须通过显式拥有的契约
- 引入防腐层（Anti-Corruption Layer, ACL）处理新旧代码交互

### 2.3 通用 Monorepo 拆分模式

```
project-root/
├── go.mod
├── cmd/
│   ├── api-server/
│   │   └── main.go
│   └── worker/
│       └── main.go
├── internal/              # 模块私有代码
│   ├── auth/              # 认证逻辑
│   ├── orders/            # 订单领域
│   │   ├── internal/      # 订单子模块私有代码
│   │   │   ├── domain/
│   │   │   ├── usecase/
│   │   │   └── infra/
│   │   └── port/          # 对外暴露的端口
│   ├── billing/           #  billing 领域
│   │   └── internal/
│   └── shared/            # 共享内核（保持极小）
│       └── ids/
└── pkg/                   # 可重用的公共库（谨慎使用）
```

---

## 3. 处理 "alias.go" 模式：逐步删除类型别名

### 3.1 类型别名的生命周期

类型别名是**临时机制**，不是永久 API。其生命周期分为三个阶段：

1. **引入阶段**：移动类型到新包，在旧包创建别名
2. **迁移阶段**：逐步更新消费者使用新导入路径
3. **清理阶段**：确认无消费者后删除别名

### 3.2 创建 Re-Export 层（别名层）

**场景**：将 `internal/model/` 拆分为 `internal/schema/` 和 `internal/enrichment/`

```go
// internal/model/alias.go —— 临时兼容层
package model

import (
    "github.com/ycvk/acorn/internal/schema"
    "github.com/ycvk/acorn/internal/enrichment"
)

// --- 从 schema 包重导出 ---

// OpnSenseDocument represents OPNsense configuration.
// Deprecated: Import from github.com/ycvk/acorn/internal/schema directly.
type OpnSenseDocument = schema.OpnSenseDocument

// Config represents system configuration.
// Deprecated: Import from github.com/ycvk/acorn/internal/schema directly.
type Config = schema.Config

// --- 从 enrichment 包重导出 ---

// EnrichedDocument represents an enriched document.
// Deprecated: Import from github.com/ycvk/acorn/internal/enrichment directly.
type EnrichedDocument = enrichment.EnrichedDocument
```

### 3.3 逐步迁移消费者

**步骤 1**：在旧包添加别名 + Deprecated 注释
**步骤 2**：更新消费者导入路径（可以分批进行）

```go
// 旧用法
import "github.com/ycvk/acorn/internal/model"

func process(doc *model.OpnSenseDocument) { ... }

// 新用法
import "github.com/ycvk/acorn/internal/schema"

func process(doc *schema.OpnSenseDocument) { ... }
```

**步骤 3**：验证无消费者后删除别名

```bash
# 删除前必须验证：grep 整个代码库
grep -r 'model\.OpnSenseDocument' internal/ cmd/ pkg/
grep -r '"github.com/ycvk/acorn/internal/model"' internal/ cmd/ pkg/
```

### 3.4 自动化工具

**`//go:fix` 指令（实验性）**：

```go
//go:fix inline
type OldType = newpkg.NewType
```

运行 `go fix` 会自动将所有 `oldpkg.OldType` 替换为 `newpkg.NewType`。

**`go-refactor` 工具**：

```bash
# 替换类型引用
go-refactor replacetype \
    --type github.com/ycvk/acorn/internal/model.OldType \
    --replacement github.com/ycvk/acorn/internal/schema.NewType \
    ./...
```

### 3.5 删除别名时的关键检查清单

- [ ] `grep -r 'oldpkg\.AliasName' ./...` 无结果
- [ ] `grep -r '"path/to/oldpkg"' ./...` 无结果（或仅剩别名文件自身）
- [ ] 所有内部消费者已更新
- [ ] CI 通过（构建 + 测试）
- [ ] 如果是公共 API，确保已发布至少一个包含别名的版本，给消费者迁移时间

### 3.6 反模式：保留别名太久

> "The aliases created confusion about which name was canonical. Grep returned both. Autocompletion showed both. New code used both names randomly."
>
> "If you have no external consumers, backward compatibility is debt."

**建议**：
- 内部代码库：无外部消费者时，别名应在同一提交中删除
- 公共库：保留一个 major version 周期，明确标记 Deprecated
- 定期审计：检查 `grep -r 'Deprecated.*alias' ./...`

---

## 4. 安全移动包而不破坏构建

### 4.1 渐进式代码修复三阶段

Russ Cox 提出的标准模式：

```
阶段 1: 引入新 API（旧 + 新共存）
   ↓
阶段 2: 逐步转换所有使用（多提交，小步快跑）
   ↓
阶段 3: 删除旧 API
```

**关键原则**：旧 API 和新 API 必须**可互换**，允许在同一个程序中混合使用。

### 4.2 不同声明类型的迁移策略

| 声明类型 | 迁移机制 | 示例 |
|---------|---------|------|
| **常量** | 直接引用 | `const OldConst = newpkg.NewConst` |
| **变量** | 直接引用 | `var OldVar = newpkg.NewVar` |
| **函数** | Wrapper 函数 | `func OldFunc() { newpkg.NewFunc() }` |
| **类型** | **类型别名** | `type OldType = newpkg.NewType` |

**函数迁移示例：**

```go
// 旧包中的转发函数
package oldpkg

import "newpkg"

// Deprecated: use newpkg.Process instead.
func Process(data []byte) error {
    return newpkg.Process(data)
}
```

**常量/变量迁移：**

```go
// 旧包中的转发声明
package oldpkg

import "newpkg"

const MaxBufferSize = newpkg.MaxBufferSize
var DefaultConfig = newpkg.DefaultConfig
```

### 4.3 包移动的具体步骤

**场景**：将 `pkg/old/` 移动到 `pkg/new/`

**步骤 1：创建新包**
```bash
mkdir -p pkg/new
# 移动文件（保留 git history：git mv）
git mv pkg/old/*.go pkg/new/
```

**步骤 2：在新包中声明新 API**
```go
// pkg/new/api.go
package new

type Client struct { ... }

func New() *Client { ... }
```

**步骤 3：在旧包创建兼容层**
```go
// pkg/old/forward.go
package old

import "github.com/ycvk/acorn/pkg/new"

// Deprecated: use new.Client instead.
type Client = new.Client

// Deprecated: use new.New instead.
func New() *new.Client {
    return new.New()
}
```

**步骤 4：逐步更新消费者**
```bash
# 查找所有消费者
grep -r '"github.com/ycvk/acorn/pkg/old"' ./...

# 分批更新（每次一个包或一个目录）
# 使用 sed 或 go-refactor 工具
```

**步骤 5：验证并删除旧包**
```bash
# 确认无引用
grep -r '"github.com/ycvk/acorn/pkg/old"' ./...

# 删除旧目录
rm -rf pkg/old/
```

### 4.4 使用工具自动化

**`golang.org/x/tools/refactor/rename` 的 `mover`：**

```go
// 使用 Go 工具链的包移动工具
import "golang.org/x/tools/refactor/rename"

// Move 函数处理：
// 1. 检查目标路径冲突
// 2. 构建导入图找到所有引用
// 3. 更新所有导入路径
// 4. 移动目录
```

**`gomove` 工具：**

```bash
# 安装
go install github.com/ksubedi/gomove@latest

# 移动包后更新导入路径
# 1. 先物理移动目录
mv internal/oldpkg internal/newpkg

# 2. 更新所有导入路径
gomove -d ./ \
    github.com/ycvk/acorn/internal/oldpkg \
    github.com/ycvk/acorn/internal/newpkg
```

**`go forward`（讨论中，未实现）：**

```go
// forward.go
//go:forward github.com/ycvk/acorn/pkg/new
package old
```

Go 构建系统会自动将所有 `pkg/old` 的导入重写为 `pkg/new`。

### 4.5 避免编译中断的关键技巧

1. **永远不要原子性大改**
   - 大提交容易失败且难以回滚
   - 小提交、频繁提交、保持 CI 绿色

2. **先添加，后删除**
   - 新 API 到位前不删除旧 API
   - 确保每个提交都能独立编译

3. **使用类型别名处理类型移动**
   - 这是唯一能让旧类型和新类型完全互操作的方法
   - 没有别名时，即使结构相同的类型也是不同类型

4. **Wrapper 函数处理函数移动**
   - 保持旧函数签名不变
   - 内部调用新函数

5. **批量更新导入路径**
   - 使用 `sed`, `gomove`, 或 IDE 重构工具
   - 更新后运行 `go mod tidy` 和 `go build ./...`

6. **CI 验证每一步**
   - 每个 PR 必须保持构建通过
   - 使用 `go build ./...` 验证整个代码库

7. **处理循环依赖**
   - 如果移动包会导致循环依赖，考虑：
     - 提取公共接口到第三个包
     - 使用接口打破循环（消费者定义接口）

### 4.6 大型重构的实战建议

**从 Victor Lyuboslavsky (Conf42 Golang 2026) 的经验：**

> "Our back end is around 800,000 lines of code... The problem is what it does to day-to-day engineering. At this size, you stop working in clean, isolated areas of code."

**他的策略：**
1. **垂直切片**：新功能作为独立模块，包含自己的 controller/service/persistence
2. **不拆分数据库**：先模块化代码，保留选择权
3. **防腐层（ACL）**：新代码不直接与遗留代码对话，ACL 是唯一理解两个世界的地方
4. **先模块化基础**：在模块化领域之前，先模块化基础设施（HTTP helpers, DB plumbing）
5. **防止耦合回流**：使用 `internal/` + CI 脚本检查跨上下文导入

---

## 5. 总结：代码迁移策略速查表

| 场景 | 策略 | 工具 |
|------|------|------|
| 拆分大包为子包 | 使用 `internal/` 隔离实现细节 | Go 编译器 |
| 移动类型到新包 | 类型别名 `type Old = new.Type` | `go fix`, `go-refactor` |
| 移动函数到新包 | Wrapper 函数调用新函数 | `go-refactor replacecall` |
| 移动常量/变量 | 直接引用新包声明 | `sed`, `gomove` |
| 批量更新导入路径 | 自动化工具替换 | `gomove`, IDE 重构 |
| 删除别名层 | `grep` 验证无消费者后删除 | `grep`, `go build` |
| 防止跨边界耦合 | `internal/` + CI 导入检查 | `go list`, `depguard` |

**核心原则：**
1. `internal/` 是 Go 最强大的封装工具，默认使用
2. 类型别名是临时债务，不是永久 API
3. 渐进式修复优于大爆炸重构
4. 每个提交保持可编译、可测试
5. 先模块化代码，再考虑拆分数据库/服务
