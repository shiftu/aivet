package harness_test

// Cloudflare bot 拦截和「key 没权限」都是 403，但出路完全不同：
// 前者写个浏览器 UA 就好，后者要换 key。这组测试盯的是 aivet 能不能分清，
// 以及分清之后能不能把用户引到对的修复项上。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// cfServer 模拟挂在 Cloudflare 后面的网关。blockPost 为 true 时只拦 POST（清单能拉，请求被拦）。
func cfServer(t *testing.T, blockPost bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("cf-ray", "8a1b2c3d4e5f-HKG")
		bot := !strings.HasPrefix(r.Header.Get("User-Agent"), "Mozilla/")
		if bot && (!blockPost || r.Method == http.MethodPost) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(403)
			w.Write([]byte(`<html><title>Attention Required! | Cloudflare</title><p>Sorry, you have been blocked</p></html>`))
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

func ctxOnline() *harness.Context {
	return &harness.Context{Ctx: context.Background(), Env: func(string) string { return "" }, Gateways: probe.NewGatewayCache()}
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

func TestBotBlockPointsToFixerWhenToolHasHeaderSlot(t *testing.T) {
	ep := probe.Endpoint{BaseURL: cfServer(t, false), Key: "k", Protocol: probe.ChatCompletions}
	b := harness.NewBuilder("hermes")
	harness.ProbeGatewayWith(ctxOnline(), b, ep, "m1", harness.Bypass{FixID: "hermes.default_headers"})
	got := find(t, b.Checks(), "hermes.gateway.reach")
	if got.Status != report.Fail || got.FixID != "hermes.default_headers" {
		t.Fatalf("要 Fail 且指向 fixer：%+v", got)
	}
	if !strings.Contains(got.Detail, "Cloudflare") || !strings.Contains(got.Hint, "aivet fix hermes.default_headers") {
		t.Fatalf("文案要点名 Cloudflare 和修复命令：%q / %q", got.Detail, got.Hint)
	}
	if strings.Contains(got.Detail, "key 没有这个权限") {
		t.Fatalf("不该再说成 key 权限问题：%q", got.Detail)
	}
}

func TestBotBlockWithoutHeaderSlotStaysHonest(t *testing.T) {
	ep := probe.Endpoint{BaseURL: cfServer(t, false), Key: "k", Protocol: probe.ChatCompletions}
	b := harness.NewBuilder("codex")
	harness.ProbeGateway(ctxOnline(), b, ep, "m1")
	got := find(t, b.Checks(), "codex.gateway.reach")
	if got.Status != report.Fail || got.FixID != "" {
		t.Fatalf("没有配置位就不该给 fixer：%+v", got)
	}
	if !strings.Contains(got.Hint, "--live") {
		t.Fatalf("要提醒 aivet 被拦 ≠ 工具被拦：%q", got.Hint)
	}
}

func TestConfiguredHeadersAreUsedOnSecondTry(t *testing.T) {
	ep := probe.Endpoint{BaseURL: cfServer(t, false), Key: "k", Protocol: probe.ChatCompletions}
	b := harness.NewBuilder("hermes")
	bp := harness.Bypass{Headers: harness.BypassHeaders("Hermes Agent"), FixID: "hermes.default_headers"}
	out := harness.ProbeGatewayWith(ctxOnline(), b, ep, "m1", bp)
	reach := find(t, b.Checks(), "hermes.gateway.reach")
	if reach.Status != report.OK || !strings.Contains(reach.Detail, "改按配置里的请求头") {
		t.Fatalf("配了头就该按工具的样子探通，并说明这一点：%+v", reach)
	}
	if ping := find(t, b.Checks(), "hermes.gateway.ping"); ping.Status != report.OK {
		t.Fatalf("%+v", ping)
	}
	if out.Headers["User-Agent"] != harness.BrowserUA {
		t.Fatalf("要把带头的端点还给调用方：%+v", out.Headers)
	}
}

func TestPostOnlyBlockIsWarnWithFixer(t *testing.T) {
	ep := probe.Endpoint{BaseURL: cfServer(t, true), Key: "k", Protocol: probe.ChatCompletions}
	b := harness.NewBuilder("pi")
	harness.ProbeGatewayWith(ctxOnline(), b, ep, "m1", harness.Bypass{FixID: "pi.default_headers"})
	if reach := find(t, b.Checks(), "pi.gateway.reach"); reach.Status != report.OK {
		t.Fatalf("清单是通的：%+v", reach)
	}
	ping := find(t, b.Checks(), "pi.gateway.ping")
	if ping.Status != report.Warn || ping.FixID != "pi.default_headers" {
		t.Fatalf("清单能拉、POST 被拦：要 Warn 且指向 fixer：%+v", ping)
	}
}

func TestPlainForbiddenIsStillAKeyProblem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare") // 网关挂在 CF 后面，但这是它自己的拒绝
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		w.Write([]byte(`{"error":{"message":"key not allowed"}}`))
	}))
	t.Cleanup(srv.Close)
	ep := probe.Endpoint{BaseURL: srv.URL + "/v1", Key: "k", Protocol: probe.ChatCompletions}
	b := harness.NewBuilder("hermes")
	harness.ProbeGatewayWith(ctxOnline(), b, ep, "m1", harness.Bypass{FixID: "hermes.default_headers"})
	got := find(t, b.Checks(), "hermes.gateway.reach")
	if got.Status != report.Fail || got.FixID != "" || got.Title != "网关认证" {
		t.Fatalf("网关自己的 JSON 403 仍是 key 问题，不该指向 headers fixer：%+v", got)
	}
}

func TestIsPublicHTTPS(t *testing.T) {
	yes := []string{"https://llm.jiangtao.lol/v1", "https://api.openai.com", "HTTPS://Gateway.Example.COM/v1/"}
	no := []string{"http://llm.jiangtao.lol/v1", "https://localhost:7421/v1", "https://127.0.0.1/v1", "https://[::1]/v1",
		"https://10.0.0.5/v1", "https://192.168.1.2:8443", "https://gw.local/v1", "https://gw.internal", "https://nodots", "", "not a url"}
	for _, u := range yes {
		if !harness.IsPublicHTTPS(u) {
			t.Errorf("%q 应算公网 HTTPS", u)
		}
	}
	for _, u := range no {
		if harness.IsPublicHTTPS(u) {
			t.Errorf("%q 不该算公网 HTTPS", u)
		}
	}
}

func TestReportHeadersOnlySpeaksForPublicGateways(t *testing.T) {
	b := harness.NewBuilder("hermes")
	harness.ReportHeaders(b, "http://127.0.0.1:7421/v1", "default_headers", nil, "hermes.default_headers")
	if len(b.Checks()) != 0 {
		t.Fatalf("本地网关不该唠叨自定义头：%+v", b.Checks())
	}
	harness.ReportHeaders(b, "https://llm.example.com/v1", "default_headers", nil, "hermes.default_headers")
	got := find(t, b.Checks(), "hermes.headers")
	if got.Status != report.OK || !strings.Contains(got.Detail, "aivet fix hermes.default_headers") {
		t.Fatalf("没配是中性提示，但要把修复项放在明面上：%+v", got)
	}
	b = harness.NewBuilder("pi")
	harness.ReportHeaders(b, "https://llm.example.com/v1", "headers", map[string]string{"User-Agent": "Mozilla/5.0 x"}, "pi.default_headers")
	if got := find(t, b.Checks(), "pi.headers"); got.Status != report.OK || !strings.Contains(got.Detail, "User-Agent=Mozilla/5.0 x") {
		t.Fatalf("%+v", got)
	}
}
