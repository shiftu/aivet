# aivet

**给套着缰绳的 AI 看病。**

harness 是马具，vet 是兽医。aivet 一个命令检查本机所有 AI coding 工具的配置，
找出「为什么连不上 / 为什么 404 / 为什么 key 不对」，能修的自动修，修不了的交给一个还能用的 agent 去修。

装上了 ≠ 跑得通。aivet 每次都真的去打一下网关。

```
$ aivet

aivet v0.1.0  · macOS arm64
给套着缰绳的 AI 看病 · 配置 + 网关探测

✔ Claude Code  2.1.248
   ✔ settings.json          ~/.claude/settings.json
   ✔ 接入方式               网关 http://127.0.0.1:7421 · key lgw_xd****v7p2（settings.json env.ANTHROPIC_AUTH_TOKEN）
   ✔ 模型                   charaboard/deepseek-v4-flash
   ✔ 网关可达               http://127.0.0.1:7421 · 38 个模型
   ✔ 模型在清单里           charaboard/deepseek-v4-flash
   ✔ 真发一条请求           anthropic 通了（812ms）

✘ Codex CLI  codex-cli 0.149.1
   ✔ config.toml            ~/.codex/config.toml
   ✘ wire_api               wire_api = "chat"——codex 0.137 起已删除，启动即报错
     › 改成 "responses"（前提：网关有 /v1/responses）
     ⚒ 可自动修复：aivet fix codex.wire_api
   …
```

## 支持的工具

| 工具 | 查什么 | 能自动修什么 |
|---|---|---|
| **Claude Code** | settings.json 语法、接入方式（OAuth / key / 网关）、`ANTHROPIC_BASE_URL` 是否误带 `/v1`、模型别名（`sonnet`/`opus`/`haiku`）走网关时有没有映射、shell 与文件里的环境变量打架、网关三连 | 跳过首次向导、去掉多余的 `/v1` |
| **Codex CLI** | config.toml 语法、`model_provider` 与表名是否成对、`wire_api`（0.137+ 只认 responses）、key 来源（env_key / auth.json / ChatGPT 登录）、网关三连 | `wire_api` 改 responses |
| **Hermes Agent** | config.yaml（新旧两代 schema）、提供方、key（env / ~/.hermes/.env / 明文）、模型是否已声明、网关三连 | — |
| **pi agent** | settings.json + models.json、defaultModel 是否在提供方清单里、key、网关三连 | defaultModel 改成清单第一个 |
| **DeepSeek Harness (dsh)** | settings.yaml + .credentials.yaml、每个 profile 的模型覆盖、插件是否装好、Node 是否在、网关三连 | 默认模型改成清单第一个 |
| **cc-switch** | 它认为当前生效的 provider 和原生文件里的是否一致（漂移检测） | — |

每件工具还会先确认它**真的跑得起来**：npm 装的那几个（codex / pi / dsh）常出现 shim 在、平台专用包没装全，`--version` 直接报 ENOENT——这种「装了但是坏的」会排在最前面报出来，而不是让底下的配置项一路绿灯。

「网关三连」= 拉模型清单 → 模型在不在清单里 → 真发一条最小请求。六件工具指向同一个网关时只探一次。

有些结论**探不到底**，aivet 就明说探不到底，不假装知道：Claude Code 配 `model: "sonnet"` 时，真正发给网关的是它内部把别名解析出的模型 id，配置文件里看不见——这种情况报提醒并告诉你 `--live` 能一锤定音，而不是拿别名去和清单比对然后误判成故障。

## 安装

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/shiftu/aivet/main/install.sh | bash
```

**Windows（PowerShell）**

```powershell
irm https://raw.githubusercontent.com/shiftu/aivet/main/install.ps1 | iex
```

或者到 [Releases](https://github.com/shiftu/aivet/releases) 下载对应系统的单个可执行文件，放到 PATH 里。没有任何运行时依赖。

有 Go 的话：`go install github.com/shiftu/aivet/cmd/aivet@latest`

## 用法

```
aivet                          体检所有已安装的工具
aivet check codex --live       只查 codex，并真的让它跑一次
aivet check --json             给脚本 / agent 用的 JSON
aivet fix                      自动修复能修的（逐条确认；--yes 全修；--dry-run 只看）
aivet setup                    新手向导：网关地址 + key + 模型 → 所有工具一次配好
aivet ask                      把报告交给一个健康的 agent，让它修剩下的
aivet skill install            把 aivet 装成 Claude Code / Codex / Hermes / pi 的技能
aivet env                      系统信息 + 各工具的安装命令
```

退出码：`0` 没有故障，`1` 有故障，`2` 用法错误。**提醒（▲）不影响退出码**——能用就是能用。

### 新手：三分钟配好一台机器

你需要一个 OpenAI 兼容的网关地址和一把 key（公司发的、或自己搭的 llm-gateway、或 DeepSeek 官方 `https://api.deepseek.com/v1`）。

```bash
aivet setup
```

它会：问你地址和 key → 先验证能连上、列出可用模型让你选 → 用选好的模型真发一条请求 →
把 Claude Code、Codex、Hermes、pi、dsh 的原生配置文件**一次写好** → 提示你跑 `aivet check`。

- 已有的配置**不会被覆盖**（加 `--force` 才覆盖），原文件都留 `*.aivet-bak-<时间>` 备份。
- 非交互：`aivet setup --gateway URL --key KEY --model M --yes`（key 也可以放环境变量 `AIVET_KEY`）。
- 只配几件：`--tools claude,codex`。

### 挂进 agent 的工作流

aivet 负责**确定性**的诊断和**安全**的自动修复；装软件、改 shell 配置、看日志这类事交给会动手的 agent：

```bash
aivet ask                      # 自动挑一个体检通过的 agent，把报告塞给它
aivet ask --with claude        # 指定谁来修
aivet ask --print              # 只打印提示词，自己粘给任何 agent
```

反过来，让 agent 会用 aivet：

```bash
aivet skill install            # 写入 ~/.claude/skills/aivet、~/.codex/skills/aivet、~/.hermes/skills/aivet、~/.pi/agent/skills/aivet
```

之后在任何一个 agent 里说「用 aivet 检查一下我的 AI 环境」，它就知道该跑 `aivet check --json`、怎么读结果、什么时候跑 `aivet fix --yes`。

报告里的 key 一律脱敏，可以放心贴到群里或喂给 agent。

## 它不做什么

- 不替你保管 key，不做切换器——那是 cc-switch 的活；aivet 只检查 cc-switch 有没有和原生文件漂移。
- 不猜模型的上下文长度：`setup` 写的是保守默认值（128k / 16k），需要的话自己改。
- `--live` 之外不启动任何工具；默认体检只读文件 + 打网关，一两秒完事。

## 开发

```bash
go test ./...
make build          # 当前平台 → dist/
make release        # macOS/Linux/Windows × amd64/arm64 → dist/
```

### 发版

```bash
git tag -a v0.2.0 -m 'v0.2.0'
git push origin main v0.2.0
make release VERSION=v0.2.0                 # 六个平台 → dist/，并生成 SHA256SUMS
gh release create v0.2.0 dist/aivet_* dist/SHA256SUMS --title 'v0.2.0' --notes '变更说明'
```

`make release` 会先跑 `go vet` + `go test`，任一失败就不产出二进制。资产名必须保持 `aivet_<os>_<arch>[.exe]`——`install.sh` / `install.ps1` 靠这个命名去 Releases 里找文件。

结构：`internal/probe` 是无工具语义的探针（读文件、找可执行、打网关）；`internal/harness/<tool>` 各自实现
`Detect / Check / Fixers / Configure / LaunchArgs`；`cmd/aivet` 只做参数解析和排版。加一件新工具 = 加一个包 + 注册一行。

## 致谢

思路来自一个培训沙盒里的 `microclass-doctor`：它让我们学到「自检只查版本号，会把两件坏掉的工具一路绿灯发到学员手里」。
所以 aivet 每次都真的发一条请求。

MIT
