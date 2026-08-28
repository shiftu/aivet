// Package pi 体检 pi coding agent（@mariozechner/pi-coding-agent）。
//
// ~/.pi/agent/settings.json 决定 defaultProvider / defaultModel；
// ~/.pi/agent/models.json 声明自定义 provider：baseUrl / api / apiKey / models[]。
// 内置 provider（anthropic/openai/google…）走固定环境变量或 auth.json 里的 OAuth。
package pi

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// H 实现 harness.Harness。
type H struct{}

func (H) ID() string    { return "pi" }
func (H) Label() string { return "pi agent" }

func (H) Detect(c *harness.Context) harness.Detection {
	p, ok := probe.Which("pi")
	if !ok {
		return harness.Detection{}
	}
	v, broken := probe.VersionOr(p, "--version")
	return harness.Detection{Installed: true, Path: p, Version: v, Broken: broken}
}

type modelDef struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Reasoning     bool   `json:"reasoning"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
}

type providerDef struct {
	Name    string     `json:"name,omitempty"`
	BaseURL string     `json:"baseUrl"`
	API     string     `json:"api"`
	APIKey  string     `json:"apiKey,omitempty"`
	Models  []modelDef `json:"models"`
}

type modelsFile struct {
	Providers map[string]providerDef `json:"providers"`
}

type resolved struct {
	settingsPath string
	settings     map[string]any
	settingsErr  error
	modelsPath   string
	models       modelsFile
	modelsErr    error
	provName     string
	model        string
	custom       bool
	prov         providerDef
	key          string
	keySource    string
	hasAuth      bool
}

var envNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,}$`)

func resolve(c *harness.Context) resolved {
	dir := filepath.Join(c.Home, ".pi", "agent")
	r := resolved{settingsPath: filepath.Join(dir, "settings.json"), modelsPath: filepath.Join(dir, "models.json")}
	r.settingsErr = probe.ReadJSON(r.settingsPath, &r.settings)
	r.modelsErr = probe.ReadJSON(r.modelsPath, &r.models)
	if v, ok := r.settings["defaultProvider"].(string); ok {
		r.provName = v
	}
	if v, ok := r.settings["defaultModel"].(string); ok {
		r.model = v
	}
	var auth map[string]any
	if probe.ReadJSON(filepath.Join(dir, "auth.json"), &auth) == nil {
		_, r.hasAuth = auth[r.provName]
	}
	if p, ok := r.models.Providers[r.provName]; ok {
		r.custom, r.prov = true, p
		switch {
		case p.APIKey == "":
		case envNameRe.MatchString(p.APIKey) && c.Env(p.APIKey) != "":
			r.key, r.keySource = c.Env(p.APIKey), "环境变量 "+p.APIKey
		default:
			r.key, r.keySource = p.APIKey, "models.json 明文 apiKey"
		}
		return r
	}
	if kp, ok := harness.LookupProvider(r.provName); ok {
		r.prov = providerDef{BaseURL: kp.BaseURL, API: apiFor(kp.Protocol)}
		if n, v := harness.FirstEnv(c.Env, kp.EnvKeys...); v != "" {
			r.key, r.keySource = v, "环境变量 "+n
		}
	}
	return r
}

func apiFor(p probe.Protocol) string {
	switch p {
	case probe.Anthropic:
		return "anthropic-messages"
	case probe.Responses:
		return "openai-responses"
	}
	return "openai-completions"
}

func protocolFor(api string) (probe.Protocol, bool) {
	switch api {
	case "openai-completions":
		return probe.ChatCompletions, true
	case "openai-responses":
		return probe.Responses, true
	case "anthropic-messages":
		return probe.Anthropic, true
	}
	return "", false
}

func (h H) Check(c *harness.Context, d harness.Detection) []report.Check {
	b := harness.NewBuilder(h.ID())
	r := resolve(c)
	switch {
	case r.settingsErr == nil:
		b.OK("settings", "settings.json", r.settingsPath)
	case probe.IsNotExist(r.settingsErr):
		b.Warn("settings", "settings.json", "不存在——pi 会用内置默认（google）", "aivet setup 接网关，或 pi 里用 /model 选")
	default:
		b.Fail("settings", "settings.json", r.settingsErr.Error(), "JSON 坏了；aivet setup --force 重写")
		return b.Checks()
	}
	if r.modelsErr != nil && !probe.IsNotExist(r.modelsErr) {
		b.Fail("models_json", "models.json", r.modelsErr.Error(), "JSON 坏了；aivet setup --force 重写")
		return b.Checks()
	}
	if r.provName == "" {
		b.Warn("provider", "提供方", "没设 defaultProvider", "aivet setup")
		return b.Checks()
	}
	if r.custom {
		b.OK("provider", "提供方", fmt.Sprintf("%s（models.json）· %s · %s", r.provName, r.prov.BaseURL, r.prov.API))
	} else if _, ok := harness.LookupProvider(r.provName); ok {
		b.OK("provider", "提供方", r.provName+"（内置）")
	} else {
		b.Warn("provider", "提供方", r.provName+"：models.json 里没有它，aivet 也不认识", "--live 实测")
	}
	if r.model == "" {
		b.Fail("model", "模型", "没设 defaultModel", "aivet setup，或 pi 里 /model")
	} else if r.custom && len(r.prov.Models) > 0 {
		ids := make([]string, 0, len(r.prov.Models))
		for _, m := range r.prov.Models {
			ids = append(ids, m.ID)
		}
		if probe.HasModel(ids, r.model) {
			b.OK("model", "模型", r.model)
		} else {
			b.FailFix("model", "模型", fmt.Sprintf("defaultModel = %q 不在 providers.%s.models 里，pi 启动会报找不到模型", r.model, r.provName),
				"改成 models 里有的一个：aivet fix pi.default_model 会选第一个", "pi.default_model")
		}
	} else {
		b.OK("model", "模型", r.model)
	}
	switch {
	case r.key != "":
		b.OK("auth", "key", fmt.Sprintf("%s（%s）", probe.MaskKey(r.key), r.keySource))
	case r.hasAuth:
		b.OK("auth", "key", "auth.json 里有该提供方的凭证（OAuth）")
	case r.custom:
		b.Fail("auth", "key", "models.json 里这个 provider 没有 apiKey", "填上 apiKey（可以是环境变量名）")
	default:
		b.Fail("auth", "key", "没找到这个提供方的环境变量，也没登录", "pi 里 /login，或设置环境变量")
	}
	if b.HasFail() {
		return b.Checks()
	}
	if proto, ok := protocolFor(r.prov.API); ok && r.key != "" && r.prov.BaseURL != "" {
		harness.ProbeGateway(c, b, probe.Endpoint{BaseURL: r.prov.BaseURL, Key: r.key, Protocol: proto}, r.model)
	}
	harness.LiveRun(c, b, "pi", d.Path, "-p", "--no-session", harness.LivePrompt)
	return b.Checks()
}

func (h H) Fixers() []harness.Fixer {
	return []harness.Fixer{{ID: "pi.default_model", Title: "defaultModel 改成提供方声明的第一个模型", Apply: fixDefaultModel}}
}

func fixDefaultModel(c *harness.Context, dry bool) ([]string, error) {
	r := resolve(c)
	if !r.custom || len(r.prov.Models) == 0 {
		return nil, fmt.Errorf("provider %q 没有声明任何模型，改不了", r.provName)
	}
	if dry {
		return []string{r.settingsPath}, nil
	}
	r.settings["defaultModel"] = r.prov.Models[0].ID
	if _, err := probe.Backup(r.settingsPath); err != nil {
		return nil, err
	}
	return []string{r.settingsPath}, probe.WriteJSON(r.settingsPath, r.settings)
}

const providerName = "gateway"

// Configure 写 models.json 的 provider + settings.json 的默认值。
func (H) Configure(c *harness.Context, p harness.Plan) (written, skipped []string, err error) {
	dir := filepath.Join(c.Home, ".pi", "agent")
	mp, sp := filepath.Join(dir, "models.json"), filepath.Join(dir, "settings.json")
	mf := map[string]any{}
	_ = probe.ReadJSON(mp, &mf)
	provs, _ := mf["providers"].(map[string]any)
	if provs == nil {
		provs = map[string]any{}
	}
	if _, has := provs[providerName]; has && !p.Force {
		return nil, []string{mp}, nil
	}
	base := probe.NormalizeBase(p.BaseURL)
	if len(base) < 3 || base[len(base)-3:] != "/v1" {
		base += "/v1"
	}
	provs[providerName] = providerDef{Name: "aivet 配置的网关", BaseURL: base, API: "openai-completions", APIKey: p.Key,
		Models: []modelDef{{ID: p.Model, Name: p.Model, ContextWindow: 128000, MaxTokens: 16384}}}
	mf["providers"] = provs
	if _, err := probe.Backup(mp); err != nil {
		return nil, nil, err
	}
	if err := probe.WriteJSON(mp, mf); err != nil {
		return nil, nil, err
	}
	s := map[string]any{}
	_ = probe.ReadJSON(sp, &s)
	s["defaultProvider"], s["defaultModel"] = providerName, p.Model
	if _, err := probe.Backup(sp); err != nil {
		return nil, nil, err
	}
	if err := probe.WriteJSON(sp, s); err != nil {
		return nil, nil, err
	}
	return []string{mp, sp}, nil, nil
}

// LaunchArgs 交互式启动并带上首条提示。
func (H) LaunchArgs(prompt string) []string { return []string{"pi", prompt} }
