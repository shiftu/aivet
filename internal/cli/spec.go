// Package cli 收纳命令行的「说明书」：每个命令是什么、有哪些选项、怎么用。
//
// 为什么单独一个包：帮助信息是 aivet 的对外契约之一 —— 人靠它学命令，
// agent 靠 `aivet help --json` 知道该怎么调。把它和 main 的控制流分开，
// 就能单独测（选项有没有漏写、例子里的命令存不存在），也不会随手改坏。
package cli

// Flag 是一个命令行选项。
type Flag struct {
	Name string `json:"flag"`          // 不带前缀的名字，如 json
	Arg  string `json:"arg,omitempty"` // 需要取值时的占位名，如 URL；布尔选项留空
	Desc string `json:"desc"`
	Env  string `json:"env,omitempty"` // 等价的环境变量
}

// Example 是一条带说明的示例命令。
type Example struct {
	Cmd  string `json:"cmd"`
	Desc string `json:"desc"`
}

// Command 是一个子命令的完整说明。
type Command struct {
	Name     string    `json:"name"`
	Aliases  []string  `json:"aliases,omitempty"`
	Group    string    `json:"group"`
	Summary  string    `json:"summary"` // 一行，总览里显示
	Usage    string    `json:"usage"`
	Long     string    `json:"long,omitempty"` // 详情页的段落说明
	Args     string    `json:"args,omitempty"` // 位置参数说明
	Flags    []Flag    `json:"flags,omitempty"`
	Examples []Example `json:"examples,omitempty"`
	Notes    []string  `json:"notes,omitempty"`
}

// Groups 是总览里的分组顺序。
var Groups = []string{"体检", "修复", "交给 agent", "其他"}

// Tools 是所有可被检查的工具 id。
var Tools = []string{"claude", "codex", "hermes", "pi", "dsh", "ccswitch"}

// ExitCodes 说明退出码含义。
var ExitCodes = map[string]string{
	"0": "没有故障（提醒不算故障）",
	"1": "有故障，或命令执行失败",
	"2": "用法错误",
}

// Tagline 是一句话介绍。
const Tagline = "给套着缰绳的 AI 看病"

// Commands 返回全部命令说明。fixIDs 是当前版本支持的自动修复项，用于把
// `aivet help fix` 写实 —— 它们由各工具包注册，不是写死在这里的常量。
func Commands(fixIDs []string) []Command {
	fixNote := "这个版本没有内置的自动修复项。"
	if len(fixIDs) > 0 {
		fixNote = "这个版本能自动修的：" + join(fixIDs, "、")
	}
	return []Command{
		{
			Name: "check", Aliases: []string{"doctor"}, Group: "体检",
			Summary: "体检所有已安装的工具（默认命令，可省略）",
			Usage:   "aivet [check] [工具…] [选项]",
			Args:    "工具：" + join(Tools, "  ") + "（不填 = 全部）",
			Long: "每件工具都做三件事：读它自己的配置文件、拿配置去打网关、看结果对不对得上。\n" +
				"网关那一步是「三连」——拉模型清单 → 你配的模型在不在清单里 → 真发一条最小请求。\n" +
				"多件工具指向同一个网关时只探一次。默认一两秒跑完。",
			Flags: []Flag{
				{Name: "json", Desc: "输出 JSON（给 agent / 脚本用）；人看的进度改走 stderr"},
				{Name: "live", Desc: "真的把每件工具启动一次，看它能不能回话（每件 10–60 秒）"},
				{Name: "offline", Desc: "一个网络请求都不发，只看配置文件"},
			},
			Examples: []Example{
				{"aivet", "体检全部已安装的工具"},
				{"aivet check codex", "只查 codex"},
				{"aivet check claude --live", "让 claude 真的跑一次，一锤定音"},
				{"aivet check --json", "给 agent 的机器可读输出"},
			},
			Notes: []string{
				"提醒（▲）不影响退出码 —— 能用就是能用。只有故障（✘）才让退出码变 1。",
				"配置里的 key 一律脱敏，报告可以放心贴给别人或喂给 agent。",
				"每次体检都会把完整报告存到 ~/.aivet/last-report.json。",
				"看到「配置结构」这一项，说明工具改了配置格式而 aivet 的知识停在旧版 —— " +
					"底下那些检查等于没查。用 --live 实测，或用 aivet knowledge 补一条。",
			},
		},
		{
			Name: "fix", Group: "修复",
			Summary: "自动修复能修的问题，改动前先备份",
			Usage:   "aivet fix [修复项…] [选项]",
			Args:    "修复项：体检结果里 ⚒ 那一行给出的 id（不填 = 先体检，修所有能修的）",
			Long: "只做有把握的改动。每个被改的文件先复制一份 <原名>.aivet-bak-<时间戳>，\n" +
				"所以随时可以退回去。改不了的（装软件、改 shell 配置）不会硬来，交给 aivet ask。\n\n" + fixNote,
			Flags: []Flag{
				{Name: "yes", Desc: "不逐条问，全部执行"},
				{Name: "dry-run", Desc: "只说要改哪些文件，不真的写"},
			},
			Examples: []Example{
				{"aivet fix --dry-run", "先看看它打算动什么"},
				{"aivet fix", "逐条确认着修"},
				{"aivet fix --yes", "全修，不问"},
				{"aivet fix codex.wire_api", "只修指定的一项"},
			},
		},
		{
			Name: "setup", Aliases: []string{"init"}, Group: "修复",
			Summary: "新手向导：网关 + key + 模型 → 一次写好所有工具",
			Usage:   "aivet setup [选项]",
			Long: "问你三样东西：网关地址、API key、用哪个模型。\n" +
				"先验证这三样确实能通（列出网关上有哪些模型让你挑，再用挑中的模型真发一条请求），\n" +
				"确认没问题之后，才把 Claude Code / Codex / Hermes / pi / dsh 的原生配置文件一次写好。\n\n" +
				"已经存在的配置默认不覆盖 —— 你手改过的东西不该被向导冲掉。要重来加 --force。",
			Flags: []Flag{
				{Name: "gateway", Arg: "URL", Desc: "网关地址，如 https://api.deepseek.com/v1（不填则交互式询问）"},
				{Name: "key", Arg: "KEY", Desc: "API key（不填则交互式询问，输入不回显）", Env: "AIVET_KEY"},
				{Name: "model", Arg: "名字", Desc: "模型名（不填则从网关清单里挑）"},
				{Name: "tools", Arg: "列表", Desc: "只配这几件，逗号分隔，如 claude,codex"},
				{Name: "force", Desc: "覆盖已有配置（默认只补缺）"},
				{Name: "yes", Desc: "非交互：缺什么就报错，不提问"},
			},
			Examples: []Example{
				{"aivet setup", "交互式，最适合第一次用"},
				{"aivet setup --tools claude,codex", "只配这两件"},
				{"aivet setup --gateway https://api.deepseek.com/v1 --key sk-… --model deepseek-chat --yes", "非交互，适合装机脚本"},
			},
			Notes: []string{"网关要是 OpenAI 兼容的（有 /v1/chat/completions）。公司发的、自建的、模型厂商官方的都行。"},
		},
		{
			Name: "ask", Group: "交给 agent",
			Summary: "把体检报告交给一个健康的 agent，让它修剩下的",
			Usage:   "aivet ask [选项]",
			Long: "aivet 只做确定性的诊断和有把握的修复；装软件、改 shell 配置、翻日志这类活儿\n" +
				"交给会动手的 agent。这个命令先跑一次体检，把没通过的项连同报告路径写成提示词，\n" +
				"挑一个体检通过的 agent 前台拉起来，接下来你和它对话。\n\n" +
				"一个健康的都没有时，提示词会直接打在屏幕上 —— 你手边可能有网页版可以粘。",
			Flags: []Flag{
				{Name: "with", Arg: "工具", Desc: "指定谁来接手：claude / codex / hermes / pi / dsh"},
				{Name: "print", Desc: "只打印提示词，不启动任何东西"},
			},
			Examples: []Example{
				{"aivet ask", "自动挑一个能用的 agent 接手"},
				{"aivet ask --with claude", "指定 Claude Code"},
				{"aivet ask --print", "拿到提示词，自己决定粘给谁"},
			},
			Notes: []string{"提示词里的 key 已脱敏。"},
		},
		{
			Name: "skill", Group: "交给 agent",
			Summary: "把 aivet 装成 agent 的技能，让它们会用",
			Usage:   "aivet skill [install|show] [选项]",
			Args:    "install（默认）写入技能文件；show 把内容打到屏幕上",
			Long: "往各 agent 的技能目录写一份 SKILL.md，告诉它们什么时候该跑 aivet、\n" +
				"怎么读 --json 的结果、什么时候可以直接 aivet fix --yes。\n" +
				"装完之后在 agent 里说一句「用 aivet 检查一下我的 AI 环境」就行。",
			Flags: []Flag{
				{Name: "for", Arg: "列表", Desc: "装给谁，逗号分隔（默认：所有已安装的 claude,codex,hermes,pi）"},
			},
			Examples: []Example{
				{"aivet skill install", "装给所有已安装的 agent"},
				{"aivet skill install --for claude", "只装给 Claude Code"},
				{"aivet skill show", "先看看要写什么进去"},
			},
		},
		{
			Name: "env", Group: "体检",
			Summary: "列出系统信息、依赖，以及各工具的安装命令",
			Usage:   "aivet env",
			Long:    "还没装齐工具时先跑这个 —— 它会按你的操作系统给出每件工具的安装命令。",
			Examples: []Example{
				{"aivet env", "看看这台机器上装了什么、还缺什么"},
			},
		},
		{
			Name: "knowledge", Aliases: []string{"know"}, Group: "其他",
			Summary: "查看/修改 aivet 对外部工具的了解（配置在哪、提供方地址、版本断言）",
			Usage:   "aivet knowledge [选项]",
			Long: "aivet 要读懂六件工具的配置，就得知道它们的配置文件在哪、字段叫什么、\n" +
				"哪个版本删掉了哪个值。这些事实会随工具升级而过时 —— 而过时的表现往往不是报错，\n" +
				"是 aivet 看着错的地方报「一切正常」。\n\n" +
				"所以它们不写死在代码里：这个命令让你看见 aivet 现在以为什么是真的，\n" +
				"并且能在 ~/.aivet/knowledge.json 里就地改掉，不用等 aivet 发新版。\n" +
				"没写的部分继续用内置的。",
			Flags: []Flag{
				{Name: "init", Desc: "生成 ~/.aivet/knowledge.json 模板（已存在则不覆盖）"},
				{Name: "json", Desc: "输出全部生效知识（给 agent）"},
			},
			Examples: []Example{
				{"aivet knowledge", "看 aivet 现在认为配置文件在哪、哪些被你改过"},
				{"aivet knowledge --init", "生成模板，然后编辑它"},
				{"aivet knowledge --json", "给 agent：完整的生效知识"},
			},
			Notes: []string{
				"工具升级换了配置位置时，往对应 paths 键的最前面加一条新路径 —— 内置的仍留作兜底。",
				"体检报告里出现「配置结构」提醒，通常就是该来这里补一条的信号。",
			},
		},
		{
			Name: "help", Group: "其他",
			Summary: "看帮助；--json 给 agent",
			Usage:   "aivet help [命令] [--json]",
			Long:    "不带参数是总览，带命令名是那个命令的详细用法。",
			Flags: []Flag{
				{Name: "json", Desc: "输出全部命令、选项、退出码和报告结构的机器可读说明"},
			},
			Examples: []Example{
				{"aivet help", "命令总览"},
				{"aivet help check", "check 的详细用法"},
				{"aivet help --json", "给 agent：一次拿到所有命令的完整规格"},
			},
		},
		{
			Name: "version", Group: "其他",
			Summary: "打印版本号",
			Usage:   "aivet version",
		},
	}
}

// Lookup 按名字或别名找命令。
func Lookup(cmds []Command, name string) (Command, bool) {
	for _, c := range cmds {
		if c.Name == name {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == name {
				return c, true
			}
		}
	}
	return Command{}, false
}

// Suggest 给拼错的命令名找最接近的一个：先看前缀，再看包含关系。
func Suggest(cmds []Command, name string) string {
	if name == "" {
		return ""
	}
	for _, c := range cmds {
		if len(c.Name) >= len(name) && c.Name[:len(name)] == name {
			return c.Name
		}
	}
	for _, c := range cmds {
		if containsStr(c.Name, name) || containsStr(name, c.Name) {
			return c.Name
		}
	}
	// 打字打错位的（chekc / seutp）前缀和包含都匹配不上，靠编辑距离兜底。
	best, bestD := "", 3
	for _, c := range cmds {
		if d := distance(name, c.Name); d < bestD {
			best, bestD = c.Name, d
		}
	}
	return best
}

// distance 是 Levenshtein 距离，只用来给拼错的命令名找最近的那个。
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func containsStr(s, sub string) bool {
	if sub == "" || len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func join(items []string, sep string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
