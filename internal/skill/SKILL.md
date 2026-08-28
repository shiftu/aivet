---
name: aivet
description: 用 aivet 体检 / 修复本机的 AI coding 工具配置（Claude Code、Codex、Hermes、pi、DeepSeek Harness、cc-switch）。当用户说「工具连不上」「模型 404」「key 不对」「帮我配好 codex」「检查一下我的 AI 环境」时使用。
---

# aivet — 给套着缰绳的 AI 看病

aivet 是一个单文件命令行工具，负责**确定性**的诊断和**安全**的自动修复；
你（agent）负责它修不了的部分：装软件、改 shell 配置、解释根因。

## 先问它自己

不确定某个命令怎么用、有哪些选项、输出是什么结构，**不要猜**——直接问：

```
aivet help --json      # 所有命令 + 选项 + 退出码 + 报告结构，一次拿全
aivet help <命令>       # 单个命令的详细用法（人看的排版）
```

下面是常用路径，细节以 `aivet help --json` 为准。

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
     （aivet 会拿实际版本号比：旧版报提醒，新版才报故障）
   - Claude Code 的 `ANTHROPIC_BASE_URL` 不要以 `/v1` 结尾

4. **看到 id 以 `.schema` 结尾的项就停一下**（标题「配置结构」）：
   这表示那件工具改了配置格式，而 aivet 的知识停在旧版 —— **它下面那些检查等于没查**，
   不管显示的是绿是红都不作数。别照着那些结果下结论，改走两条路：
   ```
   aivet check <工具> --live      # 让工具自己跑一次，完全不依赖 aivet 读得懂配置
   aivet knowledge                # 看 aivet 现在以为配置文件在哪、字段叫什么
   ```
   如果你能看出新格式长什么样（读一眼那个配置文件即可），把差异告诉用户，
   并可以往 `~/.aivet/knowledge.json` 补一条（`aivet knowledge --init` 生成模板）——
   补完立刻生效，不用等 aivet 发新版。同理，工具搬了配置文件的家，
   往 `paths` 对应键的最前面加一条新路径就能让 aivet 重新认得出来。

5. 复验直到没有 fail：
   ```
   aivet check --json
   ```
   要真的把每件工具跑一次（慢，约每件 10–60 秒）：`aivet check --live`。

6. 从零配一台机器：
   ```
   aivet setup --gateway <url> --key <key> --model <model> --yes
   ```
   它会验证网关后，把 Claude Code / Codex / Hermes / pi / dsh 的原生配置一次写好（已有配置不覆盖，加 `--force` 才覆盖）。

## 规矩

- 报告里的 key 已脱敏；**不要**把完整 key 打印到对话或日志里。
- 改任何用户配置前先备份（aivet 自己会做；你手改的也要）。
- 装软件前先告诉用户要装什么、用什么命令。`aivet env` 会给出当前系统的安装命令。
- aivet 对这些工具的了解会随它们升级而过时。**它读不懂的东西会明说**（`.schema` 项、
  「配置结构」标题、汇总行里的「结论不作数」）—— 见到这些就别再拿那部分结果当事实。
  相反，`--live` 的结论任何时候都作数：那是工具自己跑出来的，不经过 aivet 的理解。
