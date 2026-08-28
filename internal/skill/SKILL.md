---
name: aivet
description: 用 aivet 体检 / 修复本机的 AI coding 工具配置（Claude Code、Codex、Hermes、pi、DeepSeek Harness、cc-switch）。当用户说「工具连不上」「模型 404」「key 不对」「帮我配好 codex」「检查一下我的 AI 环境」时使用。
---

# aivet — 给套着缰绳的 AI 看病

aivet 是一个单文件命令行工具，负责**确定性**的诊断和**安全**的自动修复；
你（agent）负责它修不了的部分：装软件、改 shell 配置、解释根因。

## 工作流

1. 先体检，拿机器可读的结果：
   ```
   aivet check --json
   ```
   输出是 JSON：`tools[].checks[]`，每条有 `status`（ok/warn/fail/skip）、`detail`、`hint`、`fix`。
   只有 `fail` 需要处理；`warn` 是提醒；`skip` 多半是工具没装。

2. 带 `fix` 字段的项可以让 aivet 自己修（改前自动备份成 `*.aivet-bak-*`）：
   ```
   aivet fix --yes            # 修所有能修的
   aivet fix codex.wire_api   # 只修一条
   ```

3. 修不了的（没有 `fix` 字段）按 `hint` 处理。常见根因：
   - 网关连不上 → 地址错 / 本机网关没启动 / 需要代理
   - 401/403 → key 错或过期
   - 模型 404 → 模型名和网关别名不一致；`aivet check --json` 里 `gateway.model` 那条会列出相近的名字
   - codex `wire_api = "chat"` → 0.137+ 只认 `responses`，且网关必须有 `/v1/responses`
   - Claude Code 的 `ANTHROPIC_BASE_URL` 不要以 `/v1` 结尾

4. 复验直到没有 fail：
   ```
   aivet check --json
   ```
   要真的把每件工具跑一次（慢，约每件 10–60 秒）：`aivet check --live`。

5. 从零配一台机器：
   ```
   aivet setup --gateway <url> --key <key> --model <model> --yes
   ```
   它会验证网关后，把 Claude Code / Codex / Hermes / pi / dsh 的原生配置一次写好（已有配置不覆盖，加 `--force` 才覆盖）。

## 规矩

- 报告里的 key 已脱敏；**不要**把完整 key 打印到对话或日志里。
- 改任何用户配置前先备份（aivet 自己会做；你手改的也要）。
- 装软件前先告诉用户要装什么、用什么命令。`aivet env` 会给出当前系统的安装命令。
