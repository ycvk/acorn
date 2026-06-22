# ADR-0007: 移动端 Flutter → Kotlin + Jetpack Compose

Date: 2026-06-22
Status: Accepted
Supersedes: (none)

## Context

Acorn 移动端原为 Flutter（`mobile/`）。后端是 Go 1.26 静态类型，Flutter/Dart 的动态性与后端开发风格不一致。Flutter 构建链重（Gradle + Flutter SDK + CMake + NDK），`.dart_tool/`、`build/`、`.gradle/` 目录体积大（数百 MB 构建缓存），容易误提交到 git。

OpenAPI 生成链路是 Acorn 的硬约束：`docs/openapi.yaml` 是 wire contract，客户端代码由它生成。Flutter 侧用 `python3 mobile/tool/generate_openapi_client.py` 生成 Dart client，依赖 `dart_openapi_codegen`，维护成本高。

## Decision

移动端迁移到 Kotlin + Jetpack Compose：

- **语言**：Kotlin，与 Go 后端的静态类型偏好一致
- **UI**：Jetpack Compose + Material 3
- **DI**：Hilt
- **网络**：OkHttp（SSE 手写 `EventSource`，OpenAPI 不建模 SSE transport）
- **序列化**：Moshi + 自定义 adapter（OpenAPI oneOf 生成的 interface 需手动 discriminated union）
- **OpenAPI 生成**：`openapi-generator-cli -g kotlin` 替代 Python 脚本，`tool/generate_openapi_client.sh` 带 `--check` 模式
- **认证存储**：EncryptedSharedPreferences
- **Markdown 渲染**：`compose-markdown`（JitPack `com.github.jeziellago:compose-markdown:0.5.7`）
- **Java 版本**：Java 21（Java 26 会 crash Gradle Kotlin DSL compiler）

迁移策略：Kotlin 项目（`mobile-kotlin/`）与 Flutter（`mobile/`）并存开发，真机验证通过后硬删 Flutter。旧 `mobile/` 目录及整个 git 历史已用 `git filter-repo` 清除。

## Consequences

- **正面**：
  - 类型系统与后端一致，OpenAPI 生成链路成熟（`-g kotlin` 官方支持 oneOf）
  - 构建链更轻（无 CMake/NDK/CMakeLists，纯 Kotlin + Gradle）
  - 性能更好（原生 ART，无 Flutter JS bridge）
  - 真机验证通过：pairing、SSE streaming、chat、settings、thread navigation、message history、tool calls 全部正常
- **负面**：
  - `ApiClient.accessToken` 是 companion-level 静态字段（全局状态），单连接场景可用，多连接需重构
  - OpenAPI oneOf 生成 interface 后 Moshi 无法直接反序列化，需自定义 `MessagePartAdapter` 和 `RunEventPacket` sealed class 按 `kind`/`type` 字段做 discriminated union
  - `compose-markdown` 通过 JitPack 引入，非 Maven Central，`settings.gradle.kts` 需加 JitPack repo
- **风险**：OpenAPI generator 的 oneOf 已知问题（生成 interface 但无反序列化支持）通过自定义 adapter 缓解；`ApiClient.accessToken` 全局状态在当前单用户场景下可接受

## Baseline Sync

- `mobile-kotlin/` 完整项目已落地（scaffold + OpenAPI client + SSE + chat + approvals + settings + run detail）
- `mobile/`（Flutter）已从磁盘和 git 历史中删除
- `docs/openapi.yaml` 未改动（`/v1/*` + RunEvent wire contract 保持不变）
- `docs/architecture/mobile-control-surface.md` 已更新
- `.github/workflows/ci.yml` + `release.yml` 已迁移到 Gradle（`./gradlew assembleDebug` + `actions/setup-java@v4` Java 21）
- 真机验证通过（device `951559ef`，app `io.ycvk.acorn`）
- APK: `mobile-kotlin/app/build/outputs/apk/debug/app-debug.apk`（42MB）
