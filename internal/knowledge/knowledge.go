// Package knowledge 收纳 aivet 对「外部世界」的那些会过时的知识：
// 各提供方的地址和环境变量名、Claude Code 的模型别名、各工具的配置文件在哪、
// 哪个版本删掉了哪个配置值。
//
// 为什么把它们从代码里拎出来：这些事实的寿命比 aivet 的发版周期短。
// 工具升级换个配置路径、厂商换个域名，写死在代码里就得等下一个版本 ——
// 而在那之前，aivet 会一边看着错的地方一边报「一切正常」。
//
// 放在这里，用户或 agent 往 ~/.aivet/knowledge.json 补一条就立刻生效。
// 合并规则：内置的是底，用户文件按键覆盖或补充；路径候选是用户的排前面、
// 内置的仍留作兜底。用户只写想改的那几条就行。
package knowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Provider 是一个「官方」提供方：不写 base_url，key 从固定名字的环境变量来。
type Provider struct {
	EnvKeys []string `json:"env_keys"`
	BaseURL string   `json:"base_url"`
	// Protocol 是线协议：chat / responses / anthropic。空串表示 aivet 探不了它。
	Protocol string `json:"protocol"`
}

// File 是 ~/.aivet/knowledge.json 的形状。每一段都可以只写想改的部分。
type File struct {
	Readme        []string            `json:"_readme,omitempty"`
	Providers     map[string]Provider `json:"providers,omitempty"`
	ClaudeAliases map[string]string   `json:"claude_model_aliases,omitempty"`
	Paths         map[string][]string `json:"paths,omitempty"`
	Versions      map[string]string   `json:"versions,omitempty"`
}

// K 是合并后生效的知识。
type K struct {
	File
	// UserFile 是用户文件路径；Loaded 表示它确实被读进来了。
	UserFile string
	Loaded   bool
	// LoadErr 非空 = 用户文件存在但读不了。不能静默 —— 用户以为自己补的知识
	// 生效了，实际 aivet 还在用内置的那份，那比没有这个功能更糟。
	LoadErr error
	// Overridden 是用户实际覆盖/新增了哪些键，给 `aivet knowledge` 显示。
	Overridden []string
	home       string
}

// Builtin 是编译进二进制的那份知识 —— 也就是「写这个版本时已知为真」的事实。
func Builtin() File {
	return File{
		Providers: map[string]Provider{
			"openai":            {[]string{"OPENAI_API_KEY"}, "https://api.openai.com/v1", "chat"},
			"anthropic":         {[]string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}, "https://api.anthropic.com", "anthropic"},
			"deepseek":          {[]string{"DEEPSEEK_API_KEY"}, "https://api.deepseek.com/v1", "chat"},
			"deepseek-official": {[]string{"DEEPSEEK_API_KEY"}, "https://api.deepseek.com/v1", "chat"},
			"openrouter":        {[]string{"OPENROUTER_API_KEY"}, "https://openrouter.ai/api/v1", "chat"},
			"google":            {[]string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, "", ""},
			"gemini":            {[]string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, "", ""},
			"groq":              {[]string{"GROQ_API_KEY"}, "https://api.groq.com/openai/v1", "chat"},
			"mistral":           {[]string{"MISTRAL_API_KEY"}, "https://api.mistral.ai/v1", "chat"},
			"xai":               {[]string{"XAI_API_KEY"}, "https://api.x.ai/v1", "chat"},
			"zai":               {[]string{"ZAI_API_KEY", "GLM_API_KEY"}, "https://api.z.ai/api/paas/v4", "chat"},
			"minimax":           {[]string{"MINIMAX_API_KEY"}, "https://api.minimax.io/v1", "chat"},
			"minimax-cn":        {[]string{"MINIMAX_API_KEY", "MINIMAX_CN_API_KEY"}, "https://api.minimaxi.com/v1", "chat"},
			"kimi":              {[]string{"MOONSHOT_API_KEY", "KIMI_API_KEY"}, "https://api.moonshot.cn/v1", "chat"},
			"moonshot":          {[]string{"MOONSHOT_API_KEY"}, "https://api.moonshot.cn/v1", "chat"},
			"qwen":              {[]string{"DASHSCOPE_API_KEY"}, "https://dashscope.aliyuncs.com/compatible-mode/v1", "chat"},
			"dashscope":         {[]string{"DASHSCOPE_API_KEY"}, "https://dashscope.aliyuncs.com/compatible-mode/v1", "chat"},
			// OminiGate 一个域名两套协议，路径还不一样，所以拆成两条：
			// OpenAI 风格在 /v1（chat + models 清单），Anthropic 风格在根路径
			// （Claude Code 自己会补 /v1/messages —— 给它带 /v1 的地址反而会 404）。
			// 2026-08-28 实测：/v1/models 与 /v1/chat/completions 都是 401（在，要 key），
			// /v1/messages 也是 401；但 /v1/responses 是 404 —— 它没有 Responses API。
			"ominigate":           {[]string{"OMINIGATE_API_KEY", "OPENAI_API_KEY"}, "https://api.ominigate.ai/v1", "chat"},
			"ominigate-anthropic": {[]string{"OMINIGATE_API_KEY", "ANTHROPIC_AUTH_TOKEN"}, "https://api.ominigate.ai", "anthropic"},
		},
		// Claude Code 的别名不是模型名：`model: "sonnet"` 会被它解析成内置的某个
		// claude-sonnet-… id 再发出去，配置里看不到。这张表记的是「把别名重定向
		// 到网关模型」的环境变量名。Claude Code 新加别名时往这里补一行即可。
		ClaudeAliases: map[string]string{
			"sonnet": "ANTHROPIC_DEFAULT_SONNET_MODEL",
			"opus":   "ANTHROPIC_DEFAULT_OPUS_MODEL",
			"haiku":  "ANTHROPIC_DEFAULT_HAIKU_MODEL",
		},
		// 每个键是一串候选，按顺序找第一个存在的。工具改了配置位置时，
		// 往对应键的最前面加一条新路径就能让这个版本的 aivet 继续认得出来。
		Paths: map[string][]string{
			"claude.settings": {"~/.claude/settings.json"},
			"claude.state":    {"~/.claude.json"},
			"claude.creds":    {"~/.claude/.credentials.json"},
			"codex.config":    {"~/.codex/config.toml"},
			"codex.auth":      {"~/.codex/auth.json"},
			"hermes.config":   {"~/.hermes/config.yaml"},
			"hermes.env":      {"~/.hermes/.env"},
			"pi.settings":     {"~/.pi/agent/settings.json"},
			"pi.models":       {"~/.pi/agent/models.json"},
			"pi.auth":         {"~/.pi/agent/auth.json"},
			"dsh.home":        {"~/.dsh"},
			"ccswitch.home":   {"~/.cc-switch"},
		},
		// 「哪个版本起某个配置值不再有效」。检查项据此判断该报故障还是提醒 ——
		// 无条件断言会在工具改回来的那天变成误报。
		Versions: map[string]string{
			"codex.wire_api_chat_removed": "0.137.0",
		},
	}
}

// UserPath 是用户知识文件的位置。
func UserPath(home string) string { return filepath.Join(home, ".aivet", "knowledge.json") }

// Load 读内置知识并叠加用户文件。用户文件不存在是正常情况，不算错。
func Load(home string) *K {
	k := &K{File: Builtin(), UserFile: UserPath(home), home: home}
	b, err := os.ReadFile(k.UserFile)
	if err != nil {
		if !os.IsNotExist(err) {
			k.LoadErr = err
		}
		return k
	}
	var u File
	if err := json.Unmarshal(b, &u); err != nil {
		k.LoadErr = err
		return k
	}
	k.Loaded = true
	for name, p := range u.Providers {
		k.Providers[name] = p
		k.Overridden = append(k.Overridden, "providers."+name)
	}
	for alias, env := range u.ClaudeAliases {
		k.ClaudeAliases[alias] = env
		k.Overridden = append(k.Overridden, "claude_model_aliases."+alias)
	}
	for key, cands := range u.Paths {
		// 用户候选排前面，内置的留作兜底：用户想加一个新位置时只写新的那条，
		// 不必把原来的抄一遍（抄漏了反而更糟）。
		k.Paths[key] = dedupe(append(append([]string{}, cands...), k.Paths[key]...))
		k.Overridden = append(k.Overridden, "paths."+key)
	}
	for key, v := range u.Versions {
		k.Versions[key] = v
		k.Overridden = append(k.Overridden, "versions."+key)
	}
	sort.Strings(k.Overridden)
	return k
}

// Provider 查提供方；找不到返回 false。
func (k *K) Provider(name string) (Provider, bool) {
	p, ok := k.Providers[name]
	return p, ok
}

// AliasEnv 查 Claude Code 模型别名对应的重定向环境变量；不是别名返回 false。
func (k *K) AliasEnv(alias string) (string, bool) {
	e, ok := k.ClaudeAliases[strings.ToLower(alias)]
	return e, ok
}

// Version 查一条版本事实；没有返回空串。
func (k *K) Version(key string) string { return k.Versions[key] }

// Candidates 返回某个键的全部候选路径（~ 已展开）。
func (k *K) Candidates(key string) []string {
	raw := k.Paths[key]
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		out = append(out, k.expand(p))
	}
	return out
}

// Path 返回第一个真实存在的候选；都不存在时返回第一个候选。
//
// 「都不存在就退回第一个」是有意的：报「文件不存在」时要指向默认位置，
// 而不是指向某个用户随手加的备选，否则提示反而更让人糊涂。
func (k *K) Path(key string) string {
	cands := k.Candidates(key)
	if len(cands) == 0 {
		return ""
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return cands[0]
}

func (k *K) expand(p string) string {
	if p == "~" {
		return k.home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(k.home, filepath.FromSlash(p[2:]))
	}
	return filepath.FromSlash(p)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Template 是 `aivet knowledge --init` 写出去的样板：结构齐全、内容是注释性的例子，
// 让人一眼看出该往哪里加什么。
func Template() File {
	return File{
		Readme: []string{
			"这是 aivet 的知识补丁：只写你要改的那几条，其余用内置的。",
			"aivet 对外部工具的了解会随它们升级而过时；与其等 aivet 发新版，不如在这里补一条。",
			"paths：候选按顺序找第一个存在的；你写的排在内置的前面。",
			"providers.<名>.protocol 取值：chat / responses / anthropic。",
			"versions：某个配置值从哪个版本起失效，检查项据此决定报故障还是提醒。",
			"改完跑 aivet knowledge 看有没有生效。完整内置知识：aivet knowledge --json",
		},
		Paths: map[string][]string{
			"claude.settings": {"~/.claude/settings.json"},
		},
		Providers: map[string]Provider{
			"example-vendor": {EnvKeys: []string{"EXAMPLE_API_KEY"}, BaseURL: "https://api.example.com/v1", Protocol: "chat"},
		},
	}
}

// marshalTemplate 供测试确认「--init 写出去的东西自己读得回来」。
func marshalTemplate() ([]byte, error) { return json.MarshalIndent(Template(), "", "  ") }
