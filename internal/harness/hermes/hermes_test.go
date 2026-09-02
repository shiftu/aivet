package hermes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// cfServer：UA 不像浏览器就吐 Cloudflare 拦截页，像了就当正常网关。
func cfServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Mozilla/") {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(403)
			w.Write([]byte(`<html><title>Just a moment...</title></html>`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"m1"}]}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func writeCfg(t *testing.T, home, base string, headers map[string]any) string {
	t.Helper()
	p := filepath.Join(home, ".hermes", "config.yaml")
	prov := map[string]any{
		"base_url": base, "api_mode": "chat_completions", "api_key": "k",
		"models": map[string]any{"m1": map[string]any{"context_length": 1000}},
		"name":   "keep me", // fixer 不该动别的键
	}
	if headers != nil {
		prov["default_headers"] = headers
	}
	cfg := map[string]any{
		"model":     map[string]any{"default": "m1", "provider": "gw"},
		"providers": map[string]any{"gw": prov},
		"agent":     map[string]any{"max_turns": 9},
	}
	if err := probe.WriteYAML(p, cfg); err != nil {
		t.Fatal(err)
	}
	return p
}

func ctxFor(home string) (*harness.Context, *[]string) {
	var log []string
	c := &harness.Context{Ctx: context.Background(), Home: home, Env: func(string) string { return "" },
		Gateways: probe.NewGatewayCache(), Log: func(st, text string) { log = append(log, st+": "+text) }}
	return c, &log
}

func find(t *testing.T, checks []report.Check, id string) report.Check {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("没有 %s：%+v", id, checks)
	return report.Check{}
}

func TestResolveReadsDefaultHeaders(t *testing.T) {
	home := t.TempDir()
	writeCfg(t, home, "https://llm.example.com/v1", map[string]any{"User-Agent": "Mozilla/5.0 x", "X-Client-Name": "Hermes Agent"})
	c, _ := ctxFor(home)
	r := resolve(c)
	if r.headers["User-Agent"] != "Mozilla/5.0 x" || r.headers["X-Client-Name"] != "Hermes Agent" {
		t.Fatalf("没读到 default_headers：%+v", r.headers)
	}
}

func TestCheckReportsHeadersOnlyForPublicGateway(t *testing.T) {
	home := t.TempDir()
	c, _ := ctxFor(home)
	c.Offline = true
	writeCfg(t, home, "https://llm.example.com/v1", nil)
	got := find(t, (H{}).Check(c, harness.Detection{Installed: true}), "hermes.headers")
	if got.Status != report.OK || !strings.Contains(got.Detail, "没配 default_headers") || !strings.Contains(got.Detail, "aivet fix hermes.default_headers") {
		t.Fatalf("没配是中性提示 + 指路：%+v", got)
	}
	writeCfg(t, home, "https://llm.example.com/v1", map[string]any{"User-Agent": "Mozilla/5.0 x"})
	got = find(t, (H{}).Check(c, harness.Detection{Installed: true}), "hermes.headers")
	if got.Status != report.OK || !strings.Contains(got.Detail, "User-Agent=Mozilla/5.0 x") {
		t.Fatalf("配了要如实报出来：%+v", got)
	}
	writeCfg(t, home, "http://127.0.0.1:7421/v1", nil)
	for _, ch := range (H{}).Check(c, harness.Detection{Installed: true}) {
		if ch.ID == "hermes.headers" {
			t.Fatalf("本地网关不该报这一条：%+v", ch)
		}
	}
}

func TestCheckPointsToFixerWhenCloudflareBlocks(t *testing.T) {
	home := t.TempDir()
	writeCfg(t, home, cfServer(t), nil)
	c, _ := ctxFor(home)
	got := find(t, (H{}).Check(c, harness.Detection{Installed: true}), "hermes.gateway.reach")
	if got.Status != report.Fail || got.FixID != "hermes.default_headers" || !strings.Contains(got.Detail, "Cloudflare") {
		t.Fatalf("%+v", got)
	}
}

func TestCheckPassesWhenHeadersAlreadyConfigured(t *testing.T) {
	home := t.TempDir()
	writeCfg(t, home, cfServer(t), map[string]any{"User-Agent": harness.BrowserUA})
	c, _ := ctxFor(home)
	checks := (H{}).Check(c, harness.Detection{Installed: true})
	for _, id := range []string{"hermes.gateway.reach", "hermes.gateway.model", "hermes.gateway.ping"} {
		if got := find(t, checks, id); got.Status != report.OK {
			t.Fatalf("配了头就该按工具的样子探通：%+v", got)
		}
	}
}

func TestFixDefaultHeadersDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	p := writeCfg(t, home, cfServer(t), nil)
	before, _ := os.ReadFile(p)
	c, log := ctxFor(home)
	changed, err := fixDefaultHeaders(c, true)
	if err != nil || len(changed) != 1 || changed[0] != p {
		t.Fatalf("%v %v", changed, err)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("dry-run 不该改文件")
	}
	if entries, _ := os.ReadDir(filepath.Dir(p)); len(entries) != 1 {
		t.Fatalf("dry-run 不该留备份：%v", entries)
	}
	joined := strings.Join(*log, "\n")
	if !strings.Contains(joined, "providers.gw.default_headers ←") || !strings.Contains(joined, "User-Agent=Mozilla/5.0") {
		t.Fatalf("dry-run 要说清将写什么：%q", joined)
	}
}

func TestFixDefaultHeadersWritesAndReverifies(t *testing.T) {
	home := t.TempDir()
	p := writeCfg(t, home, cfServer(t), map[string]any{"X-Foo": "bar"})
	c, log := ctxFor(home)
	// 先探一次，让缓存里留着不带头那次的 403 —— 重验必须绕开它。
	(H{}).Check(c, harness.Detection{Installed: true})
	if _, err := fixDefaultHeaders(c, false); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := probe.ReadYAML(p, &cfg); err != nil {
		t.Fatal(err)
	}
	prov := cfg["providers"].(map[string]any)["gw"].(map[string]any)
	dh, _ := prov["default_headers"].(map[string]any)
	if dh["User-Agent"] != harness.BrowserUA || dh["X-Client-Name"] != "Hermes Agent" {
		t.Fatalf("写错了：%+v", dh)
	}
	if dh["X-Foo"] != "bar" || prov["name"] != "keep me" || cfg["agent"].(map[string]any)["max_turns"] != 9 {
		t.Fatalf("不该动用户原有的键：%+v", cfg)
	}
	joined := strings.Join(*log, "\n")
	if !strings.Contains(joined, "ok: 重验：带上自定义请求头后通了") {
		t.Fatalf("要当场报重验结果：%q", joined)
	}
	if matches, _ := filepath.Glob(p + ".aivet-bak-*"); len(matches) != 1 {
		t.Fatalf("要留备份：%v", matches)
	}
	// 修完再体检：应全绿。
	c.Gateways = probe.NewGatewayCache()
	for _, ch := range (H{}).Check(c, harness.Detection{Installed: true}) {
		if ch.Status == report.Fail {
			t.Fatalf("修完不该还有故障：%+v", ch)
		}
	}
}

func TestFixDefaultHeadersRefusesBuiltinProvider(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".hermes", "config.yaml")
	if err := probe.WriteYAML(p, map[string]any{"model": map[string]any{"default": "x", "provider": "openai"}}); err != nil {
		t.Fatal(err)
	}
	c, _ := ctxFor(home)
	if _, err := fixDefaultHeaders(c, false); err == nil || !strings.Contains(err.Error(), "没有 default_headers 这个配置位") {
		t.Fatalf("内置提供方没有这个配置位，要明说：%v", err)
	}
}

func TestConfigureAddsHeadersOnlyForPublicGateway(t *testing.T) {
	for _, tc := range []struct {
		base string
		want bool
	}{{"https://llm.example.com", true}, {"http://127.0.0.1:7421", false}} {
		home := t.TempDir()
		c, _ := ctxFor(home)
		if _, _, err := (H{}).Configure(c, harness.Plan{BaseURL: tc.base, Key: "k", Model: "m1"}); err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err := probe.ReadYAML(filepath.Join(home, ".hermes", "config.yaml"), &cfg); err != nil {
			t.Fatal(err)
		}
		prov := cfg["providers"].(map[string]any)["gateway"].(map[string]any)
		dh, has := prov["default_headers"].(map[string]any)
		if has != tc.want {
			t.Fatalf("%s：default_headers 存在=%v，期望 %v", tc.base, has, tc.want)
		}
		if tc.want && dh["User-Agent"] != harness.BrowserUA {
			t.Fatalf("%+v", dh)
		}
	}
}
