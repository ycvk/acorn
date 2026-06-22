# Acorn 重构 - Phase 6: Flutter 重写 + Release 简化

Date: 2026-06-21
Parent Spec: `docs/aegis/specs/2026-06-21-acorn-refactor-design.md`
Depends on: Phase 1-5

## Goal

Flutter app 彻底重写为模块化结构(feature 独立 + Riverpod + 独立 SSE 模块)。简化 release 流程(去掉 FAISS/CGO,纯 Go build)。

## Architecture

```text
本阶段范围:
  mobile/lib/     — 彻底重写
  scripts/        — 简化 build-release.sh
  deploy/          — 删除 faiss.version
  Makefile         — 删除 faiss 相关 target
```

## Baseline / Authority Refs

- Spec §3.11 Flutter 重写、§3.12 Release 简化
- `docs/architecture/mobile-control-surface.md` — 当前 mobile 架构
- `mobile/lib/src/api/acorn_api.dart` — generated client

## Compatibility Boundary

- `docs/openapi.yaml` 是唯一 wire contract(Phase 5 已精简)
- `generate_openapi_client.py --check` 仍是 CI 门禁
- SSE 仍手写
- Android APK 仍走 Flutter build

## Verification

- `cd mobile && flutter test && flutter analyze && flutter build apk --debug` 通过
- `python3 mobile/tool/generate_openapi_client.py --check` 通过
- `make build` 通过(纯 Go,无 CGO)

---

## Task 1: 清理旧 mobile/lib 目录

**Files:**
- Delete: `mobile/lib/src/` 下所有旧文件(保留 `main.dart` 骨架)

**Why:** 彻底重写,旧代码全删。

### Steps

- [ ] **1.1 备份旧代码**(可选):`cp -r mobile/lib /tmp/mobile_lib_old`
- [ ] **1.2 删除 `mobile/lib/src/` 下所有文件**:`rm -rf mobile/lib/src/*`
- [ ] **1.3 保留 `mobile/pubspec.yaml`**(更新依赖)
- [ ] **1.4 Commit**:`chore(mobile): clear old lib for rewrite`

---

## Task 2: 重新生成 API client

**Files:**
- Run: `python3 mobile/tool/generate_openapi_client.py`

**Why:** 基于精简后的 OpenAPI 生成新 Dart client。

### Steps

- [ ] **2.1 运行**:`python3 mobile/tool/generate_openapi_client.py`
- [ ] **2.2 验证**:`python3 mobile/tool/generate_openapi_client.py --check`。必须通过。
- [ ] **2.3 确认生成的文件**:`mobile/lib/src/api/acorn_api.dart` 存在且不含 plan/skill-lifecycle/procedure 类型。
- [ ] **2.4 Commit**:`chore(mobile): regenerate API client from simplified openapi`

---

## Task 3: 搭建 core 层

**Files:**
- Create: `mobile/lib/app.dart`
- Create: `mobile/lib/src/core/api/sse_client.dart`
- Create: `mobile/lib/src/core/auth/secure_store.dart`
- Create: `mobile/lib/src/core/auth/auth_controller.dart`
- Create: `mobile/lib/src/core/state/app_providers.dart`
- Create: `mobile/lib/src/core/connection/connection_controller.dart`
- Create: `mobile/lib/src/core/connection/connection_profile.dart`
- Create: `mobile/lib/src/core/connection/connection_store.dart`
- Create: `mobile/lib/src/core/theme/acorn_theme.dart`

**Why:** core 层是所有 feature 的共享基础。

**Verification:** `cd mobile && flutter analyze` 通过

### Steps

- [ ] **3.1 写 `app.dart`**:MaterialApp + 路由 + ProviderScope。
- [ ] **3.2 写 `sse_client.dart`**:独立 SSE 模块。消费 `/v1/runs/{id}/events?after_seq=0&follow=true`。解析 SSE envelope(id/event/data)。投影 RunEvent live subset。错误处理:malformed JSON / SSE id mismatch / unsupported event type → visible error。独立测试。
- [ ] **3.3 写 `secure_store.dart`**:`flutter_secure_storage` 封装。`store`/`read`/`delete` connection profile。
- [ ] **3.4 写 `auth_controller.dart`**:pairing + token storage。调用 `AcornApiClient` pair endpoint。成功后存 connection profile。
- [ ] **3.5 写 `app_providers.dart`**:Riverpod global providers:connectionProvider、apiClientProvider、sseClientProvider。
- [ ] **3.6 写 `connection_controller.dart`**:管理 active API client + SSE client。pair / disconnect / dispose。
- [ ] **3.7 写 `connection_profile.dart` + `connection_store.dart`**:connection profile 数据类 + storage 抽象。
- [ ] **3.8 写 `acorn_theme.dart`**:FlexColorScheme + 状态色 token。
- [ ] **3.9 Commit**:`feat(mobile): add core layer - api/auth/state/theme`

---

## Task 4: 搭建 shared widgets

**Files:**
- Create: `mobile/lib/src/ui/widgets/status_pill.dart`
- Create: `mobile/lib/src/ui/widgets/empty_state.dart`
- Create: `mobile/lib/src/ui/widgets/message_widgets.dart`
- Create: `mobile/lib/src/ui/widgets/section_header.dart`
- Create: `mobile/lib/src/ui/widgets/list_rows.dart`
- Create: `mobile/lib/src/ui/widgets/acorn_surfaces.dart`

**Why:** 共享 widget 给所有 feature 使用。

### Steps

- [ ] **4.1 写 `status_pill.dart`**:StatusDot / StatusPill / InlineStatusLabel。
- [ ] **4.2 写 `empty_state.dart`**:共享空状态 widget。
- [ ] **4.3 写 `message_widgets.dart`**:chat bubble / activity row / run status footer / typing indicator。Markdown 渲用 `flutter_markdown_plus`。
- [ ] **4.4 写 `section_header.dart` / `list_rows.dart` / `acorn_surfaces.dart`**:从旧代码迁移,简化。
- [ ] **4.5 Commit**:`feat(mobile): add shared ui widgets`

---

## Task 5: 实现 chat feature

**Files:**
- Create: `mobile/lib/src/features/chat/chat_controller.dart`
- Create: `mobile/lib/src/features/chat/chat_repository.dart`
- Create: `mobile/lib/src/features/chat/chat_screen.dart`
- Create: `mobile/lib/src/features/chat/chat_models.dart`
- Create: `mobile/lib/src/features/chat/widgets/assistant_markdown.dart`

**Why:** chat 是核心 feature。通过 SSE streaming 实时投影 assistant delta。

**Verification:** `cd mobile && flutter test` 通过

### Steps

- [ ] **5.1 写 `chat_repository.dart`**:调用 `AcornApiClient`:list messages / create run / get run detail。
- [ ] **5.2 写 `chat_models.dart`**:ChatMessage / ChatState 数据类。
- [ ] **5.3 写 `chat_controller.dart`**:Riverpod StateNotifier。管理 message list + run state。启动 SSE streaming。投影 assistant.delta / agent.message / run.completed / run.failed / run.interrupted / resume_requested / elicitation.pending / operator_question.pending / decision_blocked。streaming delta 只在本 controller 内,不触发其他 feature rebuild。
- [ ] **5.4 写 `chat_screen.dart`**:thread detail surface。backend message send / run start / live assistant streaming / Markdown rendering / activity rows。使用 shared widgets。
- [ ] **5.5 写 `assistant_markdown.dart`**:GitHub-flavored Markdown 渲染。code block copy action。长消息 bounded viewport。http/https 链接 url_launcher。
- [ ] **5.6 写 chat feature 测试**。
- [ ] **5.7 Commit**:`feat(mobile): implement chat feature with SSE streaming`

---

## Task 6: 实现 inbox + approvals + settings + threads features

**Files:**
- Create: `mobile/lib/src/features/inbox/{inbox_controller,inbox_repository,inbox_screen}.dart`
- Create: `mobile/lib/src/features/approvals/{approvals_controller,approvals_repository,approvals_screen}.dart`
- Create: `mobile/lib/src/features/settings/{settings_controller,settings_repository,settings_screen}.dart`
- Create: `mobile/lib/src/features/threads/{threads_controller,threads_repository,threads_screen}.dart`
- Create: `mobile/lib/src/features/pairing/pairing_screen.dart`
- Create: `mobile/lib/src/features/shell/acorn_shell.dart`

**Why:** 其余 feature。每个 feature 自包含:controller + repository + screen。

**Verification:** `cd mobile && flutter test && flutter analyze` 通过

### Steps

- [ ] **6.1 写 inbox feature**:`InboxController` StateNotifier。`InboxRepository` 调用 `GET /v1/inbox`。`InboxScreen` 展示 pending actions + active/recent runs + system status。使用 shared widgets。
- [ ] **6.2 写 approvals feature**:`ApprovalsController` StateNotifier。`ApprovalsRepository` 调用 `GET /v1/pending-actions` + `GET /v1/pending-actions/{id}` + `POST /v1/pending-actions/{id}:decide`。`ApprovalsScreen` 展示 approval detail + accept/decline。
- [ ] **6.3 写 settings feature**:`SettingsController` StateNotifier。`SettingsRepository` 调用 `GET /v1/system/status` + `GET /v1/devices` + `DELETE /v1/devices/{id}`。`SettingsScreen` 展示 server info + device info + disconnect。
- [ ] **6.4 写 threads feature**:`ThreadsController` StateNotifier。`ThreadsRepository` 调用 `GET /v1/threads` + `POST /v1/threads` + `DELETE /v1/threads/{id}`。`ThreadsScreen` 展示 thread list + inbox priority cards。
- [ ] **6.5 写 pairing screen**:scan QR 或手动输入 server URL + pairing code。调用 pair endpoint。
- [ ] **6.6 写 shell**:底部导航 Threads / Approvals / Settings。Chat 从 thread push。
- [ ] **6.7 写各 feature 测试**。
- [ ] **6.8 Commit**:`feat(mobile): implement inbox/approvals/settings/threads/pairing/shell features`

---

## Task 7: 简化 release 流程

**Files:**
- Modify: `scripts/build-release.sh`
- Delete: `scripts/build-faiss-artifacts.sh`
- Delete: `scripts/run-with-faiss-artifacts.sh`
- Delete: `deploy/faiss.version`
- Modify: `Makefile`
- Modify: `scripts/install-release.sh`

**Why:** 去掉 FAISS/CGO,release 变为纯 Go build。

**Verification:** `make build` 通过

### Steps

- [ ] **7.1 重写 `scripts/build-release.sh`**:纯 Go 交叉编译:
  ```bash
  GOOS=linux GOARCH=arm64 go build -o acorn-linux-arm64 ./cmd/acorn
  GOOS=linux GOARCH=amd64 go build -o acorn-linux-amd64 ./cmd/acorn
  ```
  删除 CGO_ENABLED=1、build tags、FAISS artifact 下载。
- [ ] **7.2 删除 `scripts/build-faiss-artifacts.sh` / `scripts/run-with-faiss-artifacts.sh` / `deploy/faiss.version`**。
- [ ] **7.3 重写 `Makefile`**:删除 `dev-faiss-artifacts` / `dev-build-faiss` / `dev-serve-faiss` target。`build` target 改为纯 `go build`。删除 build tags。
- [ ] **7.4 重写 `scripts/install-release.sh`**:删除 FAISS lib 安装。安装 binary + skills seed pack + systemd unit。
- [ ] **7.5 运行验证**:`make build`。必须通过且无 CGO。
- [ ] **7.6 运行验证**:`CGO_ENABLED=0 go build -o /tmp/acorn-test ./cmd/acorn`。必须通过。
- [ ] **7.7 Commit**:`refactor(release): simplify to pure Go build, remove FAISS/CGO`

---

## Task 8: 全量 E2E 验证

### Steps

- [ ] **8.1 后端 E2E**:`make build && make serve`(本地)。用 CLI 发任务:`echo "hello" | acorn run`。确认 direct_response 模式工作。
- [ ] **8.2 工具执行测试**:发任务需要工具调用的(如 `read file /tmp/test.txt`)。确认工具执行正常。
- [ ] **8.3 记忆测试**:通过 `remember` 工具写 fact。通过 `memory_search` 检索。确认 embedding + SQLite 工作。
- [ ] **8.4 中断+审批测试**:发任务触发 `operator_question`。通过 `/v1/pending-actions` 查看。通过 `:decide` 决定。确认 resume。
- [ ] **8.5 Mobile E2E**:`cd mobile && flutter build apk --debug`。安装到设备/模拟器。pair → inbox → chat → approvals → settings。确认 SSE streaming。
- [ ] **8.6 Context masking 测试**:构造长会话(多轮工具调用)。确认旧 tool result 被 mask。
- [ ] **8.7 Auto-compact 测试**:构造超 context limit 的长会话。确认 auto-compact 触发 + summary 生成 + circuit breaker。
- [ ] **8.8 最终全量验证**:`make test && make lint && make format-check && make test-architecture`。全部通过。
- [ ] **8.9 Commit**:`chore: phase 6 flutter rewrite + release simplification complete - E2E verified`
