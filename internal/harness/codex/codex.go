// Package codex 体检 OpenAI Codex CLI。
//
// 关键事实（都在检查项里体现）：
//   - 0.137+ 删掉了 wire_api="chat"，只认 Responses API —— 网关必须有 /v1/responses；
//   - 自定义 provider 的 key 有两条路：env_key 指向环境变量，
//     或 requires_openai_auth=true 让它读 ~/.codex/auth.json 里的 OPENAI_API_KEY；
//   - 「Model metadata for … not found」是已知噪声，不算故障。
package codex

import (
	"fmt"
	"os"
	"path/filepath"

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
	return harness.Detection{Installed: true, Path: p, Version: probe.Version(p, "--version")}
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
	cfgErr    error
	provName  string
	prov      provider
	provKnown bool
	key       string
	keySource string
	chatgpt   bool
}

func resolve(c *harness.Context) resolved {
	r := resolved{cfgPath: filepath.Join(c.Home, ".codex", "config.toml")}
	r.cfgErr = probe.ReadTOML(r.cfgPath, &r.cfg)
	r.provName = r.cfg.ModelProvider
	if r.provName == "" {
		r.provName = "openai"
	}
	r.prov, r.provKnown = r.cfg.ModelProviders[r.provName]
	if !r.provKnown && r.provName == "openai" {
		r.prov, r.provKnown = provider{Name: "OpenAI", BaseURL: "https://api.openai.com/v1", WireAPI: "responses", RequiresOpenAIAuth: true}, true
	}
	var a auth
	_ = probe.ReadJSON(filepath.Join(c.Home, ".codex", "auth.json"), &a)
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
	switch r.prov.WireAPI {
	case "chat":
		b.FailFix("wire_api", "wire_api", "wire_api = \"chat\"——codex 0.137 起已删除，启动即报错", "改成 \"responses\"（前提：网关有 /v1/responses）", "codex.wire_api")
	case "", "responses":
		b.OK("wire_api", "wire_api", "responses")
	default:
		b.Fail("wire_api", "wire_api", "未知值 "+r.prov.WireAPI, "只能是 responses")
	}
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
		harness.ProbeGateway(c, b, probe.Endpoint{BaseURL: r.prov.BaseURL, Key: r.key, Protocol: probe.Responses}, r.cfg.Model)
	}
	harness.LiveRun(c, b, "Codex", d.Path, "exec", "--skip-git-repo-check", harness.LivePrompt)
	return b.Checks()
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
	cfgPath := filepath.Join(c.Home, ".codex", "config.toml")
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

	authPath := filepath.Join(c.Home, ".codex", "auth.json")
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
