// Package codex 体检 OpenAI Codex CLI。
//
// 关键事实（都在检查项里体现）：
//   - 0.137+ 删掉了 wire_api="chat"，只认 Responses API —— 网关必须有 /v1/responses。
//     这个版本号在 knowledge 里（codex.wire_api_chat_removed），检查项会拿它和
//     实际装的版本比，不再无条件断言 —— codex 哪天改回去，无条件断言就成了误报；
//   - 自定义 provider 的 key 有两条路：env_key 指向环境变量，
//     或 requires_openai_auth=true 让它读 ~/.codex/auth.json 里的 OPENAI_API_KEY；
//   - 「Model metadata for … not found」是已知噪声，不算故障。
package codex

import (
	"fmt"
	"os"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// H 实现 harness.Harness。
type H struct{}

func (H) ID() string    { return "codex" }
func (H) Label() string { return "Codex CLI" }

func (H) Detect(c *harness.Context) harness.Detection {
	p, ok := probe.Which("codex")
	if !ok {
		return harness.Detection{}
	}
	v, broken := probe.VersionOr(p, "--version")
	return harness.Detection{Installed: true, Path: p, Version: v, Broken: broken}
}

type provider struct {
	Name               string `toml:"name"`
	BaseURL            string `toml:"base_url"`
	EnvKey             string `toml:"env_key"`
	WireAPI            string `toml:"wire_api"`
	RequiresOpenAIAuth bool   `toml:"requires_openai_auth"`
}

type config struct {
	Model          string              `toml:"model"`
	ModelProvider  string              `toml:"model_provider"`
	ModelProviders map[string]provider `toml:"model_providers"`
}

type auth struct {
	OpenAIAPIKey string         `json:"OPENAI_API_KEY"`
	Tokens       map[string]any `json:"tokens"`
}

type resolved struct {
	cfgPath   string
	cfg       config
	rawCfg    map[string]any // 原样的顶层键，用来判断 aivet 还认不认得这个文件
	cfgErr    error
	provName  string
	prov      provider
	provKnown bool
	key       string
	keySource string
	chatgpt   bool
}

func resolve(c *harness.Context) resolved {
	r := resolved{cfgPath: c.Path("codex.config")}
	r.cfgErr = probe.ReadTOML(r.cfgPath, &r.cfg)
	_ = probe.ReadTOML(r.cfgPath, &r.rawCfg)
	r.provName = r.cfg.ModelProvider
	if r.provName == "" {
		r.provName = "openai"
	}
	r.prov, r.provKnown = r.cfg.ModelProviders[r.provName]
	if !r.provKnown && r.provName == "openai" {
		r.prov, r.provKnown = provider{Name: "OpenAI", BaseURL: "https://api.openai.com/v1", WireAPI: "responses", RequiresOpenAIAuth: true}, true
	}
	var a auth
	_ = probe.ReadJSON(c.Path("codex.auth"), &a)
	r.chatgpt = len(a.Tokens) > 0
	if r.prov.EnvKey != "" {
		if v := c.Env(r.prov.EnvKey); v != "" {
			r.key, r.keySource = v, "环境变量 "+r.prov.EnvKey
		}
	}
	if r.key == "" && a.OpenAIAPIKey != "" && (r.prov.RequiresOpenAIAuth || r.prov.EnvKey == "" || r.prov.EnvKey == "OPENAI_API_KEY") {
		r.key, r.keySource = a.OpenAIAPIKey, "~/.codex/auth.json"
	}
	if r.key == "" && r.prov.EnvKey == "" {
		if v := c.Env("OPENAI_API_KEY"); v != "" {
			r.key, r.keySource = v, "环境变量 OPENAI_API_KEY"
		}
	}
	return r
}

func (h H) Check(c *harness.Context, d harness.Detection) []report.Check {
	b := harness.NewBuilder(h.ID())
	r := resolve(c)
	switch {
	case r.cfgErr == nil:
		b.OK("config", "config.toml", r.cfgPath)
	case probe.IsNotExist(r.cfgErr):
		b.Warn("config", "config.toml", "不存在——走 OpenAI 官方默认", "接网关：aivet setup")
	default:
		b.Fail("config", "config.toml", r.cfgErr.Error(), "TOML 语法错了；常见是引号没配对、表名写错。用 aivet setup --force 重写")
		return b.Checks()
	}
	// config.toml 是个多用途文件（审批策略、沙箱设置都在里面），所以「没有模型配置」
	// 有两种读法：用户本来就没配（走 OpenAI 官方），或者 codex 改了字段名而 aivet 不知道。
	// 分不清就别装作分得清 —— 陈述事实，两种读法都用得上，也不吓人。
	// 不像 hermes / pi / dsh 那几个文件，它们的存在意义就是声明提供方，读不懂就得停下。
	if len(r.rawCfg) > 0 && !hasAny(r.rawCfg, "model", "model_provider", "model_providers") {
		b.Warn("schema", "配置结构",
			"config.toml 里没有 model / model_provider / model_providers 中的任何一项",
			"没配过就是走 OpenAI 官方默认，正常。但如果你以为这里配了网关，那就是没生效 —— "+
				"也可能是 codex 换了这些字段的名字，用 aivet check codex --live 实测")
	}
	if !r.provKnown {
		b.Fail("provider", "model_provider", fmt.Sprintf("model_provider = %q，但没有 [model_providers.%s] 这张表", r.provName, r.provName), "表名和 model_provider 要一字不差；aivet setup 会成对写好")
		return b.Checks()
	}
	b.OK("provider", "model_provider", fmt.Sprintf("%s · %s", r.provName, r.prov.BaseURL))
	if r.cfg.Model != "" {
		b.OK("model", "模型", r.cfg.Model)
	} else {
		b.Warn("model", "模型", "没写 model，codex 会用它内置的默认名", "走网关时几乎必然 404；写上 model = \"…\"")
	}
	checkWireAPI(c, b, d, r)
	switch {
	case r.key != "":
		b.OK("auth", "key", fmt.Sprintf("%s（%s）", probe.MaskKey(r.key), r.keySource))
	case r.chatgpt:
		b.OK("auth", "key", "ChatGPT 账号登录")
	case r.prov.EnvKey != "":
		b.Fail("auth", "key", fmt.Sprintf("env_key = %q，但这个环境变量是空的", r.prov.EnvKey),
			fmt.Sprintf("要么 export %s=…，要么把 key 写进 ~/.codex/auth.json 的 OPENAI_API_KEY 并设 requires_openai_auth = true（aivet setup 走后一条，对新手更稳）", r.prov.EnvKey))
	default:
		b.Fail("auth", "key", "既没有 key 也没登录", "codex login，或 aivet setup 接网关")
	}
	if b.HasFail() {
		return b.Checks()
	}
	if r.key != "" {
		// 探它实际会发的那个协议，不是我们希望它发的那个 ——
		// wire_api 还是 chat 的旧版本，用 Responses 去探必然 404，那是 aivet 的错不是用户的。
		proto := probe.Responses
		if r.prov.WireAPI == "chat" {
			proto = probe.ChatCompletions
		}
		harness.ProbeGateway(c, b, probe.Endpoint{BaseURL: r.prov.BaseURL, Key: r.key, Protocol: proto}, r.cfg.Model)
	}
	harness.LiveRun(c, b, "Codex", d.Path, "exec", "--skip-git-repo-check", harness.LivePrompt)
	return b.Checks()
}

// checkWireAPI 判断 wire_api 的值在**用户这一版** codex 上还成不成立。
//
// 老实现是无条件断言：见到 "chat" 就报故障。那样有两个问题 ——
// 装的是旧版的人会被一条修不了的「故障」挡住（他那版明明能跑），
// 而 codex 哪天把 chat 加回来，这条就变成纯误报。所以改成拿版本号比。
func checkWireAPI(c *harness.Context, b *harness.Builder, d harness.Detection, r resolved) {
	switch r.prov.WireAPI {
	case "", "responses":
		b.OK("wire_api", "wire_api", "responses")
		return
	case "chat":
	default:
		b.Fail("wire_api", "wire_api", "未知值 "+r.prov.WireAPI, "只能是 responses 或 chat（后者已在新版删除）")
		return
	}
	removedIn := c.K().Version("codex.wire_api_chat_removed")
	cmp, ok := probe.CompareVersions(d.Version, removedIn)
	switch {
	case ok && cmp < 0:
		// 这一版还认 chat。报成故障就是误报 —— 但它确实是颗定时炸弹，得说。
		b.WarnFix("wire_api", "wire_api",
			fmt.Sprintf("wire_api = \"chat\"——你这版 %s 还支持它，但 %s 起已删除", d.Version, removedIn),
			"升级 codex 之后会启动即报错。趁现在改成 \"responses\"（前提：网关有 /v1/responses）", "codex.wire_api")
	case ok:
		b.FailFix("wire_api", "wire_api",
			fmt.Sprintf("wire_api = \"chat\"——codex %s 起已删除，你装的是 %s，启动即报错", removedIn, d.Version),
			"改成 \"responses\"（前提：网关有 /v1/responses）", "codex.wire_api")
	default:
		// 读不出版本号就不敢断定方向，但也不能装作没看见。
		b.WarnFix("wire_api", "wire_api",
			fmt.Sprintf("wire_api = \"chat\"——codex %s 起已删除此值，而 aivet 读不出你装的是哪一版（%q）", removedIn, d.Version),
			"新版会启动即报错，旧版还能跑。不确定就改成 \"responses\"，那个两边都认（前提：网关有 /v1/responses）", "codex.wire_api")
	}
}

// hasAny 判断 map 里有没有出现过其中任意一个键。
func hasAny(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func (h H) Fixers() []harness.Fixer {
	return []harness.Fixer{{ID: "codex.wire_api", Title: "wire_api 改为 responses", Apply: fixWireAPI}}
}

func fixWireAPI(c *harness.Context, dry bool) ([]string, error) {
	r := resolve(c)
	if r.cfgErr != nil {
		return nil, r.cfgErr
	}
	if dry {
		return []string{r.cfgPath}, nil
	}
	raw, err := os.ReadFile(r.cfgPath)
	if err != nil {
		return nil, err
	}
	out := probe.TOMLSetTableKey(string(raw), "model_providers."+r.provName, "wire_api", `"responses"`)
	if _, err := probe.Backup(r.cfgPath); err != nil {
		return nil, err
	}
	return []string{r.cfgPath}, probe.WriteFile(r.cfgPath, []byte(out))
}

const providerName = "gateway"

// Configure 写 config.toml 的 provider 表 + auth.json 的 key。
func (H) Configure(c *harness.Context, p harness.Plan) (written, skipped []string, err error) {
	cfgPath := c.Path("codex.config")
	raw, _ := os.ReadFile(cfgPath)
	content := string(raw)
	if probe.TOMLGetTableKey(content, "model_providers."+providerName, "base_url") != "" && !p.Force {
		return nil, []string{cfgPath}, nil
	}
	base := probe.NormalizeBase(p.BaseURL)
	if !hasV1(base) {
		base += "/v1"
	}
	content = probe.TOMLSetTopKey(content, "model", fmt.Sprintf("%q", p.Model))
	content = probe.TOMLSetTopKey(content, "model_provider", fmt.Sprintf("%q", providerName))
	body := fmt.Sprintf("[model_providers.%s]\nname = \"aivet 配置的网关\"\nbase_url = %q\nwire_api = \"responses\"\n# key 放在 ~/.codex/auth.json 的 OPENAI_API_KEY 里，不依赖环境变量\nrequires_openai_auth = true\n", providerName, base)
	content = probe.TOMLReplaceTable(content, "model_providers."+providerName, body)
	if _, err := probe.Backup(cfgPath); err != nil {
		return nil, nil, err
	}
	if err := probe.WriteFile(cfgPath, []byte(content)); err != nil {
		return nil, nil, err
	}
	written = append(written, cfgPath)

	authPath := c.Path("codex.auth")
	a := map[string]any{}
	_ = probe.ReadJSON(authPath, &a)
	a["OPENAI_API_KEY"] = p.Key
	if _, err := probe.Backup(authPath); err != nil {
		return nil, nil, err
	}
	if err := probe.WriteJSON(authPath, a); err != nil {
		return nil, nil, err
	}
	return append(written, authPath), skipped, nil
}

func hasV1(base string) bool { return len(base) >= 3 && base[len(base)-3:] == "/v1" }

// LaunchArgs 交互式启动并带上首条提示。
func (H) LaunchArgs(prompt string) []string { return []string{"codex", prompt} }
