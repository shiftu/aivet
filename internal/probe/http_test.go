package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
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
