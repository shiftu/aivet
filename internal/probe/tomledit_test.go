package probe

import (
	"strings"
	"testing"
)

const sampleTOML = `# 注释要留着
model = "old"
model_provider = "x"

[model_providers.x]
name = "X"
wire_api = "chat"

[projects."/tmp"]
trust_level = "trusted"
`

func TestTOMLSetTopKeyReplacesInPlace(t *testing.T) {
	out := TOMLSetTopKey(sampleTOML, "model", `"new"`)
	if !strings.Contains(out, `model = "new"`) || strings.Contains(out, `"old"`) {
		t.Fatalf("replace failed:\n%s", out)
	}
	if !strings.HasPrefix(out, "# 注释要留着") {
		t.Fatalf("注释丢了")
	}
	out = TOMLSetTopKey(sampleTOML, "approval_policy", `"never"`)
	idx := strings.Index(out, "approval_policy")
	if idx < 0 || idx > strings.Index(out, "[model_providers.x]") {
		t.Fatalf("新 key 应插在第一个表之前:\n%s", out)
	}
}

func TestTOMLSetTableKey(t *testing.T) {
	out := TOMLSetTableKey(sampleTOML, "model_providers.x", "wire_api", `"responses"`)
	if !strings.Contains(out, `wire_api = "responses"`) || strings.Contains(out, `"chat"`) {
		t.Fatalf("table key replace failed:\n%s", out)
	}
	if TOMLGetTableKey(out, "model_providers.x", "wire_api") != "responses" {
		t.Fatalf("get after set failed")
	}
	out = TOMLSetTableKey(sampleTOML, "model_providers.x", "base_url", `"http://g/v1"`)
	xStart := strings.Index(out, "[model_providers.x]")
	pStart := strings.Index(out, `[projects."/tmp"]`)
	bIdx := strings.Index(out, "base_url")
	if bIdx < xStart || bIdx > pStart {
		t.Fatalf("新 key 应落在 x 表内:\n%s", out)
	}
	out = TOMLSetTableKey(sampleTOML, "model_providers.new", "name", `"N"`)
	if !strings.Contains(out, "[model_providers.new]\nname = \"N\"") {
		t.Fatalf("missing table should be appended:\n%s", out)
	}
}

func TestTOMLReplaceTable(t *testing.T) {
	out := TOMLReplaceTable(sampleTOML, "model_providers.x", "[model_providers.x]\nname = \"Y\"\n")
	if strings.Contains(out, `wire_api`) || !strings.Contains(out, `name = "Y"`) || !strings.Contains(out, "trust_level") {
		t.Fatalf("replace table failed:\n%s", out)
	}
	if TOMLReplaceTable("", "a", "[a]\nk = 1\n") != "[a]\nk = 1\n" {
		t.Fatal("empty file append")
	}
}

func TestTOMLQuotedTableName(t *testing.T) {
	c := "[model_providers.\"my-gw\"]\nbase_url = \"http://a\"\n"
	if TOMLGetTableKey(c, "model_providers.my-gw", "base_url") != "http://a" {
		t.Fatal("quoted table lookup failed")
	}
}
