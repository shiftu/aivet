package pi

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

func writeFiles(t *testing.T, home, base string, headers map[string]any) string {
	t.Helper()
	mp := filepath.Join(home, ".pi", "agent", "models.json")
	prov := map[string]any{
		"name": "keep me", "baseUrl": base, "api": "openai-completions", "apiKey": "k",
		"compat": map[string]any{"supportsStore": false}, // aivet 不认识的字段，fixer 必须保住
		"models": []any{map[string]any{"id": "m1", "name": "M1", "reasoning": false, "contextWindow": 1000, "maxTokens": 100}},
	}
	if headers != nil {
		prov["headers"] = headers
	}
	if err := probe.WriteJSON(mp, map[string]any{"providers": map[string]any{"gw": prov}}); err != nil {
		t.Fatal(err)
	}
	if err := probe.WriteJSON(filepath.Join(home, ".pi", "agent", "settings.json"), map[string]any{"defaultProvider": "gw", "defaultModel": "m1"}); err != nil {
		t.Fatal(err)
	}
	return mp
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

func TestResolveReadsHeaders(t *testing.T) {
	home := t.TempDir()
	writeFiles(t, home, "https://llm.example.com/v1", map[string]any{"User-Agent": "Mozilla/5.0 x"})
	c, _ := ctxFor(home)
	if r := resolve(c); r.prov.Headers["User-Agent"] != "Mozilla/5.0 x" {
		t.Fatalf("没读到 headers：%+v", r.prov)
	}
}

func TestCheckReportsHeadersOnlyForPublicGateway(t *testing.T) {
	home := t.TempDir()
	c, _ := ctxFor(home)
	c.Offline = true
	writeFiles(t, home, "https://llm.example.com/v1", nil)
	got := find(t, (H{}).Check(c, harness.Detection{Installed: true}), "pi.headers")
	if got.Status != report.OK || !strings.Contains(got.Detail, "aivet fix pi.default_headers") {
		t.Fatalf("%+v", got)
	}
	writeFiles(t, home, "http://127.0.0.1:7421/v1", nil)
	for _, ch := range (H{}).Check(c, harness.Detection{Installed: true}) {
		if ch.ID == "pi.headers" {
			t.Fatalf("本地网关不该报这一条：%+v", ch)
		}
	}
}

func TestCheckPointsToFixerWhenCloudflareBlocks(t *testing.T) {
	home := t.TempDir()
	writeFiles(t, home, cfServer(t), nil)
	c, _ := ctxFor(home)
	got := find(t, (H{}).Check(c, harness.Detection{Installed: true}), "pi.gateway.reach")
	if got.Status != report.Fail || got.FixID != "pi.default_headers" {
		t.Fatalf("%+v", got)
	}
}

func TestFixDefaultHeadersDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	mp := writeFiles(t, home, cfServer(t), nil)
	before, _ := os.ReadFile(mp)
	c, log := ctxFor(home)
	changed, err := fixDefaultHeaders(c, true)
	if err != nil || len(changed) != 1 || changed[0] != mp {
		t.Fatalf("%v %v", changed, err)
	}
	if after, _ := os.ReadFile(mp); string(before) != string(after) {
		t.Fatal("dry-run 不该改文件")
	}
	if !strings.Contains(strings.Join(*log, "\n"), "providers.gw.headers ←") {
		t.Fatalf("%q", *log)
	}
}

func TestFixDefaultHeadersKeepsUnknownFieldsAndReverifies(t *testing.T) {
	home := t.TempDir()
	mp := writeFiles(t, home, cfServer(t), map[string]any{"X-Foo": "bar"})
	c, log := ctxFor(home)
	(H{}).Check(c, harness.Detection{Installed: true}) // 让缓存里先有不带头的 403
	if _, err := fixDefaultHeaders(c, false); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := probe.ReadJSON(mp, &raw); err != nil {
		t.Fatal(err)
	}
	prov := raw["providers"].(map[string]any)["gw"].(map[string]any)
	h := prov["headers"].(map[string]any)
	if h["User-Agent"] != harness.BrowserUA || h["X-Client-Name"] != "pi coding agent" || h["X-Foo"] != "bar" {
		t.Fatalf("%+v", h)
	}
	if _, ok := prov["compat"]; !ok || prov["name"] != "keep me" {
		t.Fatalf("aivet 不认识的字段丢了：%+v", prov)
	}
	if !strings.Contains(strings.Join(*log, "\n"), "ok: 重验：带上自定义请求头后通了") {
		t.Fatalf("%q", *log)
	}
	c.Gateways = probe.NewGatewayCache()
	for _, ch := range (H{}).Check(c, harness.Detection{Installed: true}) {
		if ch.Status == report.Fail {
			t.Fatalf("修完不该还有故障：%+v", ch)
		}
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
		var mf modelsFile
		if err := probe.ReadJSON(filepath.Join(home, ".pi", "agent", "models.json"), &mf); err != nil {
			t.Fatal(err)
		}
		h := mf.Providers["gateway"].Headers
		if (len(h) > 0) != tc.want {
			t.Fatalf("%s：headers=%v，期望存在=%v", tc.base, h, tc.want)
		}
	}
}
