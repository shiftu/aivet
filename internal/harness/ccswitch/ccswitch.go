// Package ccswitch 体检 cc-switch。
//
// 它不是 harness，是「切换器」：把各工具的配置存在自己的库里，切换时写回原生文件。
// 工具启动时只读原生文件，不知道 cc-switch 存在 —— 所以原生文件是唯一真相，
// cc-switch 只是它的一个写入者。
//
// 政策（aivet 的立场）：原生优先，官方登录为正，cc-switch 是 fallback。
// 官方能用时 cc-switch 可以空着，也可以存一份备用不启用；原生坏了，才轮到
// cc-switch 里存的那份顶上 —— 而且顶上之前先探一下它通不通，别从一个坏的换到另一个坏的。
//
// 所以这一节回答的是「谁在管、原生这份能不能跑」，而不只是「同步没同步」。
// 「能不能跑」来自那件工具自己的体检结论（harness.Posture），这里不重读原生文件去猜。
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

// apps 是 cc-switch 管、aivet 也查的那几件工具。
var apps = []string{"claude", "codex", "hermes"}

// H 实现 harness.Harness。
type H struct{}

func (H) ID() string    { return "ccswitch" }
func (H) Label() string { return "cc-switch" }

// Correlates 声明要对照哪些工具的姿态。
func (H) Correlates() []string { return apps }

func (H) Detect(c *harness.Context) harness.Detection {
	dir := c.Path("ccswitch.home")
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

// provider 是 cc-switch 库里的一条记录。
type provider struct {
	ID      string
	Name    string
	BaseURL string // 从 settings_config 里抠出来的 base_url；官方档为空
	Current bool
	cfg     string // settings_config 原文（只有 sqlite 路径有），探测时从里面抠 key
}

func (h H) Check(c *harness.Context, d harness.Detection) []report.Check {
	b := harness.NewBuilder(h.ID())
	dir := c.Path("ccswitch.home")
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

	provs := readProviders(c, dir, d.Path)
	if provs == nil {
		b.Warn("db", "provider 库", "既没有 sqlite3 命令也没有 cc-switch CLI，读不到它存了谁", "装 sqlite3（macOS 自带；Ubuntu: apt install sqlite3）或把 cc-switch CLI 加进 PATH")
		return b.Checks()
	}
	for _, app := range apps {
		p, seen := c.PostureOf(app)
		if !seen {
			// 正常不会到这里（main 会 Prime）；真到了就当它装着、什么都没探。
			p = harness.Posture{Installed: true}
		}
		v := judge(app, p, provs[app], func(pr provider) (probeState, string) { return probeCandidate(c, app, pr) })
		switch v.st {
		case report.Skip:
			b.Skip("current."+app, app, v.detail)
		case report.Warn:
			b.Warn("current."+app, app, v.detail, v.hint)
		default:
			b.OK("current."+app, app, v.detail)
		}
	}
	return b.Checks()
}

// readProviders 读各 app 的全部 provider；优先 sqlite3，其次 CLI 表格。读不到返回 nil。
func readProviders(c *harness.Context, dir, cli string) map[string][]provider {
	if sq, ok := probe.Which("sqlite3"); ok {
		db := filepath.Join(dir, "cc-switch.db")
		out, err := probe.Run(c.Ctx, 10*time.Second, "", sq, "-readonly", "-json", "-cmd", ".timeout 2000", db,
			"select app_type, id, name, is_current, settings_config from providers order by sort_index, id")
		if err == nil && strings.TrimSpace(out) != "" {
			var rows []struct {
				App     string `json:"app_type"`
				ID      string `json:"id"`
				Name    string `json:"name"`
				Current int    `json:"is_current"`
				Cfg     string `json:"settings_config"`
			}
			if json.Unmarshal([]byte(out), &rows) == nil {
				m := map[string][]provider{}
				for _, r := range rows {
					m[r.App] = append(m[r.App], provider{ID: r.ID, Name: r.Name, Current: r.Current == 1,
						BaseURL: baseFromSettings(r.App, r.Cfg), cfg: r.Cfg})
				}
				return m
			}
		}
	}
	if cli == "" {
		return nil
	}
	m := map[string][]provider{}
	for _, app := range apps {
		out, err := probe.Run(context.Background(), 15*time.Second, "", cli, "provider", "list", "--app", app)
		if err != nil {
			continue
		}
		m[app] = parseRows(out)
	}
	return m
}

var rowRe = regexp.MustCompile(`^\s*│\s*(✓?)\s*┆\s*(\S+)\s*┆\s*(.*?)\s*┆\s*(\S+)\s*│`)

// parseRows 从 CLI 表格里读每一行；带 ✓ 的是当前生效的。
func parseRows(table string) []provider {
	var out []provider
	for _, line := range strings.Split(table, "\n") {
		m := rowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		base := m[4]
		if base == "N/A" {
			base = ""
		}
		out = append(out, provider{ID: m[2], Name: m[3], BaseURL: base, Current: m[1] == "✓"})
	}
	return out
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

// keyFromSettings 从 settings_config 里抠出探测要用的 key。
// hermes 存的是 key_env（key 本身在 ~/.hermes/.env），要按名字去找。
func keyFromSettings(c *harness.Context, app, cfg string) string {
	var m map[string]any
	if json.Unmarshal([]byte(cfg), &m) != nil {
		return ""
	}
	str := func(mm map[string]any, k string) string {
		v, _ := mm[k].(string)
		return v
	}
	sub := func(k string) map[string]any { v, _ := m[k].(map[string]any); return v }
	switch app {
	case "claude":
		if v := str(sub("env"), "ANTHROPIC_AUTH_TOKEN"); v != "" {
			return v
		}
		return str(sub("env"), "ANTHROPIC_API_KEY")
	case "codex":
		return str(sub("auth"), "OPENAI_API_KEY")
	case "hermes":
		if name := str(m, "key_env"); name != "" {
			if v := c.Env(name); v != "" {
				return v
			}
			return probe.ParseDotenv(c.Path("hermes.env"))[name]
		}
		return str(m, "api_key")
	}
	return ""
}

// probeState 是对一个备选 provider 的探测结论。
type probeState int

const (
	probeUnknown     probeState = iota // 没探（离线 / 拿不到 key）
	probeReachable                     // 清单拉得到
	probeUnreachable                   // 不通
)

// probeCandidate 探 cc-switch 里存的那个 provider 通不通 —— 推荐用户切过去之前，
// 得先知道它不是另一个坏的。只探到「清单拉得到」为止，不发真请求。
func probeCandidate(c *harness.Context, app string, p provider) (probeState, string) {
	if c.Offline {
		return probeUnknown, "离线没探"
	}
	key := keyFromSettings(c, app, p.cfg)
	if key == "" {
		return probeUnknown, "cc-switch 里没存 key，探不了"
	}
	proto := probe.ChatCompletions
	if app == "claude" {
		proto = probe.Anthropic
	}
	ids, pr := c.Gateways.Models(c.Ctx, probe.Endpoint{BaseURL: p.BaseURL, Key: key, Protocol: proto})
	switch {
	case pr.OK:
		return probeReachable, fmt.Sprintf("探过是通的（%d 个模型）", len(ids))
	case pr.Status == 404 || pr.Status == 405:
		return probeReachable, "探过能连上（没有清单接口）"
	default:
		return probeUnreachable, "也探不通（" + pr.Detail + "）"
	}
}

func (H) Fixers() []harness.Fixer { return nil }
