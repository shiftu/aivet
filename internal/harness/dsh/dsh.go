// Package dsh 体检 DeepSeek Harness（@deepseek-ai/dsh）。
//
// ~/.dsh/settings.yaml：
//
//	llm-pi-ai.providers.<name>: {apiKeyEnv, api, baseURL, models[{id}]}
//	agent-default-model: {provider, model}
//
// ~/.dsh/.credentials.yaml：refs.<ENV名>: key   —— apiKeyEnv 找不到环境变量时从这里取。
// 每个 profile（tui/web/headless）的 cordis.patch.yml 还可以覆盖 agent-default-model。
package dsh

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/platform"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// H 实现 harness.Harness。
type H struct{}

func (H) ID() string    { return "dsh" }
func (H) Label() string { return "DeepSeek Harness (dsh)" }

func (H) Detect(c *harness.Context) harness.Detection {
	p, ok := probe.Which("dsh")
	if !ok {
		return harness.Detection{}
	}
	v, broken := probe.VersionOr(p, "--version")
	return harness.Detection{Installed: true, Path: p, Version: v, Broken: broken}
}

type providerDef struct {
	DisplayName string `yaml:"displayName,omitempty"`
	APIKeyEnv   string `yaml:"apiKeyEnv"`
	API         string `yaml:"api"`
	BaseURL     string `yaml:"baseURL"`
	Models      []struct {
		ID            string `yaml:"id"`
		ContextWindow int    `yaml:"contextWindow,omitempty"`
		MaxTokens     int    `yaml:"maxTokens,omitempty"`
	} `yaml:"models"`
}

type target struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

type settings struct {
	LLM struct {
		Providers map[string]providerDef `yaml:"providers"`
	} `yaml:"llm-pi-ai"`
	Default target `yaml:"agent-default-model"`
}

type credentials struct {
	Version int               `yaml:"version"`
	Refs    map[string]string `yaml:"refs"`
}

type resolved struct {
	home        string
	settingsErr error
	s           settings
	raw         map[string]any // 原样的顶层键，用来判断 aivet 还认不认得这个文件
	creds       credentials
	credsErr    error
	overrides   map[string]target // profile → 覆盖的默认模型
}

func resolve(c *harness.Context) resolved {
	r := resolved{home: c.Path("dsh.home"), overrides: map[string]target{}}
	if h := c.Env("DSH_HOME"); h != "" {
		r.home = h
	}
	r.settingsErr = probe.ReadYAML(filepath.Join(r.home, "settings.yaml"), &r.s)
	_ = probe.ReadYAML(filepath.Join(r.home, "settings.yaml"), &r.raw)
	r.credsErr = probe.ReadYAML(filepath.Join(r.home, ".credentials.yaml"), &r.creds)
	if r.s.Default.Provider == "" {
		r.s.Default = target{Provider: "deepseek-official", Model: "deepseek-v4-flash"}
	}
	entries, _ := os.ReadDir(filepath.Join(r.home, "profiles"))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "node_modules") || strings.HasPrefix(e.Name(), "-") {
			continue
		}
		var patch []struct {
			ID     string `yaml:"id"`
			Config target `yaml:"config"`
		}
		if probe.ReadYAML(filepath.Join(r.home, "profiles", e.Name(), "cordis.patch.yml"), &patch) != nil {
			continue
		}
		for _, p := range patch {
			if p.ID == "agent-default-model" && (p.Config.Provider != "" || p.Config.Model != "") {
				t := p.Config
				if t.Provider == "" {
					t.Provider = r.s.Default.Provider
				}
				if t.Model == "" {
					t.Model = r.s.Default.Model
				}
				r.overrides[e.Name()] = t
			}
		}
	}
	return r
}

// endpoint 解析一个 (provider, model) 目标的网关信息。
func (r resolved) endpoint(c *harness.Context, t target) (ep probe.Endpoint, keySource string, declared []string, custom bool) {
	if p, ok := r.s.LLM.Providers[t.Provider]; ok {
		custom = true
		ep.BaseURL = p.BaseURL
		ep.Protocol = probe.ChatCompletions
		if p.API == "openai-responses" {
			ep.Protocol = probe.Responses
		} else if p.API == "anthropic-messages" {
			ep.Protocol = probe.Anthropic
		}
		for _, m := range p.Models {
			declared = append(declared, m.ID)
		}
		if p.APIKeyEnv != "" {
			if v := c.Env(p.APIKeyEnv); v != "" {
				ep.Key, keySource = v, "环境变量 "+p.APIKeyEnv
			} else if v := r.creds.Refs[p.APIKeyEnv]; v != "" {
				ep.Key, keySource = v, ".credentials.yaml refs."+p.APIKeyEnv
			}
		}
		return
	}
	if kp, ok := c.Provider(t.Provider); ok {
		ep.BaseURL, ep.Protocol = kp.BaseURL, kp.Protocol
		if n, v := harness.FirstEnv(c.Env, kp.EnvKeys...); v != "" {
			ep.Key, keySource = v, "环境变量 "+n
		} else {
			for _, n := range kp.EnvKeys {
				if v := r.creds.Refs[n]; v != "" {
					ep.Key, keySource = v, ".credentials.yaml refs."+n
					break
				}
			}
		}
	}
	return
}

func (h H) Check(c *harness.Context, d harness.Detection) []report.Check {
	b := harness.NewBuilder(h.ID())
	r := resolve(c)
	if _, ok := probe.Which("node"); !ok {
		b.Fail("node", "Node.js", "dsh 是 Node 程序，但 PATH 上没有 node", platform.NodeHint())
	}
	switch {
	case r.settingsErr == nil:
		b.OK("settings", "settings.yaml", filepath.Join(r.home, "settings.yaml"))
	case probe.IsNotExist(r.settingsErr):
		b.Warn("settings", "settings.yaml", "不存在——用内置默认（deepseek-official / deepseek-v4-flash）", "需要 DEEPSEEK_API_KEY；或 aivet setup 接网关")
	default:
		b.Fail("settings", "settings.yaml", r.settingsErr.Error(), "YAML 缩进错了？aivet setup --force 重写")
		return b.Checks()
	}
	if r.credsErr != nil && !probe.IsNotExist(r.credsErr) {
		b.Fail("credentials", ".credentials.yaml", r.credsErr.Error(), "")
		return b.Checks()
	}
	// settings.yaml 有内容却没有这两个键 = aivet 读不懂它，
	// 再往下就会拿内置默认（deepseek-official）去核对，而那不是用户配的东西。
	if harness.SchemaDrift(b, h.ID(), "schema", filepath.Join(r.home, "settings.yaml"), r.raw, "llm-pi-ai", "agent-default-model") {
		return b.Checks()
	}
	targets := map[string]target{"全局默认": r.s.Default}
	for prof, t := range r.overrides {
		targets["profile "+prof] = t
	}
	first := true
	for _, label := range orderedKeys(targets) {
		t := targets[label]
		ep, keySource, declared, custom := r.endpoint(c, t)
		id := "target"
		if label != "全局默认" {
			id = "target." + strings.TrimPrefix(label, "profile ")
		}
		if ep.BaseURL == "" && !custom {
			b.Warn(id, label, fmt.Sprintf("%s / %s：提供方既不在 settings.yaml 也不是内置的", t.Provider, t.Model), "")
			continue
		}
		if custom && len(declared) > 0 && !probe.HasModel(declared, t.Model) {
			fix := ""
			if first {
				fix = "dsh.default_model"
			}
			b.FailFix(id, label, fmt.Sprintf("%s / %s：模型不在该提供方的 models 列表里", t.Provider, t.Model), "改成列表里有的：aivet fix dsh.default_model 会选第一个", fix)
			continue
		}
		if ep.Key == "" {
			hint := "在 ~/.dsh/.credentials.yaml 的 refs 里加上对应的 key（aivet setup 会写）"
			if !custom {
				hint = "设置 DEEPSEEK_API_KEY，或 aivet setup 接网关"
			}
			b.Fail(id+".auth", label+" key", fmt.Sprintf("%s / %s：没找到 key", t.Provider, t.Model), hint)
			continue
		}
		b.OK(id, label, fmt.Sprintf("%s / %s · key %s（%s）", t.Provider, t.Model, probe.MaskKey(ep.Key), keySource))
		if first {
			harness.ProbeGateway(c, b, ep, t.Model)
			// 声明的 models 是用户能切到的菜单；profile 的覆盖模型上面每条已经单独验过了。
			slots := make([]harness.ModelSlot, 0, len(declared))
			for _, m := range declared {
				slots = append(slots, harness.Slot("声明的", m))
			}
			harness.CheckOtherModels(c, b, ep, t.Model, slots)
		}
		first = false
	}
	for _, prof := range []string{"tui", "headless"} {
		pd := filepath.Join(r.home, "profiles", prof)
		if probe.Exists(pd) && !probe.Exists(filepath.Join(pd, "node_modules")) {
			b.Warn("profile."+prof, "profile "+prof, "插件还没安装（没有 node_modules）", "第一次启动会自动 pnpm install，需要能访问 npm；卡住就手动：dsh plugin --profile "+prof+" install")
		}
	}
	harness.LiveRun(c, b, "dsh", d.Path, "--profile", "headless", harness.LivePrompt)
	return b.Checks()
}

func orderedKeys(m map[string]target) []string {
	keys := []string{"全局默认"}
	for k := range m {
		if k != "全局默认" {
			keys = append(keys, k)
		}
	}
	return keys
}

func (h H) Fixers() []harness.Fixer {
	return []harness.Fixer{{ID: "dsh.default_model", Title: "agent-default-model 改成提供方声明的第一个模型", Apply: fixDefaultModel}}
}

func fixDefaultModel(c *harness.Context, dry bool) ([]string, error) {
	r := resolve(c)
	p, ok := r.s.LLM.Providers[r.s.Default.Provider]
	if !ok || len(p.Models) == 0 {
		return nil, fmt.Errorf("提供方 %q 没有声明模型，改不了", r.s.Default.Provider)
	}
	sp := filepath.Join(r.home, "settings.yaml")
	if dry {
		return []string{sp}, nil
	}
	raw := map[string]any{}
	if err := probe.ReadYAML(sp, &raw); err != nil {
		return nil, err
	}
	raw["agent-default-model"] = map[string]any{"provider": r.s.Default.Provider, "model": p.Models[0].ID}
	if _, err := probe.Backup(sp); err != nil {
		return nil, err
	}
	return []string{sp}, probe.WriteYAML(sp, raw)
}

const providerName = "gateway"
const keyEnvName = "AIVET_GATEWAY_KEY"

// Configure 写 settings.yaml 的 provider + 默认模型，key 进 .credentials.yaml。
func (H) Configure(c *harness.Context, p harness.Plan) (written, skipped []string, err error) {
	home := c.Path("dsh.home")
	if h := c.Env("DSH_HOME"); h != "" {
		home = h
	}
	sp, cp := filepath.Join(home, "settings.yaml"), filepath.Join(home, ".credentials.yaml")
	raw := map[string]any{}
	if err := probe.ReadYAML(sp, &raw); err != nil && !probe.IsNotExist(err) {
		return nil, nil, err
	}
	llm, _ := raw["llm-pi-ai"].(map[string]any)
	if llm == nil {
		llm = map[string]any{}
	}
	provs, _ := llm["providers"].(map[string]any)
	if provs == nil {
		provs = map[string]any{}
	}
	if _, has := provs[providerName]; has && !p.Force {
		return nil, []string{sp}, nil
	}
	base := probe.NormalizeBase(p.BaseURL)
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	provs[providerName] = map[string]any{
		"displayName": "aivet 配置的网关",
		"apiKeyEnv":   keyEnvName,
		"api":         "openai-completions",
		"baseURL":     base,
		"models":      []any{map[string]any{"id": p.Model, "contextWindow": p.Context(), "maxTokens": p.MaxOut()}},
	}
	llm["providers"] = provs
	raw["llm-pi-ai"] = llm
	raw["agent-default-model"] = map[string]any{"provider": providerName, "model": p.Model}
	if _, err := probe.Backup(sp); err != nil {
		return nil, nil, err
	}
	if err := probe.WriteYAML(sp, raw); err != nil {
		return nil, nil, err
	}
	creds := map[string]any{}
	_ = probe.ReadYAML(cp, &creds)
	refs, _ := creds["refs"].(map[string]any)
	if refs == nil {
		refs = map[string]any{}
	}
	refs[keyEnvName] = p.Key
	creds["refs"] = refs
	if _, ok := creds["version"]; !ok {
		creds["version"] = 1
	}
	if _, err := probe.Backup(cp); err != nil {
		return nil, nil, err
	}
	if err := probe.WriteYAML(cp, creds); err != nil {
		return nil, nil, err
	}
	return []string{sp, cp}, nil, nil
}

// LaunchArgs：用 headless profile 单次接手（tui 是否接受首条提示未确认）。
func (H) LaunchArgs(prompt string) []string { return []string{"dsh", "--profile", "headless", prompt} }
