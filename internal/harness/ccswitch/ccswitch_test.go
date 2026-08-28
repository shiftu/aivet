package ccswitch

import "testing"

func TestParseRows(t *testing.T) {
	table := "│   ┆ ID             ┆ Name                   ┆ API URL │\n" +
		"│   ┆ default        ┆ default                ┆ https://a    │\n" +
		"│ ✓ ┆ openai-gpt55   ┆ OpenAI gpt-5.6-sol     ┆ https://proxy.iher.ai    │\n" +
		"│   ┆ claude-official┆ Claude Official        ┆ N/A     │\n"
	rows := parseRows(table)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header must not parse): %+v", len(rows), rows)
	}
	if rows[0].Current || rows[0].BaseURL != "https://a" {
		t.Fatalf("row0 %+v", rows[0])
	}
	if !rows[1].Current || rows[1].ID != "openai-gpt55" || rows[1].BaseURL != "https://proxy.iher.ai" || rows[1].Name != "OpenAI gpt-5.6-sol" {
		t.Fatalf("row1 %+v", rows[1])
	}
	if rows[2].BaseURL != "" {
		t.Fatalf("N/A must become empty: %+v", rows[2])
	}
	if got := parseRows("nothing"); len(got) != 0 {
		t.Fatal("no row should be empty")
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
