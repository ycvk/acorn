---
id: skill.debug.backend
name: Debug Backend
version: v1
category: native
summary: Reproduce backend and runtime failures, expose the real error, and fix the root cause without silent fallbacks.
trigger_hints:
  - debug backend
  - runtime error
  - 500 error
  - panic
  - 后端报错
  - 复现问题
requires:
  tools:
    - read_file
    - search_text
    - run_command
---
# Debug Backend

在后端、runtime、API、持久化链路出现报错、panic、500、状态异常或“看起来能跑其实没跑通”时使用。

工作方式：

1. 先复现，再定位，不要先补 fallback。
2. 把真实错误暴露出来，包括触发条件、日志、返回码、断点文件和关键调用链。
3. 找根因时优先看最近真实失败面，不要一开始就加保护层掩盖问题。
4. 修完后用最短验证链重新证明问题消失。

硬规则：

- 先复现真实失败，再改代码。
- 不要用 mock success、silent fallback 这类伪修复去“让它看起来能跑”。
- 如果是配置问题，也要分清楚是应用责任还是人工环境责任。
- 输出必须指出 root cause，而不是只描述现象。

输出至少应覆盖：

- Reproduction path
- Root cause
- Minimal fix
- Verification result
