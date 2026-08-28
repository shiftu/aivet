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
| **Claude Code** | settings.json 语法、接入方式（OAuth / key / 网关）、`ANTHROPIC_BASE_URL` 是否误带 `/v1`、模型别名（`sonnet`/`opus`/`haiku`）走网关时有没有映射、shell 与文件里的环境变量打架、网关三连、**三个别名各自指到的模型在不在清单里** | 跳过首次向导、去掉多余的 `/v1` |
| **Codex CLI** | config.toml 语法、`model_provider` 与表名是否成对、`wire_api`（0.137+ 只认 responses）、key 来源（env_key / auth.json / ChatGPT 登录）、网关三连、**`review_model`（`/review` 用的那个）在不在清单里** | `wire_api` 改 responses |
| **Hermes Agent** | config.yaml（新旧两代 schema）、提供方、key（env / ~/.hermes/.env / 明文）、模型是否已声明、网关三连、**声明的那份模型菜单有没有网关给不出的** | — |
| **pi agent** | settings.json + models.json、defaultModel 是否在提供方清单里、key、网关三连、**`enabledModels`（Ctrl+P 能切到的那几个）在不在清单里** | defaultModel 改成清单第一个 |
| **DeepSeek Harness (dsh)** | settings.yaml + .credentials.yaml、每个 profile 的模型覆盖、插件是否装好、Node 是否在、网关三连、**声明的那份模型菜单有没有网关给不出的** | 默认模型改成清单第一个 |
| **cc-switch** | 谁在管：原生官方登录 / 原生自管 / cc-switch 接管 / 记录过时；原生坏了时，cc-switch 里存的备选**先探再推荐** | — |

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
aivet completion --install     装 shell 补全（bash / zsh / fish / PowerShell）
aivet update                   把 aivet 自己更新到最新版
```

**自己更新自己**：`aivet update` 从 GitHub release 下当前平台的二进制，用它换掉正在跑的这个，
下载时有进度条。发布时一起传的 `SHA256SUMS` 是必对的一环 —— 对不上就当场删掉不装，
所以任何一步出错，你手里那个 aivet 都还是完好的。`--check` 只看不动，`--version vX.Y.Z` 可以往回装。
（用 brew 之类装的话更新交给它，别用这个。）

**按 Tab 补全**：装 aivet 的脚本会顺手替你装好，一般不用自己动手。想手动装（或换了 shell）就
`aivet completion --install`——它认出你在用哪个 shell，把脚本写到该在的地方，再往 rc 文件里补上加载它的那几行。
重复跑是安全的：那几行用标记框着，升级时整段换掉，不会越叠越多。
装完之后命令、选项、工具名、可自动修复项的 id 都能补出来（`aivet check <Tab>` → `claude codex hermes …`，
`aivet fix <Tab>` → 这台机器上此刻真能修的那几项）。补全脚本只是层薄壳，每次按 Tab 都回头问 aivet 要候选 ——
所以装一次就够了，以后 aivet 升级、加了命令也不用重装。

不想让它碰你的 rc 文件，就用 `aivet completion zsh > 你说了算的路径`——不带 `--install` 时它只把脚本打到标准输出。

不用记：`aivet help` 是命令总览，`aivet help <命令>`（或 `aivet <命令> --help`）是那个命令的详细页——选项、例子、注意事项都在里面。

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

agent 不用猜命令怎么用——`aivet help --json` 一次给出所有命令、选项、退出码，以及 `check --json` 报告的字段含义和建议的处理流程。

反过来，让 agent 会用 aivet：

```bash
aivet skill install            # 写入 ~/.claude/skills/aivet、~/.codex/skills/aivet、~/.hermes/skills/aivet、~/.pi/agent/skills/aivet
```

之后在任何一个 agent 里说「用 aivet 检查一下我的 AI 环境」，它就知道该跑 `aivet check --json`、怎么读结果、什么时候跑 `aivet fix --yes`。

报告里的 key 一律脱敏，可以放心贴到群里或喂给 agent。

## 它过时的时候会说出来

aivet 要读懂六件工具的配置，就得知道它们的配置文件在哪、字段叫什么、哪个版本删掉了哪个值。
这些事实的寿命比 aivet 的发版周期短 —— 工具升级换个路径、厂商换个域名，aivet 就落后了。

**麻烦的不是落后本身，是落后的表现形式**：解析器读到一个改了名的字段不会报错，
它只会留一个零值，于是后面每条检查都在核对空值，然后一路绿灯。查了等于没查，
而报告上写着「一切正常」—— 这比报错危险，因为没人会来问。

所以：

- **读不懂就明说**。配置文件有内容却没有一个 aivet 认得的字段时，会单独报一条「配置结构」，
  并且**就此打住**，不再往下吐那些没有依据的绿灯。汇总行也会跟着改口，不说「都能用」。
- **知识可以就地补**，不用等发版：

  ```bash
  aivet knowledge          # 看 aivet 现在以为配置文件在哪、认识哪些提供方
  aivet knowledge --init   # 生成 ~/.aivet/knowledge.json，改完立刻生效
  ```

  工具搬了配置文件的家，就往 `paths` 对应键的最前面加一条新路径（内置的仍留作兜底）；
  要加一个新提供方，或改一条版本断言，同理。只写你要改的那几条，其余继续用内置的。
- **版本断言带版本号比**。比如「codex 0.137 起删掉了 `wire_api = "chat"`」：
  装的是旧版就报提醒（你那儿还能跑），新版才报故障；读不出版本号就说读不出来，不瞎断言。
- **`--live` 的结论永远作数**。它让工具自己跑一次去问模型，完全不经过 aivet 对配置的理解 ——
  aivet 的知识再怎么过时，这条路都不会失效。

## 它不做什么

- 不替你保管 key，不做切换器——那是 cc-switch 的活。aivet 对 cc-switch 的立场是：**原生配置优先，官方登录为正，cc-switch 是 fallback**。官方能用时 cc-switch 可以空着，也可以存一份备用不启用，aivet 不会催你迁；原生探不通了，才会把 cc-switch 里存的备选探一遍，通了再告诉你 `cc-switch use`。
- 不猜模型的上下文长度：`setup` 优先照抄网关清单里的 `context_length` / `max_completion_tokens`；网关给不出才退回保守默认值（128k / 16k），并且会明说是退回来的。
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
