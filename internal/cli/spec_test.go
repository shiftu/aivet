package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/ui"
)

// mainCommands 是 cmd/aivet 的 switch 里真实分发的命令。
// 说明书和实现对不上是最烦人的一类 bug：帮助里写着的命令跑不了，
// 或者能跑的命令帮助里查不到。这里把两边钉死。
var mainCommands = []string{"check", "fix", "setup", "ask", "skill", "env", "help", "version"}

func TestEveryDispatchedCommandIsDocumented(t *testing.T) {
	cmds := Commands(nil)
	for _, name := range mainCommands {
		if _, ok := Lookup(cmds, name); !ok {
			t.Errorf("%s 能跑但帮助里没有", name)
		}
	}
	if len(cmds) != len(mainCommands) {
		t.Errorf("帮助里有 %d 条命令，实际分发 %d 条——多出来的跑不了", len(cmds), len(mainCommands))
	}
}

func TestAliasesResolve(t *testing.T) {
	cmds := Commands(nil)
	for _, alias := range []string{"doctor", "init"} {
		if _, ok := Lookup(cmds, alias); !ok {
			t.Errorf("别名 %s 查不到", alias)
		}
	}
}

// 每条命令都得有分组、一行说明和用法，否则总览会出现空洞。
func TestCommandsAreComplete(t *testing.T) {
	groups := map[string]bool{}
	for _, g := range Groups {
		groups[g] = true
	}
	for _, c := range Commands(nil) {
		if c.Summary == "" || c.Usage == "" {
			t.Errorf("%s 缺 summary 或 usage", c.Name)
		}
		if !groups[c.Group] {
			t.Errorf("%s 的分组 %q 不在 Groups 里，总览会漏掉它", c.Name, c.Group)
		}
		if !strings.HasPrefix(c.Usage, "aivet") {
			t.Errorf("%s 的 usage 应以 aivet 开头：%q", c.Name, c.Usage)
		}
		for _, f := range c.Flags {
			if f.Desc == "" {
				t.Errorf("%s 的 --%s 没写说明", c.Name, f.Name)
			}
		}
	}
}

// 例子里的命令必须真的存在——照着帮助敲却报「不认识的命令」最伤人。
func TestExamplesReferenceRealCommands(t *testing.T) {
	cmds := Commands(nil)
	for _, c := range cmds {
		for _, e := range c.Examples {
			fields := strings.Fields(e.Cmd)
			if len(fields) == 0 || fields[0] != "aivet" {
				t.Errorf("%s 的例子不是以 aivet 开头：%q", c.Name, e.Cmd)
				continue
			}
			if len(fields) == 1 {
				continue // 光一个 aivet，走默认的 check
			}
			if strings.HasPrefix(fields[1], "-") {
				continue
			}
			if _, ok := Lookup(cmds, fields[1]); !ok {
				t.Errorf("%s 的例子里 %q 不是真命令：%q", c.Name, fields[1], e.Cmd)
			}
			if e.Desc == "" {
				t.Errorf("%s 的例子 %q 没写说明", c.Name, e.Cmd)
			}
		}
	}
}

// fix 那页要列出当前真实可用的修复项，不能写死。
func TestFixPageListsActualFixers(t *testing.T) {
	c, _ := Lookup(Commands([]string{"codex.wire_api", "pi.default_model"}), "fix")
	if !strings.Contains(c.Long, "codex.wire_api") || !strings.Contains(c.Long, "pi.default_model") {
		t.Fatalf("fix 页没列出实际修复项：%s", c.Long)
	}
	c, _ = Lookup(Commands(nil), "fix")
	if !strings.Contains(c.Long, "没有内置的自动修复项") {
		t.Fatalf("一个修复项都没有时要说清楚：%s", c.Long)
	}
}

func TestSuggestHandlesTypos(t *testing.T) {
	cmds := Commands(nil)
	for in, want := range map[string]string{
		"chekc": "check", "seutp": "setup", "chec": "check", "ch": "check", "vers": "version",
	} {
		if got := Suggest(cmds, in); got != want {
			t.Errorf("Suggest(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Suggest(cmds, "zzzzzzzzzz"); got != "" {
		t.Errorf("差太远就别硬猜，得到 %q", got)
	}
}

// help --json 是给 agent 的契约，字段少一个都可能让它不会用。
func TestJSONSpecCarriesWhatAgentsNeed(t *testing.T) {
	var buf bytes.Buffer
	r := Renderer{W: &buf, P: ui.Plain(), Wid: 80, Version: "v9.9.9"}
	if err := r.JSON(Commands([]string{"codex.wire_api"})); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("输出的不是合法 JSON：%v", err)
	}
	for _, k := range []string{"name", "version", "summary", "tools", "exit_codes", "commands", "report_schema"} {
		if _, ok := got[k]; !ok {
			t.Errorf("JSON 缺字段 %q", k)
		}
	}
	schema := got["report_schema"].(map[string]any)
	statuses := schema["check_status_values"].(map[string]any)
	for _, st := range []string{"ok", "warn", "fail", "skip"} {
		if _, ok := statuses[st]; !ok {
			t.Errorf("没说明 status=%s 是什么意思", st)
		}
	}
	if len(schema["suggested_agent_workflow"].([]any)) == 0 {
		t.Error("没给 agent 工作流程")
	}
}

// 渲染不能崩，且窄终端下也要输出内容。
func TestRenderersProduceOutput(t *testing.T) {
	cmds := Commands([]string{"codex.wire_api"})
	for _, w := range []int{40, 80, 110} {
		var buf bytes.Buffer
		r := Renderer{W: &buf, P: ui.Plain(), Wid: w, Version: "v1"}
		r.Overview(cmds)
		if !strings.Contains(buf.String(), "aivet setup") {
			t.Errorf("宽度 %d 的总览没画出来", w)
		}
		for _, c := range cmds {
			var b2 bytes.Buffer
			r2 := Renderer{W: &b2, P: ui.Plain(), Wid: w, Version: "v1"}
			r2.Detail(c)
			if !strings.Contains(b2.String(), c.Summary) {
				t.Errorf("宽度 %d 下 %s 的详情页没画出 summary", w, c.Name)
			}
		}
	}
}
