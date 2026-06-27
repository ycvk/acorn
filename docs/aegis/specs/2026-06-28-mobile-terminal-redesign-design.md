# Acorn Mobile — Terminal/Dev-tool Aesthetic 全重构

Date: 2026-06-28
Status: Approved
Scope: visual + interaction + architecture (full rebuild)

## 1. Design Direction

**Aesthetic**: Terminal / Dev-tool。Acorn 是跑在 VPS 上的 ambient agent，APP 是它的控制台——不是聊天软件，是运维终端。

**设计原则**:
- 暗色优先，完全弃用 dynamicColor（`dynamicColor = false`）
- 方角为主（4.dp 圆角），terminal 感
- monospace 字体用于 code/run-id/timestamp/status，sans-serif 用于正文
- 状态色驱动交互：绿=运行中/成功，琥珀=pending，红=错误/destructive
- 无 emoji，无 illustration——用 icon + typography 表达层次
- 信息密度高于留白——这是工具不是消费品

## 2. 色板

完全弃用 Material3 默认紫色和 dynamicColor。自定义 `darkColorScheme`：

| 角色 | 色值 | 用途 |
|------|------|------|
| `bg` (background) | `#0D1117` | 主背景 |
| `surface` | `#161B22` | card / bubble / nav bar |
| `surfaceVariant` | `#21262D` | 次级 surface / input field |
| `surfaceContainer` | `#161B22` | M3 surface container |
| `border` | `#30363D` | 边框 / divider |
| `textPrimary` (onSurface) | `#E6EDF3` | 主文字 |
| `textSecondary` (onSurfaceVariant) | `#8B949E` | 次文字 / label |
| `accent` (primary) | `#3FB950` | 绿 — 运行中 / 成功 / primary action |
| `accentDim` (primaryContainer) | `#238636` | primary button bg |
| `onAccent` (onPrimary) | `#0D1117` | accent 上的文字 |
| `warning` (tertiary) | `#D29922` | pending approval |
| `danger` (error) | `#F85149` | 错误 / 停止 / destructive |
| `info` (secondary) | `#58A6FF` | 链接 / info |

不定义 lightColorScheme——暗色优先，light 不支持（`alwaysDark = true`）。

## 3. Typography

不加新字体依赖。用系统 font family：

```kotlin
val Mono = FontFamily.Monospace   // code, run-id, timestamp, status
val Sans = FontFamily.Default     // 正文
```

| 样式 | FontFamily | Size | Weight | 用途 |
|------|------------|------|--------|------|
| `displaySmall` | Sans | 20sp | Bold | screen title |
| `headlineMedium` | Sans | 18sp | SemiBold | card title |
| `titleSmall` | Mono | 12sp | Medium | run-id, thread id, timestamp |
| `bodyLarge` | Sans | 15sp | Normal | chat body |
| `bodyMedium` | Sans | 14sp | Normal | card body |
| `bodySmall` | Mono | 12sp | Normal | metadata, status label |
| `labelLarge` | Sans | 14sp | Medium | button |
| `labelSmall` | Mono | 11sp | Medium | badge, tag |

## 4. Shape

统一 `RoundedCornerShape(4.dp)`。不用 16.dp 大圆角——terminal aesthetic 要方。

- Card/Surface: `4.dp`
- Bubble: `4.dp`（User 右上角直角，Assistant 左上角直角，其余 4dp）
- TextField: `0.dp`（underline style，不是 outlined）
- Button: `4.dp`

## 5. 屏幕重构

### 5.1 PairingScreen → 终端开机

**视觉**:
- 全黑 `#0D1117` 背景
- 居中布局，上方一个 `>` prompt 符号 + "acorn pair" 文字（monospace）
- 三个输入框用 underline style（`OutlinedTextField` 换 `TextField` 默认 underline）
- 配对按钮：`accentDim` 背景，`>` 符号 + "pair" 文字
- 错误信息用 `danger` 色，前面加 `!` 符号

**交互**:
- 配对中按钮显示 spinner
- 成功后直接跳转（已有逻辑）

### 5.2 Shell + 底部导航

**视觉**:
- 底部 `NavigationBar`，`surface` 背景色
- 顶部 1dp border `border` 色
- 3 个 tab：`inbox` / `approvals` / `settings`
- icon 用 Material Icons：`Inbox` / `CheckCircle` / `Settings`
- Approvals tab 有 badge（pending 数量，`warning` 色背景圆点）

**交互**:
- 点击 tab 切换内容区
- Approvals badge 实时更新（已有 inbox 聚合，从 ViewModel 拿 pending count）

### 5.3 ThreadsScreen → Inbox

**视觉**:
- Top bar：`inbox` 标题（`displaySmall`，sans bold）
- 每个 thread item 改为 card 风格：
  - 第一行：thread title（`headlineMedium`）+ 右侧 timestamp（`titleSmall` mono）
  - 第二行：last message preview（`bodyMedium`，1 行 ellipsis，`textSecondary` 色）
  - 左侧 4dp 竖线 accent 色表示 active/unread
- FAB：`accentDim` 背景圆角 4dp，`+` icon

**交互**:
- 点击 thread 进 chat
- FAB 创建新 thread
- 空状态：居中 `>` prompt + "no threads. tap + to start"（mono）

**数据**:
- 当前 `threads` API 只返回 id + title。last message preview 需要从 thread 的 messages 拿。
- 不改 API——从 `GET /v1/threads/{id}/messages` 取最后一条（或 inbox 聚合已有）。
- 如果 inbox API 已返回 last message，直接用；否则 fallback 到 thread title。

### 5.4 ChatScreen

**视觉**:
- Top bar：
  - 左：back arrow
  - 中：thread title（`headlineMedium`）+ 下方 run status（`bodySmall` mono，绿点=running / 灰=idle）
  - 右：overflow menu（暂留 placeholder）
- Message list:
  - User bubble：`accentDim` 背景，右对齐，`onAccent` 色文字，`4.dp` 圆角（右上直角）
  - Assistant bubble：`surface` 背景，左对齐，`textPrimary` 色，`4.dp` 圆角（左上直角），左边框 1dp `border` 色
  - Streaming bubble：同 assistant + 底部 `▌` 光标
  - Reasoning block：`surfaceVariant` 背景，`textSecondary` 色，可折叠，label 用 `▸ thinking`
- Composer:
  - `surfaceVariant` 背景，`4.dp` 圆角容器
  - TextField underline style，`textPrimary` 色
  - Send 按钮：`accent` 色 icon，streaming 时变为 `danger` 色 stop icon
- Activity row：`surfaceVariant` 背景，`info` 色左竖线，`bodySmall` mono

**交互**:
- 自动滚动到底部（已有）
- streaming 时 send→stop 按钮切换（已有）

### 5.5 ApprovalsScreen

**视觉**:
- Top bar：`approvals` 标题 + pending count badge
- 每个 pending action 是 card：
  - 标题（`headlineMedium`）
  - 风险标签：`warning` 色背景 chip + "RISK" 文字
  - 描述（`bodyMedium`）
  - 选项 chips：每个是 `surfaceVariant` 背景可点击 chip
  - 决策中 spinner
- 空状态：`>` prompt + "no pending approvals"

**交互**:
- 点击 chip 发起决策
- 决策中 chip 禁用 + spinner
- ModalBottomSheet 保留（已有）

### 5.6 SettingsScreen

**视觉**:
- Server URL（mono，`textSecondary`）
- Device name + token info
- Disconnect 按钮：`danger` 色 outline button
- 关于信息：Acorn version（mono）

**交互**:
- Disconnect 确认 dialog
- 保留已有逻辑

## 6. 架构改动

### 6.1 Theme 重写

`Color.kt` / `Theme.kt` / `Type.kt` 完全重写：
- 弃用 dynamicColor
- 自定义 darkColorScheme
- 新增 `Shape.kt`（统一 4dp）
- 新增 `Mono` font family 常量

### 6.2 新增 Inbox 视图概念

当前 ThreadsScreen 只有 threads 列表。全重构改为 Inbox——对齐后端 `GET /v1/inbox`：
- 聚合 active runs + pending actions + threads
- 但不改 API——如果 inbox API 已有数据就用，否则从现有 threads + approvals ViewModel 组合

**决策**: 不新增 inbox API 调用。保留 threads 列表但视觉改为 inbox 风格（加 last message preview + timestamp + active indicator）。这是视觉重构不是数据层重构。

### 6.3 RunStatus 指示器

ChatScreen top bar 加 run status indicator：
- 绿点 + "running" = 有 active run
- 灰点 + "idle" = 无 active run
- 数据从 ChatViewModel 的 `chatState.isStreaming` 拿（已有）

### 6.4 Approvals badge

底部导航 approvals tab 显示 pending count badge：
- 从 ApprovalsViewModel 拿 pending actions count
- 但 ApprovalsViewModel 是 per-screen 的，ShellViewModel 不持有它
- **方案**: ShellViewModel 加一个 `pendingCount` StateFlow，通过 inbox 轮询拿 pending count

**简化**: 不加 inbox 轮询。ApprovalsScreen 加载时把 count 写回 ShellViewModel（通过 shared Hilt singleton 或 SharedFlow）。

**最简**: ShellViewModel 定期轮询 `GET /v1/pending-actions`（已有 API），维护 pendingCount。每 30s 一次。

## 7. 不做

- 不加 QR 扫描 UI（CameraX 依赖在但 QR 扫描不在这次范围）
- 不加 light theme
- 不加 i18n（保持英文 UI 文案）
- 不改 OpenAPI client / API DTO
- 不加新依赖（不加 Lottie / Compose animation lib / 字体包）
- 不改 ViewModel 架构（保留 Hilt + StateFlow）
- 不加 push notification（SSE 保留现有逻辑）

## 8. 文件改动清单

### 重写
- `core/theme/Color.kt` — 新色板
- `core/theme/Theme.kt` — 弃 dynamicColor，暗色优先
- `core/theme/Type.kt` — 新 typography + mono
- `feature/chat/ChatScreen.kt` — bubble/composer/topbar 重做
- `feature/threads/ThreadsScreen.kt` — inbox 风格 thread item
- `feature/approvals/ApprovalsScreen.kt` — card 风格 + risk label
- `feature/pairing/PairingScreen.kt` — 终端开机风格
- `feature/shell/AcornShell.kt` — nav badge + icon 更新

### 新增
- `core/theme/Shape.kt` — 统一 shape

### 改动
- `feature/shell/ShellViewModel.kt` — 加 pendingCount 轮询
- `feature/threads/ThreadsViewModel.kt` — 加 last message preview（如果需要）

### 不动
- `api/` — OpenAPI client 不动
- `core/auth/` — auth 逻辑不动
- `core/sse/` — SSE 逻辑不动
- `core/di/` — Hilt module 不动
- `data/repository/` — repository 不动
- `ChatViewModel.kt` / `ApprovalsViewModel.kt` — ViewModel 逻辑基本不动

## 9. 验证

- `./gradlew assembleDebug` 编译通过
- 每个屏幕在暗色下视觉一致
- 不引入新依赖（`build.gradle.kts` 不改）
- 不改 API client（`api/` 目录不动）
- 架构守卫 `shipped_artifacts_test.go` 通过
