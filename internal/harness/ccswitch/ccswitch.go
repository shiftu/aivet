// Package ccswitch 体检 cc-switch。
//
// 它不是 harness，是「切换器」：把各工具的配置存在自己的库里，切换时写回原生文件。
// 所以这里查的是**漂移**：cc-switch 认为当前生效的 provider，和原生文件里实际的是否一致。
// 不一致通常意味着用户手改了原生文件、或 cc-switch 没写回成功——两种都会让人「明明切了却没生效」。
//
// 读库优先用 sqlite3 命令行（macOS 自带）；没有就退回解析 `cc-switch provider list` 的表格。
package ccswitch

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// H 实现 harness.Harness。
type H struct{}

func (H) ID() string    { return "ccswitch" }
func (H) Label() string { return "cc-switch" }

func (H) Detect(c *harness.Context) harness.Detection {
	dir := filepath.Join(c.Home, ".cc-switch")
	p, ok := probe.Which("cc-switch")
	if !ok && !probe.Exists(dir) {
		return harness.Detection{}
	}
	d := harness.Detection{Installed: true, Path: p}
	if ok {
		d.Version, d.Broken = probe.VersionOr(p, "--version")
	} else {
		d.Version = "仅数据目录（GUI）"
	}
	return d
}

// current 是 cc-switch 里某个 app 当前生效的 provider。
type current struct {
	ID      string
	Name    string
	BaseURL string // 从 settings_config 里抠出来的 base_url（可能为空）
}

func (h H) Check(c *harness.Context, d harness.Detection) []report.Check {
	b := harness.NewBuilder(h.ID())
	dir := filepath.Join(c.Home, ".cc-switch")
	var settings map[string]any
	if err := probe.ReadJSON(filepath.Join(dir, "settings.json"), &settings); err != nil {
		if probe.IsNotExist(err) {
			b.Warn("settings", "settings.json", "不存在——cc-switch 还没启动过", "打开一次 cc-switch 让它初始化")
		} else {
			b.Fail("settings", "settings.json", err.Error(), "")
		}
		return b.Checks()
	}
	b.OK("settings", "settings.json", filepath.Join(dir, "settings.json"))

	cur := readCurrent(c, dir, d.Path)
	if cur == nil {
		b.Warn("db", "当前 provider", "既没有 sqlite3 命令也没有 cc-switch CLI，读不到它选了谁", "装 sqlite3（macOS 自带；Ubuntu: apt install sqlite3）或把 cc-switch CLI 加进 PATH")
		return b.Checks()
	}
	for _, app := range []string{"claude", "codex", "hermes"} {
		cp, ok := cur[app]
		if !ok {
			b.OK("current."+app, app, "cc-switch 里没有选中的 provider（不接管）")
			continue
		}
		actual := actualBase(c, app)
		switch {
		case cp.BaseURL == "" || actual == "":
			b.OK("current."+app, app, fmt.Sprintf("当前 %s（%s）", cp.ID, orDash(cp.BaseURL)))
		case probe.NormalizeBase(cp.BaseURL) == probe.NormalizeBase(actual):
			b.OK("current."+app, app, fmt.Sprintf("当前 %s · 与原生配置一致（%s）", cp.ID, actual))
		default:
			b.Warn("current."+app, app, fmt.Sprintf("cc-switch 选的是 %s（%s），但原生配置里是 %s", cp.ID, cp.BaseURL, actual),
				fmt.Sprintf("原生文件被手改过，或 cc-switch 没写回。以 cc-switch 为准就：cc-switch use %s --app %s；以手改为准就在 cc-switch 里另存一个 provider", cp.ID, app))
		}
	}
	return b.Checks()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// readCurrent 读各 app 当前 provider；优先 sqlite3，其次 CLI 表格。
func readCurrent(c *harness.Context, dir, cli string) map[string]current {
	if sq, ok := probe.Which("sqlite3"); ok {
		db := filepath.Join(dir, "cc-switch.db")
		out, err := probe.Run(c.Ctx, 10*time.Second, "", sq, "-readonly", "-json", "-cmd", ".timeout 2000", db,
			"select app_type, id, name, settings_config from providers where is_current=1")
		if err == nil && strings.TrimSpace(out) != "" {
			var rows []struct {
				App, ID, Name, Settings string `json:"-"`
				AppType                 string `json:"app_type"`
				IDv                     string `json:"id"`
				Namev                   string `json:"name"`
				Cfg                     string `json:"settings_config"`
			}
			if json.Unmarshal([]byte(out), &rows) == nil {
				m := map[string]current{}
				for _, r := range rows {
					m[r.AppType] = current{ID: r.IDv, Name: r.Namev, BaseURL: baseFromSettings(r.AppType, r.Cfg)}
				}
				return m
			}
		}
	}
	if cli == "" {
		return nil
	}
	m := map[string]current{}
	for _, app := range []string{"claude", "codex", "hermes"} {
		out, err := probe.Run(context.Background(), 15*time.Second, "", cli, "provider", "list", "--app", app)
		if err != nil {
			continue
		}
		if cp, ok := parseCurrentRow(out); ok {
			m[app] = cp
		}
	}
	return m
}

var rowRe = regexp.MustCompile(`^\s*│\s*✓\s*┆\s*(\S+)\s*┆\s*(.*?)\s*┆\s*(\S+)\s*│`)

// parseCurrentRow 从 CLI 表格里找带 ✓ 的那一行。
func parseCurrentRow(table string) (current, bool) {
	for _, line := range strings.Split(table, "\n") {
		if m := rowRe.FindStringSubmatch(line); m != nil {
			base := m[3]
			if base == "N/A" {
				base = ""
			}
			return current{ID: m[1], Name: m[2], BaseURL: base}, true
		}
	}
	return current{}, false
}

// baseFromSettings 从 cc-switch 存的 settings_config JSON 里抠 base_url。
func baseFromSettings(app, cfg string) string {
	var m map[string]any
	if json.Unmarshal([]byte(cfg), &m) != nil {
		return ""
	}
	switch app {
	case "claude":
		if env, ok := m["env"].(map[string]any); ok {
			if v, ok := env["ANTHROPIC_BASE_URL"].(string); ok {
				return v
			}
		}
	case "codex":
		if t, ok := m["config"].(string); ok {
			re := regexp.MustCompile(`(?m)^\s*base_url\s*=\s*"([^"]+)"`)
			if mm := re.FindStringSubmatch(t); mm != nil {
				return mm[1]
			}
		}
	case "hermes":
		if v, ok := m["base_url"].(string); ok {
			return v
		}
	}
	return ""
}

// actualBase 读原生配置里实际生效的 base_url。
func actualBase(c *harness.Context, app string) string {
	switch app {
	case "claude":
		if v := c.Env("ANTHROPIC_BASE_URL"); v != "" {
			return v
		}
		var s map[string]any
		if probe.ReadJSON(filepath.Join(c.Home, ".claude", "settings.json"), &s) == nil {
			if env, ok := s["env"].(map[string]any); ok {
				if v, ok := env["ANTHROPIC_BASE_URL"].(string); ok {
					return v
				}
			}
		}
	case "codex":
		var cfg struct {
			ModelProvider  string                    `toml:"model_provider"`
			ModelProviders map[string]map[string]any `toml:"model_providers"`
		}
		if probe.ReadTOML(filepath.Join(c.Home, ".codex", "config.toml"), &cfg) == nil {
			if p, ok := cfg.ModelProviders[cfg.ModelProvider]; ok {
				if v, ok := p["base_url"].(string); ok {
					return v
				}
			}
		}
	case "hermes":
		var cfg map[string]any
		if probe.ReadYAML(filepath.Join(c.Home, ".hermes", "config.yaml"), &cfg) == nil {
			prov := ""
			if m, ok := cfg["model"].(map[string]any); ok {
				prov, _ = m["provider"].(string)
			} else if s, ok := cfg["provider"].(string); ok {
				prov = s
			}
			if ps, ok := cfg["providers"].(map[string]any); ok {
				if p, ok := ps[prov].(map[string]any); ok {
					if v, ok := p["base_url"].(string); ok {
						return v
					}
				}
			}
		}
	}
	return ""
}

func (H) Fixers() []harness.Fixer { return nil }
