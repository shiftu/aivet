package setup_test

// 这一组测试盯的是 aivet 最危险的失效方式：目标工具升级改了配置结构之后，
// 解析器不会报错，只会把字段留成零值 —— 于是每一条检查都在核对空值，
// 然后一路绿灯。报告写着「一切正常」，实际什么都没查，而且没人会来问。
//
// 所以这里不只断言「有没有报出来」，更断言「有没有闭嘴」：
// 读不懂配置之后还继续吐绿灯，比不报警更糟。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/harness/dsh"
	"github.com/shiftu/aivet/internal/harness/hermes"
	"github.com/shiftu/aivet/internal/harness/pi"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

func ctxAt(home string) *harness.Context {
	return &harness.Context{
		Ctx: context.Background(), Home: home, Offline: true,
		Env:      func(string) string { return "" },
		Gateways: probe.NewGatewayCache(),
	}
}

func writeAt(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func statusOf(checks []report.Check, id string) (report.Check, bool) {
	for _, c := range checks {
		if c.ID == id {
			return c, true
		}
	}
	return report.Check{}, false
}

func TestSchemaDriftIsReportedAndStopsTheGreenLights(t *testing.T) {
	for _, tc := range []struct {
		name    string
		file    string // 相对 home 的路径
		body    string // 一份 aivet 完全不认识的配置（假想的下一代 schema）
		check   func(*harness.Context) []report.Check
		driftID string
	}{
		{
			name: "hermes 换了配置结构",
			file: ".hermes/config.yaml",
			body: "llmConfig:\n  defaultModel: gpt-9\n  vendor: acme\n",
			check: func(c *harness.Context) []report.Check {
				return hermes.H{}.Check(c, harness.Detection{Installed: true})
			},
			driftID: "hermes.schema",
		},
		{
			name: "pi 换了 models.json 结构",
			file: ".pi/agent/models.json",
			body: `{"vendors": {"acme": {"endpoint": "https://acme/v1"}}}`,
			check: func(c *harness.Context) []report.Check {
				return pi.H{}.Check(c, harness.Detection{Installed: true})
			},
			driftID: "pi.schema",
		},
		{
			name: "dsh 换了配置结构",
			file: ".dsh/settings.yaml",
			body: "modelRouting:\n  default: acme/fast\n",
			check: func(c *harness.Context) []report.Check {
				return dsh.H{}.Check(c, harness.Detection{Installed: true})
			},
			driftID: "dsh.schema",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			writeAt(t, filepath.Join(home, tc.file), tc.body)
			checks := tc.check(ctxAt(home))

			drift, ok := statusOf(checks, tc.driftID)
			if !ok {
				t.Fatalf("读不懂配置却没报出来。检查项：%+v", checks)
			}
			if drift.Status != report.Warn {
				t.Errorf("schema 漂移该是提醒（能不能用还没定论），得到 %s", drift.Status)
			}
			// 提示里必须指出两条真正的出路，否则用户只知道「有问题」，不知道下一步。
			for _, want := range []string{"--live", "knowledge"} {
				if !strings.Contains(drift.Hint, want) {
					t.Errorf("提示里应提到 %s：%q", want, drift.Hint)
				}
			}
			// 关键：读不懂之后不许再吐绿灯。
			for _, c := range checks {
				if c.ID == tc.driftID {
					continue
				}
				if c.Status == report.OK && strings.Contains(c.ID, "model") {
					t.Errorf("读不懂配置之后还在报 %s 通过 —— 这正是「查了等于没查」", c.ID)
				}
			}
		})
	}
}

// 认得出来的配置不许触发漂移提醒 —— 一个只在真出事时才说话的检查项，
// 平时吵起来就没人信了。
func TestFamiliarConfigDoesNotTripDrift(t *testing.T) {
	home := t.TempDir()
	writeAt(t, filepath.Join(home, ".hermes/config.yaml"),
		"model:\n  default: m1\n  provider: gw\nproviders:\n  gw:\n    base_url: http://g/v1\n    api_mode: chat_completions\n    key_env: GK\n")
	for _, c := range (hermes.H{}).Check(ctxAt(home), harness.Detection{Installed: true}) {
		if c.ID == "hermes.schema" {
			t.Fatalf("正常配置被误报成 schema 漂移：%+v", c)
		}
	}
}

// 工具搬了配置文件的家，用户在 knowledge.json 里补一条新路径，
// aivet 就该重新认得出来 —— 这是「不必等发版」那个承诺的兑现处。
func TestUserDeclaredPathRecoversFromRelocation(t *testing.T) {
	home := t.TempDir()
	moved := filepath.Join(home, ".config", "hermes", "config.yaml")
	writeAt(t, moved,
		"model:\n  default: m1\n  provider: gw\nproviders:\n  gw:\n    base_url: http://g/v1\n    api_mode: chat_completions\n    key_env: GK\n")

	// 补路径之前：老地方没有文件，aivet 只能报「不存在」。
	before, _ := statusOf((hermes.H{}).Check(ctxAt(home), harness.Detection{Installed: true}), "hermes.config")
	if before.Status != report.Fail {
		t.Fatalf("文件搬走后应报不存在，得到 %s（%s）", before.Status, before.Detail)
	}

	writeAt(t, filepath.Join(home, ".aivet", "knowledge.json"),
		`{"paths": {"hermes.config": ["~/.config/hermes/config.yaml"]}}`)

	after, _ := statusOf((hermes.H{}).Check(ctxAt(home), harness.Detection{Installed: true}), "hermes.config")
	if after.Status != report.OK {
		t.Fatalf("补了路径之后应该重新认得出来，得到 %s（%s）", after.Status, after.Detail)
	}
	if !strings.Contains(after.Detail, filepath.Join(".config", "hermes")) {
		t.Errorf("应该读的是用户声明的那个路径，实际 %s", after.Detail)
	}
}
