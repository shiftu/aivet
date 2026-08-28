// Package claude 体检 Claude Code。
//
// 它没有「provider 配置文件」的概念：走网关就是三个环境变量
// （ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_MODEL），
// 可以放在 shell 里，也可以放在 ~/.claude/settings.json 的 env 段。
// 不走网关就是官方登录（OAuth）或 ANTHROPIC_API_KEY。
package claude

import (
	"fmt"
	"strings"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/platform"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// 别名不是模型名：`model: "sonnet"` 会被 Claude Code 解析成它内置的某个
// claude-sonnet-… id 再发出去，那个 id 我们从配置里看不到。所以走网关时
// 要么用 ANTHROPIC_MODEL 直接写死网关上的名字，要么用重定向环境变量把别名映射过去。
// 别名表在 internal/knowledge（claude_model_aliases）—— Claude Code 加了新别名，
// 用户补一行就行，不必等 aivet 发版。

// stripQualifier 去掉 claude-fable-5[1m] 这类结尾的能力后缀 —— 它不属于模型名。
func stripQualifier(m string) string {
	if i := strings.Index(m, "["); i > 0 {
		return strings.TrimSpace(m[:i])
	}
	return m
}

// H 实现 harness.Harness。
type H struct{}

func (H) ID() string    { return "claude" }
func (H) Label() string { return "Claude Code" }

func (H) Detect(c *harness.Context) harness.Detection {
	p, ok := probe.Which("claude")
	if !ok {
		return harness.Detection{}
	}
	v, broken := probe.VersionOr(p, "--version")
	return harness.Detection{Installed: true, Path: p, Version: v, Broken: broken}
}

// resolved 是合并了 shell 环境和 settings.json 之后的有效配置。
type resolved struct {
	settingsPath string
	settings     map[string]any
	settingsErr  error
	envInFile    map[string]string
	baseURL      string
	baseSource   string
	key          string
	keySource    string
	model        string            // 配置里写的那个（可能是别名）
	alias        string            // 是 sonnet/opus/haiku 之一时，这里是别名名字
	aliasEnv     string            // 该别名对应的映射环境变量名
	effModel     string            // 真正会发给网关的模型名；别名没映射时为空
	aliasModels  map[string]string // 别名 → 该别名 env 里映射到的模型（设了才有）
	hasOAuth     bool
	onboarded    bool
	stateErr     error
}

func resolve(c *harness.Context) resolved {
	r := resolved{settingsPath: c.Path("claude.settings"), envInFile: map[string]string{}}
	r.settingsErr = probe.ReadJSON(r.settingsPath, &r.settings)
	if envAny, ok := r.settings["env"].(map[string]any); ok {
		for k, v := range envAny {
			r.envInFile[k] = fmt.Sprint(v)
		}
	}
	pick := func(names ...string) (string, string) {
		for _, n := range names {
			if v := c.Env(n); v != "" {
				return v, "shell 环境变量 " + n
			}
		}
		for _, n := range names {
			if v := r.envInFile[n]; v != "" {
				return v, "settings.json env." + n
			}
		}
		return "", ""
	}
	r.baseURL, r.baseSource = pick("ANTHROPIC_BASE_URL")
	r.key, r.keySource = pick("ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY")
	r.model, _ = pick("ANTHROPIC_MODEL")
	if r.model == "" {
		if m, ok := r.settings["model"].(string); ok {
			r.model = m
		}
	}
	if _, ok := r.settings["apiKeyHelper"]; ok && r.key == "" {
		r.key, r.keySource = "(apiKeyHelper)", "settings.json apiKeyHelper 脚本"
	}
	var state map[string]any
	r.stateErr = probe.ReadJSON(c.Path("claude.state"), &state)
	if r.stateErr == nil {
		r.onboarded, _ = state["hasCompletedOnboarding"].(bool)
		if acct, ok := state["oauthAccount"].(map[string]any); ok && len(acct) > 0 {
			r.hasOAuth = true
		}
	}
	if probe.Exists(c.Path("claude.creds")) {
		r.hasOAuth = true
	}
	// 三个别名各自可以指到不同模型 —— 用户在 /model 里切一下就会发出去，
	// 只验当前那一个等于只验了三分之一。
	r.aliasModels = map[string]string{}
	for alias, envName := range c.K().ClaudeAliases {
		if v, _ := pick(envName); v != "" {
			r.aliasModels[alias] = v
		}
	}
	if base := strings.ToLower(stripQualifier(r.model)); base != "" {
		if envName, isAlias := c.K().AliasEnv(base); isAlias {
			r.alias, r.aliasEnv = base, envName
			r.effModel, _ = pick(envName)
		} else {
			r.effModel = stripQualifier(r.model)
		}
	}
	return r
}

// Posture 报「靠什么在跑」：没网关地址、又有登录态或 key，就是官方；两样都没有是没配置。
func (H) Posture(c *harness.Context) harness.Posture {
	r := resolve(c)
	return harness.Posture{Official: r.baseURL == "" && (r.hasOAuth || r.key != ""), BaseURL: r.baseURL}
}

func (h H) Check(c *harness.Context, d harness.Detection) []report.Check {
	b := harness.NewBuilder(h.ID())
	r := resolve(c)

	switch {
	case r.settingsErr == nil:
		b.OK("settings", "settings.json", r.settingsPath)
	case probe.IsNotExist(r.settingsErr) && (r.baseURL != "" || r.key != "" || r.hasOAuth):
		b.OK("settings", "settings.json", "不存在——配置来自 shell 环境变量或官方登录")
	case probe.IsNotExist(r.settingsErr):
		// 文件不在、shell 里也没有、还没登录 —— 三样都空的时候，「不存在」就不再是
		// 「用默认值」这么无害了：也可能是 Claude Code 换了配置文件的位置，
		// 而 aivet 还在看老地方。报成 OK 等于替一个查不到的东西打包票。
		b.Warn("settings", "settings.json", "在 "+r.settingsPath+" 没找到，shell 里也没有 ANTHROPIC_* ",
			"没配过就跑 aivet setup。如果你确实配过 Claude Code，那可能是它换了配置文件位置 —— "+
				"把真实路径加进 ~/.aivet/knowledge.json 的 paths（aivet knowledge --init 生成模板）")
	default:
		b.Fail("settings", "settings.json", r.settingsErr.Error(), "文件被手改坏了；用 aivet setup 重写，或删掉重来")
		return b.Checks()
	}

	usingGateway := r.baseURL != ""
	switch {
	case usingGateway && r.key != "":
		b.OK("auth", "接入方式", fmt.Sprintf("网关 %s · key %s（%s）", r.baseURL, probe.MaskKey(r.key), r.keySource))
	case usingGateway && r.key == "":
		b.Fail("auth", "接入方式", "设了 ANTHROPIC_BASE_URL 却没有 ANTHROPIC_AUTH_TOKEN", "把网关 key 放进 settings.json env.ANTHROPIC_AUTH_TOKEN（aivet setup 会写）")
	case r.key != "":
		b.OK("auth", "接入方式", fmt.Sprintf("官方 API key %s（%s）", probe.MaskKey(r.key), r.keySource))
	case r.hasOAuth:
		b.OK("auth", "接入方式", "官方账号登录（OAuth）")
	default:
		b.Fail("auth", "接入方式", "既没登录，也没有 key / 网关", "运行 claude 按提示登录；或 aivet setup 接网关")
	}

	if usingGateway && strings.HasSuffix(probe.NormalizeBase(r.baseURL), "/v1") {
		b.WarnFix("base_v1", "BASE_URL 结尾", "ANTHROPIC_BASE_URL 以 /v1 结尾——Claude Code 会自己拼 /v1/messages，多半变成 /v1/v1/messages 而 404",
			"去掉结尾的 /v1", "claude.base_v1")
	}

	if (usingGateway || r.key != "") && !r.onboarded {
		b.WarnFix("onboarding", "首次向导", "hasCompletedOnboarding 不是 true——第一次启动会先问一轮主题/登录问题", "用 key 接入时可以直接跳过", "claude.onboarding")
	}

	for _, k := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL"} {
		if sv, fv := c.Env(k), r.envInFile[k]; sv != "" && fv != "" && sv != fv {
			b.Warn("env_conflict", "环境变量冲突", fmt.Sprintf("%s 在 shell 里和 settings.json 里不一样，shell 的会生效", k), "二选一；shell 里的通常来自 ~/.zshrc / ~/.bashrc / 系统环境变量")
		}
	}

	switch {
	case r.model == "" && usingGateway:
		b.Warn("model", "模型", "没设 ANTHROPIC_MODEL，会用 Claude 内置的默认模型名去打网关", "在 settings.json env 里加 ANTHROPIC_MODEL（aivet setup 会写）")
	case r.model == "":
	case r.alias != "" && r.effModel != "":
		b.OK("model", "模型", fmt.Sprintf("%s → %s（%s）", r.alias, r.effModel, r.aliasEnv))
	default:
		b.OK("model", "模型", r.model)
	}

	if b.HasFail() {
		return b.Checks()
	}
	if r.key != "" && r.key != "(apiKeyHelper)" {
		base := r.baseURL
		if base == "" {
			base = "https://api.anthropic.com"
		}
		ep := probe.Endpoint{BaseURL: base, Key: r.key, Protocol: probe.Anthropic}
		switch {
		case r.alias != "" && r.effModel == "" && usingGateway:
			// 只能查到「网关通不通」为止。再往下就是猜：Claude Code 发出去的是它
			// 内部把别名解析成的那个 id，配置文件里看不到，我们无从核对。
			// 断言它挂了会误伤（网关也可能就认这个别名），断言它没事更糟。
			ids, ok := harness.ProbeReach(c, b, ep)
			if ok {
				hint := "要么把 ANTHROPIC_MODEL 直接写成网关上的名字，要么设 " + r.aliasEnv + " 把别名映射过去（aivet setup --force --tools claude 两件都会写）。想立刻知道到底通不通：aivet check claude --live"
				if len(ids) > 0 {
					hint = "清单里长得像的：" + strings.Join(harness.Similar(ids, r.alias, 5), "、") + "。" + hint
				}
				b.Warn("model.alias", "模型别名没映射",
					fmt.Sprintf("配置里是别名 %q，Claude Code 会把它解析成自己内置的模型 id 再发给网关 —— 那个 id 网关多半没有，但从配置看不出来", r.alias), hint)
			}
		default:
			harness.ProbeGateway(c, b, ep, r.effModel)
		}
		var slots []harness.ModelSlot
		for alias, m := range r.aliasModels {
			slots = append(slots, harness.Slot(alias+" 别名", m))
		}
		harness.CheckOtherModels(c, b, ep, r.effModel, slots)
	} else if !c.Live {
		b.Warn("gateway", "网关探测", "OAuth 登录 / helper 脚本没法用 HTTP 探，只能真跑一次", "aivet check claude --live")
	}
	harness.LiveRun(c, b, "Claude Code", d.Path, "-p", harness.LivePrompt, "--output-format", "text")
	return b.Checks()
}

func (h H) Fixers() []harness.Fixer {
	return []harness.Fixer{
		{ID: "claude.onboarding", Title: "跳过首次向导（hasCompletedOnboarding=true）", Apply: fixOnboarding},
		{ID: "claude.base_v1", Title: "去掉 ANTHROPIC_BASE_URL 结尾的 /v1", Apply: fixBaseV1},
	}
}

func fixOnboarding(c *harness.Context, dry bool) ([]string, error) {
	p := c.Path("claude.state")
	if dry {
		return []string{p}, nil
	}
	state := map[string]any{}
	if err := probe.ReadJSON(p, &state); err != nil && !probe.IsNotExist(err) {
		return nil, err
	}
	state["hasCompletedOnboarding"] = true
	if _, err := probe.Backup(p); err != nil {
		return nil, err
	}
	return []string{p}, probe.WriteJSON(p, state)
}

func fixBaseV1(c *harness.Context, dry bool) ([]string, error) {
	p := c.Path("claude.settings")
	if dry {
		return []string{p}, nil
	}
	s := map[string]any{}
	if err := probe.ReadJSON(p, &s); err != nil {
		return nil, err
	}
	env, _ := s["env"].(map[string]any)
	if env == nil {
		return nil, fmt.Errorf("settings.json 里没有 env 段——BASE_URL 来自 shell 环境变量，请手动改 ~/.zshrc 等")
	}
	base, _ := env["ANTHROPIC_BASE_URL"].(string)
	env["ANTHROPIC_BASE_URL"] = strings.TrimSuffix(probe.NormalizeBase(base), "/v1")
	if _, err := probe.Backup(p); err != nil {
		return nil, err
	}
	return []string{p}, probe.WriteJSON(p, s)
}

// Configure 把网关写进 settings.json env，并跳过首次向导。
func (H) Configure(c *harness.Context, p harness.Plan) (written, skipped []string, err error) {
	sp := c.Path("claude.settings")
	s := map[string]any{}
	if err := probe.ReadJSON(sp, &s); err != nil && !probe.IsNotExist(err) {
		return nil, nil, err
	}
	env, _ := s["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	if _, has := env["ANTHROPIC_BASE_URL"]; has && !p.Force {
		skipped = append(skipped, sp)
	} else {
		base := strings.TrimSuffix(probe.NormalizeBase(p.BaseURL), "/v1")
		env["ANTHROPIC_BASE_URL"] = base
		env["ANTHROPIC_AUTH_TOKEN"] = p.Key
		delete(env, "ANTHROPIC_API_KEY")
		env["ANTHROPIC_MODEL"] = p.Model
		// /model 里的 sonnet/opus/haiku 别名也指到同一个模型，免得学员切一下就 404。
		// 别名表从 knowledge 拿：Claude Code 新加别名时补一行就能一起写进去。
		for _, k := range c.K().ClaudeAliases {
			env[k] = p.Model
		}
		s["env"] = env
		if _, err := probe.Backup(sp); err != nil {
			return nil, nil, err
		}
		if err := probe.WriteJSON(sp, s); err != nil {
			return nil, nil, err
		}
		written = append(written, sp)
	}
	if w, err := fixOnboarding(c, false); err == nil {
		written = append(written, w...)
	}
	return written, skipped, nil
}

// LaunchArgs 交互式启动并带上首条提示。
func (H) LaunchArgs(prompt string) []string { return []string{"claude", prompt} }

// InstallHint 安装提示。
func (H) InstallHint() string { return platform.Install("claude") }
