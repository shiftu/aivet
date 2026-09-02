package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelsURLAndAPIURL(t *testing.T) {
	if ModelsURL("http://x/v1/") != "http://x/v1/models" {
		t.Fatal(ModelsURL("http://x/v1/"))
	}
	if ModelsURL("http://x") != "http://x/v1/models" {
		t.Fatal(ModelsURL("http://x"))
	}
	if apiURL("https://api.anthropic.com", "messages") != "https://api.anthropic.com/v1/messages" {
		t.Fatal(apiURL("https://api.anthropic.com", "messages"))
	}
}

func TestListModelsAndPingAgainstFakeGateway(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
		case "/v1/chat/completions", "/v1/responses":
			w.Write([]byte(`{"ok":true}`))
		case "/v1/messages":
			if r.Header.Get("x-api-key") != "k" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	infos, pr := ListModels(ctx, Endpoint{BaseURL: srv.URL + "/v1", Key: "k"})
	ids := ModelIDs(infos)
	if !pr.OK || len(ids) != 2 || !HasModel(ids, "m2") {
		t.Fatalf("models: %v %+v", ids, pr)
	}
	_, pr = ListModels(ctx, Endpoint{BaseURL: srv.URL, Key: "bad"})
	if pr.OK || pr.Status != 401 {
		t.Fatalf("bad key should 401: %+v", pr)
	}
	for _, proto := range []Protocol{ChatCompletions, Responses, Anthropic} {
		if pr := Ping(ctx, Endpoint{BaseURL: srv.URL, Key: "k", Protocol: proto}, "m1"); !pr.OK {
			t.Fatalf("%s ping failed: %+v", proto, pr)
		}
	}
	g := NewGatewayCache()
	g.Models(ctx, Endpoint{BaseURL: srv.URL, Key: "k"})
	srv.Close() // 缓存命中不应再打网络
	if ids, pr := g.Models(ctx, Endpoint{BaseURL: srv.URL + "/", Key: "k"}); !pr.OK || len(ids) != 2 {
		t.Fatalf("cache miss: %+v", pr)
	}
}

func TestCloudflareBlockTellsEdgeBlockFromGatewayDenial(t *testing.T) {
	h := func(kv ...string) http.Header {
		out := http.Header{}
		for i := 0; i+1 < len(kv); i += 2 {
			out.Set(kv[i], kv[i+1])
		}
		return out
	}
	cases := []struct {
		name    string
		status  int
		hdr     http.Header
		body    string
		blocked bool
	}{
		{"挑战页", 403, h("Server", "cloudflare", "cf-ray", "abc"), `<html><title>Just a moment...</title>`, true},
		{"cf-mitigated 头", 403, h("Server", "cloudflare", "cf-mitigated", "challenge"), `{"whatever":1}`, true},
		{"WAF 纯文本", 403, h("Server", "cloudflare"), `error code: 1020`, true},
		{"503 挑战", 503, h("Server", "cloudflare"), `<html>Checking your browser before accessing`, true},
		{"网关自己的 403（JSON，但挂在 CF 后面）", 403, h("Server", "cloudflare", "cf-ray", "abc"), `{"error":{"message":"model not allowed"}}`, false},
		{"普通 403", 403, h("Server", "nginx"), `forbidden`, false},
		{"401 永远不算", 401, h("Server", "cloudflare"), `<html>Just a moment</html>`, false},
		{"200 永远不算", 200, h("Server", "cloudflare"), ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CloudflareBlock(c.status, c.hdr, []byte(c.body))
			if (got != "") != c.blocked {
				t.Fatalf("blocked=%v，证据 %q", c.blocked, got)
			}
		})
	}
}

// cloudflareServer 模拟「网关前面挂着 Cloudflare」：UA 不像浏览器就拦，像了就放行到网关。
func cloudflareServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("cf-ray", "8a1b2c3d4e5f-HKG")
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Mozilla/") {
			w.Header().Set("cf-mitigated", "challenge")
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(403)
			w.Write([]byte(`<!DOCTYPE html><html><head><title>Just a moment...</title></head><body><div id="challenge-platform"></div></body></html>`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"bad key"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"m1"}]}`))
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestBotBlockedAndCustomHeadersGetThrough(t *testing.T) {
	base := cloudflareServer(t) + "/v1"
	ctx := context.Background()
	plain := Endpoint{BaseURL: base, Key: "k", Protocol: ChatCompletions}
	_, pr := ListModels(ctx, plain)
	if pr.OK || pr.Status != 403 || !pr.BotBlocked {
		t.Fatalf("不带浏览器头应被判成 bot 拦截：%+v", pr)
	}
	if !strings.Contains(pr.Detail, "Cloudflare") || strings.Contains(pr.Detail, "key 没有这个权限") {
		t.Fatalf("文案要说清是 Cloudflare 拦截而不是 key 权限：%q", pr.Detail)
	}
	if pr := Ping(ctx, plain, "m1"); !pr.BotBlocked {
		t.Fatalf("ping 也该识别出拦截：%+v", pr)
	}
	browser := plain.WithHeaders(map[string]string{"User-Agent": "Mozilla/5.0 test"})
	if len(plain.Headers) != 0 {
		t.Fatal("WithHeaders 不该改原来的 Endpoint")
	}
	if ids, pr := ListModels(ctx, browser); !pr.OK || len(ids) != 1 {
		t.Fatalf("带浏览器头应通：%+v", pr)
	}
	if pr := Ping(ctx, browser, "m1"); !pr.OK || pr.BotBlocked {
		t.Fatalf("带浏览器头 ping 应通：%+v", pr)
	}
	// 缓存 key 必须把头折进去：不带头那次的 403 不能被带头的请求命中。
	g := NewGatewayCache()
	if _, pr := g.Models(ctx, plain); !pr.BotBlocked {
		t.Fatalf("%+v", pr)
	}
	if _, pr := g.Models(ctx, browser); !pr.OK {
		t.Fatalf("带头应绕开不带头的缓存：%+v", pr)
	}
	if pr := g.Ping(ctx, plain, "m1"); !pr.BotBlocked {
		t.Fatalf("%+v", pr)
	}
	if pr := g.Ping(ctx, browser, "m1"); !pr.OK {
		t.Fatalf("带头 ping 应绕开不带头的缓存：%+v", pr)
	}
}
