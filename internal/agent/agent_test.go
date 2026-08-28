package agent

import (
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/report"
)

func TestPromptListsOnlyProblemsAndNeverKeys(t *testing.T) {
	r := report.Report{Tools: []report.Tool{{ID: "codex", Installed: true, Checks: []report.Check{
		{Tool: "codex", Title: "key", Status: report.OK, Detail: "sk-abc****xyz"},
		{Tool: "codex", Title: "wire_api", Status: report.Fail, Detail: "chat", Hint: "改 responses"},
	}}}}
	p := Prompt(r, "/tmp/r.json")
	if !strings.Contains(p, "wire_api") || strings.Contains(p, "sk-abc") {
		t.Fatalf("prompt = %s", p)
	}
	if !strings.Contains(p, "/tmp/r.json") {
		t.Fatal("应包含报告路径")
	}
}
