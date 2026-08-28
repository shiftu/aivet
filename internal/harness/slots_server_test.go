package harness_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// modelsServer 是一个只会答模型清单的假网关。
func modelsServer(t *testing.T, ids ...string) string {
	t.Helper()
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"context_length":1000,"max_completion_tokens":100}`, id))
	}
	body := `{"data":[` + strings.Join(parts, ",") + `]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// noCatalogServer 模拟「网关活着，但没有 OpenAI 风格的清单接口」。
func noCatalogServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
