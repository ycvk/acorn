# Harness Fixtures

本目录存放用于 bootstrap verification 的 replay fixtures。

## 文件格式

每个 fixture 是一个 Markdown 文件，命名格式：
`{pattern-id}_{YYYY-MM-DD}.md`

```markdown
---
type: fixture
pattern_id: pattern_openapi_mobile_sync
date: 2026-05-20
source_run: {run_id}
---

# Fixture: pattern_openapi_mobile_sync

## 原始输入
用户请求：修改 inbox API 响应字段

## 原始执行路径
1. 修改 `internal/web/inbox.go`
2. 未检查 `docs/openapi.yaml`
3. 未重新生成 mobile client
4. 测试通过

## 期望正确行为
1. 修改 `internal/web/inbox.go`
2. 同步更新 `docs/openapi.yaml`
3. 重新生成 `mobile/lib/src/api/acorn_api.dart`
4. 运行 `flutter analyze`

## 验证要点
- [ ] 能检测到 API handler 变更
- [ ] 能提示同步 openapi.yaml
- [ ] 能提示重新生成 mobile client
```

## 创建方式

1. reflexion-extract 识别到可复现的错误模式
2. 人工或 meta-review 将模式标记为"需要 fixture"
3. 从原始 run 中提取输入/输出，创建 fixture 文件
4. fixture 作为 bootstrap-verify 的回归测试素材