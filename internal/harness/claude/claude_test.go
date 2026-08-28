package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// gw 是一个只有 charaboard/* 模型的网关——刻意不含 "sonnet"。
func gw(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"charaboard/claude-sonnet-5"},{"id":"charaboard/sonnet-4.6"}]}`))
		case "/v1/messages":
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
}

// writeSettings 造一份 ~/.claude/settings.json。
func writeSettings(t *testing.T, home string, env map[string]any, model string) {
	t.Helper()
	s := map[string]any{"env": env}
	if model != "" {
		s["model"] = model
	}
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	if err := probe.WriteJSON(filepath.Join(home, ".claude", "settings.json"), s); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, home string) []report.Check {
	t.Helper()
	c := &harness.Context{Ctx: context.Background(), Home: home, Env: func(string) string { return "" }, Gateways: probe.NewGatewayCache()}
	return H{}.Check(c, harness.Detection{Installed: true, Path: "/nonexistent/claude"})
}

func find(checks []report.Check, id string) (report.Check, bool) {
	for _, c := range checks {
		if c.ID == id {
			return c, true
		}
	}
	return report.Check{}, false
}

// 别名 sonnet 不在网关清单里，但那不代表挂了——不能报 fail。
func TestUnmappedAliasIsWarnNotFail(t *testing.T) {
	srv := gw(t)
	defer srv.Close()
	home := t.TempDir()
	writeSettings(t, home, map[string]any{"ANTHROPIC_BASE_URL": srv.URL, "ANTHROPIC_AUTH_TOKEN": "k"}, "sonnet")
	checks := run(t, home)
	if _, ok := find(checks, "claude.gateway.model"); ok {
		t.Fatal("别名不该拿去和清单比对——那比的是错的东西")
	}
	c, ok := find(checks, "claude.model.alias")
	if !ok || c.Status != report.Warn {
		t.Fatalf("应报 warn，实际 %+v", c)
	}
	if c.Hint == "" || !contains(c.Hint, "charaboard/claude-sonnet-5") || !contains(c.Hint, "--live") {
		t.Fatalf("hint 要给出相近模型和 --live 出路：%q", c.Hint)
	}
	for _, ch := range checks {
		if ch.Status == report.Fail {
			t.Fatalf("不该有 fail：%+v", ch)
		}
	}
}

// 映射到网关上真实存在的模型：正常走三连，且显示 别名 → 真名。
func TestMappedAliasProbesTheRealModel(t *testing.T) {
	srv := gw(t)
	defer srv.Close()
	home := t.TempDir()
	writeSettings(t, home, map[string]any{
		"ANTHROPIC_BASE_URL":             srv.URL,
		"ANTHROPIC_AUTH_TOKEN":           "k",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "charaboard/claude-sonnet-5",
	}, "sonnet")
	checks := run(t, home)
	m, _ := find(checks, "claude.model")
	if m.Status != report.OK || !contains(m.Detail, "sonnet → charaboard/claude-sonnet-5") {
		t.Fatalf("模型行应显示映射关系：%+v", m)
	}
	if c, ok := find(checks, "claude.gateway.model"); !ok || c.Status != report.OK {
		t.Fatalf("映射后应核对真名：%+v", c)
	}
}

// 映射到了一个网关没有的名字——这才是真故障。
func TestMappedAliasToMissingModelFails(t *testing.T) {
	srv := gw(t)
	defer srv.Close()
	home := t.TempDir()
	writeSettings(t, home, map[string]any{
		"ANTHROPIC_BASE_URL":             srv.URL,
		"ANTHROPIC_AUTH_TOKEN":           "k",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "no-such-model",
	}, "sonnet")
	c, ok := find(run(t, home), "claude.gateway.model")
	if !ok || c.Status != report.Fail {
		t.Fatalf("映射到不存在的模型应报 fail：%+v", c)
	}
}

// 官方登录时别名完全正常，一句都不该唠叨。
func TestAliasIsFineWithoutGateway(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, map[string]any{}, "sonnet")
	os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte("{}"), 0o600)
	for _, c := range run(t, home) {
		if c.ID == "claude.model.alias" {
			t.Fatalf("没走网关时不该提别名：%+v", c)
		}
	}
}

// claude-fable-5[1m] 这类后缀不属于模型名，比对前要剥掉。
func TestQualifierStripped(t *testing.T) {
	if stripQualifier("claude-fable-5[1m]") != "claude-fable-5" || stripQualifier("plain") != "plain" {
		t.Fatal(stripQualifier("claude-fable-5[1m]"))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
