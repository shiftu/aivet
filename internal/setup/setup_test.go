package setup_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/harness/claude"
	"github.com/shiftu/aivet/internal/harness/codex"
	"github.com/shiftu/aivet/internal/harness/dsh"
	"github.com/shiftu/aivet/internal/harness/hermes"
	"github.com/shiftu/aivet/internal/harness/pi"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
	"github.com/shiftu/aivet/internal/setup"
	"github.com/shiftu/aivet/internal/ui"
)

// fakeGateway 模拟一个 OpenAI 兼容网关，只认 key "k1"、模型 "m-a"。
func fakeGateway(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k1" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"m-a"},{"id":"m-b"}]}`))
		case "/v1/chat/completions", "/v1/responses", "/v1/messages":
			w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestSetupThenCheckAllGreen(t *testing.T) {
	srv := fakeGateway(t)
	defer srv.Close()
	home := t.TempDir()
	all := []harness.Harness{claude.H{}, codex.H{}, hermes.H{}, pi.H{}, dsh.H{}}
	c := &harness.Context{Ctx: context.Background(), Home: home, Env: func(string) string { return "" }, Gateways: probe.NewGatewayCache()}
	pr := ui.Printer{W: os.Stderr, P: ui.Plain(), Wid: 80}

	err := setup.Run(c, all, pr, strings.NewReader(""), setup.Options{
		BaseURL: srv.URL, Key: "k1", Model: "m-a", Yes: true,
		Tools: []string{"claude", "codex", "hermes", "pi", "dsh"},
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, p := range []string{".claude/settings.json", ".claude.json", ".codex/config.toml", ".codex/auth.json", ".hermes/config.yaml", ".hermes/.env", ".pi/agent/models.json", ".pi/agent/settings.json", ".dsh/settings.yaml", ".dsh/.credentials.yaml"} {
		if !probe.Exists(filepath.Join(home, p)) {
			t.Errorf("没写 %s", p)
		}
	}
	// 用假的「已安装」跑体检：所有工具都应全绿（网关缓存要新的，避免缓存的 401）。
	c.Gateways = probe.NewGatewayCache()
	for _, h := range all {
		checks := h.Check(c, harness.Detection{Installed: true, Path: "/nonexistent/" + h.ID()})
		for _, ch := range checks {
			if ch.Status == report.Fail {
				t.Errorf("%s: %s → %s (%s)", h.ID(), ch.Title, ch.Detail, ch.Hint)
			}
		}
	}
	// 幂等：不带 --force 再跑一次应全部跳过、不覆盖。
	raw1, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err := setup.Run(c, all, pr, strings.NewReader(""), setup.Options{BaseURL: srv.URL, Key: "k1", Model: "m-a", Yes: true, Tools: []string{"codex"}}); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if string(raw1) != string(raw2) {
		t.Fatal("没有 --force 不该改文件")
	}
}

func TestSetupRejectsBadKey(t *testing.T) {
	srv := fakeGateway(t)
	defer srv.Close()
	c := &harness.Context{Ctx: context.Background(), Home: t.TempDir(), Env: func(string) string { return "" }, Gateways: probe.NewGatewayCache()}
	pr := ui.Printer{W: os.Stderr, P: ui.Plain(), Wid: 80}
	err := setup.Run(c, nil, pr, strings.NewReader(""), setup.Options{BaseURL: srv.URL, Key: "wrong", Model: "m-a", Yes: true})
	if err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("expected key rejection, got %v", err)
	}
}

func TestCodexWireAPIFix(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte("model = \"x\"\nmodel_provider = \"g\"\n\n[model_providers.g]\nbase_url = \"http://g/v1\"\nwire_api = \"chat\"\nenv_key = \"GK\"\n"), 0o600)
	c := &harness.Context{Ctx: context.Background(), Home: home, Env: func(k string) string {
		if k == "GK" {
			return "k1"
		}
		return ""
	}, Gateways: probe.NewGatewayCache(), Offline: true}
	checks := codex.H{}.Check(c, harness.Detection{Installed: true, Version: "codex-cli 0.140.0"})
	var found bool
	for _, ch := range checks {
		if ch.FixID == "codex.wire_api" && ch.Status == report.Fail {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报 wire_api 故障: %+v", checks)
	}
	_, f, ok := harness.FindFixer([]harness.Harness{codex.H{}}, "codex.wire_api")
	if !ok {
		t.Fatal("fixer 不存在")
	}
	if _, err := f.Apply(c, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), `wire_api = "responses"`) || !strings.Contains(string(raw), `env_key = "GK"`) {
		t.Fatalf("fix 结果不对:\n%s", raw)
	}
	if m, _ := filepath.Glob(cfg + ".aivet-bak-*"); len(m) != 1 {
		t.Fatal("应留一个备份")
	}
}

// wire_api = "chat" 该报故障还是提醒，取决于用户装的是哪一版 —— 无条件断言
// 会在两头出错：旧版用户被一条他那儿根本不成立的「故障」挡住，
// 而 codex 万一把 chat 加回来，这条就成了纯误报。
func TestCodexWireAPIIsVersionAware(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		want    report.Status
	}{
		{"新版已删除 → 故障", "codex-cli 0.140.0", report.Fail},
		{"旧版还支持 → 提醒", "codex-cli 0.130.2", report.Warn},
		{"读不出版本 → 提醒，不瞎断言", "", report.Warn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			cfg := filepath.Join(home, ".codex", "config.toml")
			os.MkdirAll(filepath.Dir(cfg), 0o755)
			os.WriteFile(cfg, []byte("model = \"x\"\nmodel_provider = \"g\"\n\n[model_providers.g]\nbase_url = \"http://g/v1\"\nwire_api = \"chat\"\nenv_key = \"GK\"\n"), 0o600)
			c := &harness.Context{Ctx: context.Background(), Home: home, Env: func(k string) string {
				if k == "GK" {
					return "k1"
				}
				return ""
			}, Gateways: probe.NewGatewayCache(), Offline: true}
			for _, ch := range (codex.H{}).Check(c, harness.Detection{Installed: true, Version: tc.version}) {
				if ch.ID != "codex.wire_api" {
					continue
				}
				if ch.Status != tc.want {
					t.Fatalf("版本 %q：想要 %s，得到 %s（%s）", tc.version, tc.want, ch.Status, ch.Detail)
				}
				// 三种情况都得留一条能自动修的出路。
				if ch.FixID != "codex.wire_api" {
					t.Errorf("版本 %q：这一项应该可自动修复，实际 FixID=%q", tc.version, ch.FixID)
				}
				return
			}
			t.Fatalf("版本 %q：没有 codex.wire_api 这一项", tc.version)
		})
	}
}
