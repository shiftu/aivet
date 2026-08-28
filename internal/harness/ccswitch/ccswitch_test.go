package ccswitch

import "testing"

func TestParseCurrentRow(t *testing.T) {
	table := "│   ┆ default        ┆ default                ┆ https://a    │\n│ ✓ ┆ openai-gpt55   ┆ OpenAI gpt-5.6-sol     ┆ https://proxy.iher.ai    │\n"
	cp, ok := parseCurrentRow(table)
	if !ok || cp.ID != "openai-gpt55" || cp.BaseURL != "https://proxy.iher.ai" || cp.Name != "OpenAI gpt-5.6-sol" {
		t.Fatalf("parsed %+v ok=%v", cp, ok)
	}
	if _, ok := parseCurrentRow("nothing"); ok {
		t.Fatal("no row should be false")
	}
}

func TestBaseFromSettings(t *testing.T) {
	if got := baseFromSettings("codex", `{"config":"model = \"x\"\n[model_providers.a]\nbase_url = \"http://g/v1\"\n"}`); got != "http://g/v1" {
		t.Fatalf("codex base = %q", got)
	}
	if got := baseFromSettings("claude", `{"env":{"ANTHROPIC_BASE_URL":"http://c"}}`); got != "http://c" {
		t.Fatalf("claude base = %q", got)
	}
	if got := baseFromSettings("hermes", `{"base_url":"http://h/v1"}`); got != "http://h/v1" {
		t.Fatalf("hermes base = %q", got)
	}
}
