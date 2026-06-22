# Acorn 后端残留清理 + Kotlin 移动端迁移设计

Date: 2026-06-22
Status: Design Spec (pending user review)
Scope: backend dead-shell removal + mobile Flutter→Kotlin migration
Related: `docs/aegis/specs/2026-06-21-acorn-refactor-design.md` (大部分后端工作已落地)

## 1. 动机

### 1.1 前序重构已落地

PR #8 (`refactor: architecture simplification`) 及后续 commit 已完成:

- `plan_execute` / `single_agent` 编排模式删除(`internal/runtime/plan/` 已删)
- `CompactionEngine` + 8 种 rehydration packet 删除,改为 hybrid masking + auto-compact
- Bleve + FAISS + CGO 删除,改为 OpenAI embedding + SQLite BLOB + 纯 Go 暴力检索
- MCP server mode 删除(保留 client)
- SQLite schema 从 ~23 表降到当前实际状态

### 1.2 残留死壳

大重构后遗留了一批"砍了一半"的抽象和死代码:

1. **mode 路由壳**:`OrchestrationMode` 类型 + `ModeDirectResponse` 常量 + `parseClientRunMode` + `assembleRunnerByMode` 全链路只剩一个 mode 还在传参。
2. **`compression_token_counter.go`**:100 行独立类型,无任何引用(死文件)。
3. **smoke `--mode` flag**:help 文本和 flag 仍提 `plan_execute`,实际已不可用。
4. **`.artifacts/faiss-native/`**:13MB 死目录,FAISS 已删。
5. **skill lifecycle/assess 企业级机制**:`skill_assess` tool + `UpdateSkillLifecycle` + `BuildHealthReport` + `LifecycleStatus` 五态 + `AssessmentVerdict` 三态 + `EvidenceRefs` + `RoutingFixture`。单用户日常助手不需要企业级 skill 知识管理。
6. **`capability_service_snapshot.go` MCP provider 健康探测**:375 行中大量 `providerReadinessFromCapability` / `providerStartupReason` / `resolveProviderStatuses` 为 doctor 和 inbox 聚合 MCP provider 健康状态,但单用户场景 MCP provider 状态检查过重。

### 1.3 移动端方向

Flutter 移动端当前可用(~9800 行 Dart,已 feature 分包),但用户决定换技术栈:**Kotlin + Jetpack Compose**。理由:与 Go 后端静态类型偏好一致、性能最佳、包体积小、OpenAPI 生成链路成熟。

## 2. 决策记录

| 决策点 | 选择 | 理由 |
|---|---|---|
| mode 路由 | 固定 `direct_response`,删 `OrchestrationMode` 类型 + 全链路参数 | 只有一个 mode,保留类型是过度设计 |
| `compression_token_counter.go` | 删 | 零引用死文件 |
| smoke `--mode` flag | 删 flag + help 文本 | 不可用 flag 误导用户 |
| `.artifacts/faiss-native/` | 删 | 13MB 死目录 |
| skill lifecycle/assess | 删 `skill_assess` tool + `UpdateSkillLifecycle` + `BuildHealthReport` 中 RoutingFixture + `LifecycleStatus` 多态 + `AssessmentVerdict` + `EvidenceRefs` | 单用户不需要企业级 skill 知识管理 |
| skill 保留 | `Recommend` + `ActivateExplicit` + `Evaluate` + `Scan` + `Loader`(read-only) + `Writer`(create/update skill 文件) + 简单 health(loader problem + duplicate trigger) | 核心能力保留 |
| `capability_service_snapshot.go` | 保留 `Snapshot` 核心 + 简化 MCP provider readiness 为 startup status only,删 `providerReadinessFromCapability` / `providerStartupReason` / `resolveProviderStatuses` 复杂层 | doctor 需要基本 provider 状态,但不需要分层 readiness 聚合 |
| `memorymodule/mutation_plan` + `mutation_apply` | **保留** | `CreateFact` 内部走 `ApplyMemoryMutation`(atomic write + BuildIndex + semantic rebuild + rollback),是 memory 写入核心管道,非冗余 |
| 移动端技术栈 | Kotlin + Jetpack Compose | 用户选择 |
| 移动端 OpenAPI 生成 | openapi-generator-cli `-g kotlin` | 成熟、支持 oneOf sealed class |
| 移动端 SSE | OkHttp EventSource 手写 | OpenAPI 不建模 SSE transport |
| 移动端存储 | EncryptedSharedPreferences | 替代 flutter_secure_storage |
| 移动端 QR | Google ML Kit Barcode Scanning | 替代 mobile_scanner |
| 移动端状态 | Flow + StateFlow + ViewModel | 替代 Riverpod |
| 移动端 DI | Hilt | Android 官方推荐 |
| 旧 Flutter 代码库 | 并存开发,验证通过后硬删 | 不边改边破坏 |
| OpenAPI schema | 不动 | `/v1/*` + RunEvent schema 保持不变,后端零改动 |
| SQLite schema | 不动 | 已接近目标(~11 表含可选 memory_vectors) |

## 3. 目标架构

### 3.1 后端 Phase A 清理后

```text
internal/
  events/
    events.go              # RunStatus + EventKind + PendingActionKind/Status + records,无 OrchestrationMode
  app/
    client_service.go      # 无 parseClientRunMode,mode 字段固定
    capability_service.go  # 保留 Snapshot,简化 readiness 层
    capability_service_snapshot.go  # 简化 MCP provider 探测
  cli/
    smoke.go               # 无 --mode flag
    cli.go                 # help 文本无 plan_execute
  runtime/
    run.go                 # 无 assembleRunnerByMode,直接调 newDirectResponseRunner
    runner.go              # 无 mode 参数
    api/api.go             # 无 OrchestrationMode 字段
  contextplane/
    (删 compression_token_counter.go)
  skills/
    (删 lifecycle_tools.go + writer_lifecycle.go 中 lifecycle 部分 + health.go 中 RoutingFixture 复杂部分 + model.go 中 lifecycle/assessment 类型)
    # 保留:loader.go + scan.go + markdown.go + writer.go(基础) + recommendation.go + eligibility.go + problems.go + model.go(基础) + health.go(简化) + tools.go
```

### 3.2 移动端 Kotlin 结构

```text
mobile-kotlin/
  app/
    build.gradle.kts
    src/main/
      AndroidManifest.xml
      java/io/ycvk/acorn/
        AcornApp.kt                        # Application + Hilt
        MainActivity.kt
        core/
          api/                             # openapi-generator 生成
          auth/
            AuthController.kt              # pairing + token storage
            SecureStore.kt                 # EncryptedSharedPreferences
          sse/
            RunEventStreamClient.kt        # OkHttp EventSource,独立测试
            RunEventProjection.kt           # SSE → UI state
          state/
            ConnectionState.kt             # Flow<ConnectionState>
          theme/
            AcornTheme.kt                  # Material 3
            Color.kt
            Type.kt
        data/
          repository/
            ChatRepository.kt              # 消费 generated client
            ThreadsRepository.kt
            InboxRepository.kt
            ApprovalsRepository.kt
            RunsRepository.kt
            SettingsRepository.kt
            MemoryRepository.kt
        feature/
          chat/
            ChatViewModel.kt
            ChatScreen.kt
            widgets/                        # chat 专属 widget
          threads/
            ThreadsViewModel.kt
            ThreadsScreen.kt
            widgets/
          inbox/
            InboxViewModel.kt
            InboxScreen.kt
          approvals/
            ApprovalsViewModel.kt
            ApprovalsScreen.kt
          runs/
            RunDetailViewModel.kt
            RunDetailScreen.kt
          settings/
            SettingsViewModel.kt
            SettingsScreen.kt
          pairing/
            PairingViewModel.kt
            PairingScreen.kt
            QrScannerScreen.kt
  tool/
    generate_openapi_client.sh             # 替代 Python 脚本
  build.gradle.kts                          # project-level
  settings.gradle.kts
```

### 3.3 OpenAPI 生成链路

```text
docs/openapi.yaml (OpenAPI 3.1.0, 不动)
  ↓
openapi-generator-cli -g kotlin -o mobile-kotlin/app/src/main/java/io/ycvk/acorn/core/api/
  ↓
generated Kotlin client (Retrofit + OkHttp + Moshi/Kotlinx Serialization)
  ↓
data/repository/ (消费 generated client)
  ↓
feature/ (ViewModel 消费 repository)
```

SSE 不走 generated client(OpenAPI 不建模 SSE transport),手写 OkHttp EventSource 消费 RunEvent live subset:
- `assistant.delta` → append delta + reasoning
- `agent.message` / `run.completed` → finalize
- `run.failed` / `run.interrupted` → finalize with status
- `run.resume_requested` / `elicitation.pending` / `operator_question.pending` / `decision_blocked` → activity row

## 4. Phase A:后端残留清理

### 4.1 mode 路由删除

**删除:**
- `internal/events/events.go`:`OrchestrationMode` 类型 + `ModeDirectResponse` 常量 + `Normalize()` 方法(L17-25)
- `internal/app/client_helpers.go`:`ErrClientInvalidRunMode` sentinel(L15)
- `internal/app/client_service.go`:`parseClientRunMode` 函数(L160-169) + `OrchestrationMode` 字段传递(创建 run 时固定 direct_response,不再从请求读 mode)
- `internal/app/run_once.go`:`OrchestrationMode` 字段(L47)
- `internal/cli/smoke.go`:`--mode` flag(L22) + `displayMode` 逻辑
- `internal/cli/cli.go`:help 文本 `plan_execute` 字样(L67)
- `internal/runtime/runner.go`:`mode` 参数(L233) + `assembleRunnerByMode` 包装层(直接调 `newDirectResponseRunner`)
- `internal/runtime/run.go`:`assembleRunnerByMode` 函数(L155)
- `internal/runtime/api/api.go`:`OrchestrationMode` 字段(L55)
- `internal/web/handler_helpers.go`:`ErrClientInvalidRunMode` 错误分支(L24)
- `internal/app/client_service_test.go`:invalid mode 测试用例(L801-805,断言不再适用)

**确认已删(无需动作):**
- `internal/store/sqlite/store_run.go`:无 `orchestration_mode` 列(`store_schema.go` L37-39 注释确认已 retired,新 schema 不创建)
- `internal/web/dto_run.go`:无 `mode` 字段

**OpenAPI 影响:**`create-run` request 的 `mode` 字段。后端固定 `direct_response`,API 层接受空值或 `direct_response`(向后兼容已有 mobile client,直到 Kotlin 版本上线)。

### 4.2 死文件删除

- `internal/contextplane/compression_token_counter.go`(零引用)
- `.artifacts/faiss-native/`(13MB 死目录)

### 4.3 skill lifecycle/assess 简化

**删除:**
- `internal/skills/lifecycle_tools.go`(218 行):`BuildSkillLifecycleTools` + `assessSkill` + `AssessToolInput/Output` + `lifecycleStatusForAssessment` + `emitAssessmentLifecycleEvent` + `skillMutableSource`
- `internal/skills/writer_lifecycle.go` 中 lifecycle 部分:`UpdateSkillLifecycle` + `validateLifecycleEvidence` + `applyLifecycleUpdate` + `LifecycleUpdate` 类型
- `internal/skills/health.go` 中复杂部分:`RoutingFixture` + `RoutingFixtureResult` + `runRoutingFixtures` + `runRoutingFixture` + `hasRoutingMetadata` + `hasWeakRoutingMetadata` + `duplicateTriggerKeys`
- `internal/skills/model.go` 中 lifecycle/assessment 类型:`LifecycleStatus`(5 态) + `AssessmentVerdict`(3 态) + `SkillAssessment` + `LifecycleEvent` + `LifecycleUpdate` + `EvidenceRefs` 相关字段
- `internal/runtime/runner_toolset.go` 中 `BuildSkillLifecycleTools` 调用
- `internal/app/skill_service.go` 中 `BuildHealthReport` + `RoutingFixture` 调用

**保留:**
- `internal/skills/loader.go`:read-only `ScanSkills` + `findSkillByID`
- `internal/skills/scan.go`:文件扫描
- `internal/skills/markdown.go`:frontmatter 解析
- `internal/skills/writer.go` + `writer_lifecycle.go` 中 `ReadSkillFile` / `WriteSkillFile` / `normalizeCreateInput` / `buildNormalizedCreateInput` / `applyCreateInputDefaults`:skill 文件 CRUD(不带 lifecycle)
- `internal/skills/recommendation.go`:`Recommend` + `ActivateExplicit`(简单关键词匹配)
- `internal/skills/eligibility.go`:`Evaluate`
- `internal/skills/problems.go`:loader problem
- `internal/skills/health.go` 简化版:`BuildHealthReport` 只检查 loader problem + eligibility(删 routing fixture)
- `internal/skills/model.go` 基础:`Spec` + `Source` + `Origin` + `View`
- `internal/skills/tools.go`:`BuildSkillTools`(如有 read-only skill tools)

**skill frontmatter 简化:**
- 删除 `lifecycle_status` / `evidence_refs` / `replaced_by` / `updated_by_run_id` 字段
- 保留 `id` / `name` / `summary` / `tags` / `trigger_hints` / `task_pattern` / `instruction` / `source` / `created` / `updated`

### 4.4 capability_service 简化

**保留:**
- `CapabilitiesService.Snapshot`:聚合 model / tools / skills / MCP providers / runtime readiness
- `snapshotTools` + `snapshotSkills` + `snapshotMCPProviders` 基本聚合
- `RuntimeReadiness` + `ProviderReadinessSummary`(简化为 startup status + auth status)
- `configuredProviderConfigs`(`capability_service_snapshot.go:251`):`snapshotMCPProviders` 的必要辅助,保留

**删除:**
- `providerReadinessFromCapability`(83 行复杂派生)
- `providerStartupReason`
- `resolveProviderStatuses` 复杂层
- `mcpProviderParallelPolicy`

### 4.5 验收

- `make test` 通过(更新测试)
- `make lint` + `make format-check` 通过
- `make test-architecture` 通过(更新守卫)
- OpenAPI `--check` 通过(schema 不动)
- `acorn doctor` 仍输出有效 snapshot
- `acorn smoke` 仍能跑 run
- `skill_assess` tool 不再出现在 tool catalog
- skill health report 只报 loader problem + eligibility,不报 routing fixture

## 5. Phase B:Kotlin 移动端迁移

### 5.1 并存策略

- `mobile/`(Flutter)与 `mobile-kotlin/`(Kotlin)并存开发
- Kotlin 版本验证通过 + CI 跑通后,删 `mobile/` + `mobile/.dart_tool/` + `mobile/build/`(释放 ~4.6GB)
- 期间后端 `/v1` API 契约不变,两个移动端都能连后端

### 5.2 B1:骨架 + 生成链路

- 建 `mobile-kotlin/` Gradle 项目(minSdk 26,targetSdk 34)
- 配置 `openapi-generator-cli` 生成 Kotlin client(Retrofit + OkHttp + Moshi)
- 配置 Hilt DI
- 配置 Material 3 主题
- `tool/generate_openapi_client.sh` 替代 `mobile/tool/generate_openapi_client.py`
- 验证:generated client 能编译,`--check` 等效门禁可行

### 5.3 B2:SSE + pairing + device auth(vertical slice)

- `core/sse/RunEventStreamClient.kt`:OkHttp EventSource,消费 RunEvent live subset
- `core/sse/RunEventProjection.kt`:SSE event → UI state(assistant delta / message / run status / resume / approval)
- `core/auth/AuthController.kt`:pairing + token storage
- `core/auth/SecureStore.kt`:EncryptedSharedPreferences
- `feature/pairing/PairingScreen.kt` + `QrScannerScreen.kt`:ML Kit QR
- 验证:pair → 后端发 token → 存 token → 连 SSE → 收到 assistant.delta

### 5.4 B3:chat feature

- `data/repository/ChatRepository.kt`:create thread / send message / start run / get events
- `feature/chat/ChatViewModel.kt`:StateFlow<ChatState>,消费 SSE projection
- `feature/chat/ChatScreen.kt`:Compose UI,assistant bubble + user message + composer
- `feature/chat/widgets/`:AssistantMarkdown(Markdown 渲染)、TypingIndicator、ActivityRow
- 验证:发消息 → run 启动 → SSE streaming → assistant bubble 实时更新 → run 完成

### 5.5 B4:其余 features

- `threads/`:list / create / delete / open
- `inbox/`:`/v1/inbox` 聚合
- `approvals/`:pending-actions list + detail + decide
- `runs/`:run detail
- `settings/`:server / device / model / disconnect
- 验证:每个 feature 独立可测,不互相 rebuild

### 5.6 B5:CI + release

- `.github/workflows/ci.yml`:`mobile-android` job 从 `flutter-action` 换成 `gradle build`
- `scripts/build-release.sh`:Android APK 从 `flutter build apk` 改为 `gradle assembleRelease`(或在单独 workflow)
- `tool/generate_openapi_client.sh --check` 替代 Python `--check` 门禁
- 验证:CI 绿,APK 可构建

### 5.7 验收

- pair → inbox → chat → approvals → settings 全流程可用
- SSE streaming:assistant delta 实时投影
- device auth:pairing code → bearer token → revoked
- Resume:中断后恢复 run
- Markdown 渲染:code block / link / 长消息
- QR 扫码:pairing payload 解析
- Kotlin client 与 OpenAPI schema 一致(`--check` 通过)
- CI 全绿
- APK 可安装运行

## 6. Phase C:收尾

- 删 `mobile/` 全目录(释放 ~4.6GB)
- 更新 `docs/architecture/mobile-control-surface.md`:Flutter → Kotlin
- 更新 `README.md` + `AGENTS.md`:mobile 段落
- 更新 `docs/aegis/INDEX.md`
- ADR-0007:移动端从 Flutter 迁移到 Kotlin + Jetpack Compose
- ADR-0008:删 mode 路由壳 + skill lifecycle/assess 企业级机制

## 7. 非目标

- 不改 OpenAPI schema(`/v1/*` + RunEvent 不动)
- 不改 SQLite schema
- 不改后端 tool 实现(workspace / command / web / mcp-client / memory)
- 不改后端 runtime 编排(direct_response 保留)
- 不改后端 context 压缩(hybrid masking + auto-compact 保留)
- 不写数据迁移工具
- 不做 iOS(只 Android,与当前 Flutter 发布范围一致)

## 8. 风险

| 风险 | 缓解 |
|---|---|
| OpenAPI `oneOf` RunEvent 在 Kotlin generator 的 sealed class 序列化 | B1 阶段先验证生成 + 单元测试 |
| SSE 在 Android 后台稳定性 | 前台 follow surface only(同 Flutter 当前策略) |
| ML Kit QR 扫码体积 | ML Kit barcode-scanning ~2MB,可接受 |
| skill lifecycle 删除影响 doctor 输出 | doctor 改为只报 loader problem + eligibility,不报 routing fixture |
| `mode` 字段移除影响旧 Flutter client | 后端接受空值或 `direct_response`(向后兼容),Kotlin 版本上线后再考虑彻底移除字段 |
| 并存期间仓库体积 | `.gitignore` 加 `mobile-kotlin/build/`,Flutter `mobile/build/` 已忽略 |

## 9. ADR 信号

本 spec 触发:
- ADR-0007:移动端 Flutter → Kotlin + Jetpack Compose
- ADR-0008:删 mode 路由壳 + OrchestrationMode 类型
- ADR-0009:删 skill lifecycle/assess 企业级机制

## 10. 实现策略

- **hard cutover**:Phase A 删死壳,不保留兼容路径;Phase B 并存后硬删 Flutter
- **Phase A 先做**:0.5-1 天,独立可验证
- **Phase B subagent-driven**:B1-B5 可并行(vertical slice 后各 feature 独立)
- **writing-plans 作为下一步**:spec 通过后写实现计划
