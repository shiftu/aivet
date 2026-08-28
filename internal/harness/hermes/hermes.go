// Package hermes 体检 Hermes Agent（NousResearch）。
//
// 配置在 ~/.hermes/config.yaml。两代 schema 都认：
//
//	新：model: {default, provider}      旧：model: "…"  provider: "…"
//
// 自定义提供方在 providers.<name>：base_url / api_mode / key_env / api_key / models。
// key 的来源顺序：shell 环境变量 → ~/.hermes/.env → config 里的 api_key 明文。
package hermes

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// H 实现 harness.Harness。
type H struct{}

func (H) ID() string    { return "hermes" }
func (H) Label() string { return "Hermes Agent" }

func (H) Detect(c *harness.Context) harness.Detection {
	p, ok := probe.Which("hermes")
	if !ok {
		return harness.Detection{}
	}
	v := probe.Version(p, "--version")
	if i := strings.Index(v, " ·"); i > 0 {
		v = v[:i]
	}
	return harness.Detection{Installed: true, Path: p, Version: v}
}

type resolved struct {
	cfgPath   string
	cfg       map[string]any
	cfgErr    error
	provName  string
	model     string
	custom    bool
	baseURL   string
	apiMode   string
	keyEnv    string
	key       string
	keySource string
	models    []string
}

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func sub(m map[string]any, k string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[k].(map[string]any)
	return v
}

func resolve(c *harness.Context) resolved {
	r := resolved{cfgPath: filepath.Join(c.Home, ".hermes", "config.yaml")}
	r.cfgErr = probe.ReadYAML(r.cfgPath, &r.cfg)
	if m := sub(r.cfg, "model"); m != nil {
		r.model, r.provName = str(m, "default"), str(m, "provider")
	} else {
		r.model, r.provName = str(r.cfg, "model"), str(r.cfg, "provider")
	}
	dotenv := probe.ParseDotenv(filepath.Join(c.Home, ".hermes", ".env"))
	env := func(k string) string {
		if v := c.Env(k); v != "" {
			return v
		}
		return dotenv[k]
	}
	if p := sub(sub(r.cfg, "providers"), r.provName); p != nil {
		r.custom = true
		r.baseURL, r.apiMode, r.keyEnv = str(p, "base_url"), str(p, "api_mode"), str(p, "key_env")
		if r.model == "" {
			r.model = str(p, "default_model")
		}
		switch ms := p["models"].(type) {
		case []any:
			for _, m := range ms {
				if s, ok := m.(string); ok {
					r.models = append(r.models, s)
				} else if mm, ok := m.(map[string]any); ok {
					r.models = append(r.models, str(mm, "id"))
				}
			}
		case map[string]any:
			for id := range ms {
				r.models = append(r.models, id)
			}
		}
		if r.keyEnv != "" {
			if v := env(r.keyEnv); v != "" {
				r.key, r.keySource = v, r.keyEnv
				if c.Env(r.keyEnv) == "" {
					r.keySource = "~/.hermes/.env " + r.keyEnv
				}
			}
		}
		if r.key == "" {
			if v := str(p, "api_key"); v != "" {
				r.key, r.keySource = v, "config.yaml 明文 api_key"
			}
		}
		return r
	}
	if kp, ok := harness.LookupProvider(r.provName); ok {
		r.baseURL = kp.BaseURL
		r.apiMode = string(kp.Protocol)
		if n, v := harness.FirstEnv(env, kp.EnvKeys...); v != "" {
			r.key, r.keySource = v, n
		}
	}
	return r
}

func (h H) Check(c *harness.Context, d harness.Detection) []report.Check {
	b := harness.NewBuilder(h.ID())
	r := resolve(c)
	switch {
	case r.cfgErr == nil:
		b.OK("config", "config.yaml", r.cfgPath)
	case probe.IsNotExist(r.cfgErr):
		b.Fail("config", "config.yaml", "不存在", "跑一次 hermes setup，或 aivet setup 接网关")
		return b.Checks()
	default:
		b.Fail("config", "config.yaml", r.cfgErr.Error(), "YAML 缩进错了？用 aivet setup --force 重写")
		return b.Checks()
	}
	if r.provName == "" {
		b.Fail("provider", "提供方", "config.yaml 里没有 model.provider", "hermes model 选一个，或 aivet setup")
		return b.Checks()
	}
	if r.custom {
		b.OK("provider", "提供方", fmt.Sprintf("%s（自定义）· %s · %s", r.provName, r.baseURL, r.apiMode))
	} else if _, ok := harness.LookupProvider(r.provName); ok {
		b.OK("provider", "提供方", r.provName+"（内置）")
	} else {
		b.Warn("provider", "提供方", fmt.Sprintf("%s：既不在 providers 段，aivet 也不认识它", r.provName), "如果 hermes 自己能跑就没事；--live 试试")
	}
	if r.model != "" {
		b.OK("model", "模型", r.model)
	} else {
		b.Fail("model", "模型", "没有 model.default", "hermes model，或 aivet setup")
	}
	if r.custom && len(r.models) > 0 && !probe.HasModel(r.models, r.model) {
		b.Warn("model_declared", "模型已声明", fmt.Sprintf("%s 不在 providers.%s.models 里", r.model, r.provName), "hermes 会拿不到上下文长度等元数据；把它加进 models 段")
	}
	if r.custom && r.baseURL == "" {
		b.Fail("base_url", "base_url", "自定义提供方没写 base_url", "")
	}
	switch {
	case r.key != "":
		b.OK("auth", "key", fmt.Sprintf("%s（%s）", probe.MaskKey(r.key), r.keySource))
	case r.custom && r.keyEnv != "":
		b.Fail("auth", "key", fmt.Sprintf("key_env = %s，但 shell 和 ~/.hermes/.env 里都没有", r.keyEnv), fmt.Sprintf("往 ~/.hermes/.env 加一行 %s=你的key", r.keyEnv))
	case r.custom:
		b.Fail("auth", "key", "providers 段里既没 key_env 也没 api_key", "加 key_env: XXX 并在 ~/.hermes/.env 写 XXX=…")
	default:
		b.Warn("auth", "key", "没在环境变量 / .env 里找到这个提供方的 key", "hermes 可能用了它自己的凭证池（hermes auth）；--live 实测")
	}
	if b.HasFail() {
		return b.Checks()
	}
	if r.key != "" && r.baseURL != "" {
		proto := probe.ChatCompletions
		if strings.Contains(r.apiMode, "responses") {
			proto = probe.Responses
		} else if r.apiMode == "anthropic" {
			proto = probe.Anthropic
		}
		harness.ProbeGateway(c, b, probe.Endpoint{BaseURL: r.baseURL, Key: r.key, Protocol: proto}, r.model)
	}
	harness.LiveRun(c, b, "Hermes", d.Path, "-z", harness.LivePrompt)
	return b.Checks()
}

func (H) Fixers() []harness.Fixer { return nil }

const providerName = "gateway"
const keyEnvName = "AIVET_GATEWAY_KEY"

// Configure 写 providers.gateway + model 段，key 进 ~/.hermes/.env。
func (H) Configure(c *harness.Context, p harness.Plan) (written, skipped []string, err error) {
	cfgPath := filepath.Join(c.Home, ".hermes", "config.yaml")
	cfg := map[string]any{}
	if err := probe.ReadYAML(cfgPath, &cfg); err != nil && !probe.IsNotExist(err) {
		return nil, nil, err
	}
	providers, _ := cfg["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	if _, has := providers[providerName]; has && !p.Force {
		return nil, []string{cfgPath}, nil
	}
	base := probe.NormalizeBase(p.BaseURL)
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	providers[providerName] = map[string]any{
		"name":          "aivet 配置的网关",
		"base_url":      base,
		"api_mode":      "chat_completions",
		"key_env":       keyEnvName,
		"default_model": p.Model,
		"models": map[string]any{
			p.Model: map[string]any{"context_length": 128000, "max_completion_tokens": 32000},
		},
	}
	cfg["providers"] = providers
	// 统一写成新 schema；旧的顶层 provider/model 字段删掉，避免两套打架。
	delete(cfg, "provider")
	cfg["model"] = map[string]any{"default": p.Model, "provider": providerName}
	if _, err := probe.Backup(cfgPath); err != nil {
		return nil, nil, err
	}
	if err := probe.WriteYAML(cfgPath, cfg); err != nil {
		return nil, nil, err
	}
	envPath := filepath.Join(c.Home, ".hermes", ".env")
	if err := probe.UpsertDotenv(envPath, keyEnvName, p.Key); err != nil {
		return nil, nil, err
	}
	return []string{cfgPath, envPath}, nil, nil
}

// LaunchArgs：hermes 的交互模式不接受首条提示，用单次问答模式接手。
func (H) LaunchArgs(prompt string) []string { return []string{"hermes", "chat", "-q", prompt} }
