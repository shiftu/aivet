package cli

import (
	"strings"
	"testing"
)

// names 把候选摊平成一个字符串，方便在表里写期望。
func names(vs []Value) string {
	var out []string
	for _, v := range vs {
		out = append(out, v.Name)
	}
	return strings.Join(out, " ")
}

// 补全错了不会报错，只会安静地补出错的东西 —— 所以每一种形状都得钉一遍。
func TestComplete(t *testing.T) {
	cmds := Commands([]string{"codex.wire_api", "pi.default_model"})
	tests := []struct {
		name  string
		words []string
		want  string
	}{
		{"什么都没敲，给全部命令", []string{""}, "check fix setup ask skill env knowledge help completion version"},
		{"命令名前缀", []string{"c"}, "check completion"},
		{"顶层的横杠", []string{"-"}, "--help --version"},
		{"check 的位置参数是工具", []string{"check", ""}, "claude codex hermes pi dsh ccswitch"},
		{"敲过的工具不再提示", []string{"check", "codex", ""}, "claude hermes pi dsh ccswitch"},
		{"别名也能查到命令", []string{"doctor", "cl"}, "claude"},
		{"选项名", []string{"check", "--l"}, "--live"},
		{"选项名带上 --help", []string{"env", "-"}, "--help"},
		{"fix 的位置参数是活的修复项", []string{"fix", ""}, "codex.wire_api pi.default_model"},
		{"skill 的子命令", []string{"skill", ""}, "install show"},
		{"skill 只收一个子命令", []string{"skill", "install", ""}, ""},
		{"取值型选项：ask --with", []string{"ask", "--with", ""}, "claude codex hermes pi dsh"},
		{"取值型选项带前缀", []string{"ask", "--with", "c"}, "claude codex"},
		{"逗号分隔：补最后一段，带上前面的", []string{"setup", "--tools", "claude,c"}, "claude,codex"},
		{"逗号分隔：不重复提示已选的", []string{"skill", "--for", "claude,codex,"}, "claude,codex,hermes claude,codex,pi"},
		{"等号连写", []string{"setup", "--tools=he"}, "--tools=hermes"},
		{"bash 把等号拆成三个词", []string{"setup", "--tools", "=", "he"}, "--tools=hermes"},
		{"自由取值的选项没有候选", []string{"setup", "--gateway", ""}, ""},
		{"help 能补出命令名", []string{"help", "kn"}, "knowledge"},
		{"不认识的命令不瞎猜", []string{"nonsense", ""}, ""},
		{"没有位置参数的命令", []string{"env", ""}, ""},
		{"完全没给词也当成在敲第一个", nil, "check fix setup ask skill env knowledge help completion version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := names(Complete(cmds, tt.words)); got != tt.want {
				t.Errorf("Complete(%q)\n 得到 %q\n 期望 %q", tt.words, got, tt.want)
			}
		})
	}
}

// 说明会被 zsh 显示在候选旁边，空着就白瞎了这块地方。
func TestCompleteCarriesDescriptions(t *testing.T) {
	for _, c := range Complete(Commands(nil), []string{""}) {
		if c.Desc == "" {
			t.Errorf("命令 %s 补出来没有说明", c.Name)
		}
	}
	for _, c := range Complete(Commands(nil), []string{"check", "--"}) {
		if c.Desc == "" {
			t.Errorf("选项 %s 补出来没有说明", c.Name)
		}
	}
}

// 补全的取值必须是真的能填进去的东西 —— 补出一个命令不认的值比不补更糟。
func TestFlagValuesAreRealTools(t *testing.T) {
	known := map[string]bool{}
	for _, id := range Tools {
		known[id] = true
	}
	for _, c := range Commands(nil) {
		for _, f := range c.Flags {
			if f.Arg == "" && len(f.Values) > 0 {
				t.Errorf("aivet %s --%s 不取值，却给了候选", c.Name, f.Name)
			}
			for _, v := range f.Values {
				if strings.HasSuffix(f.Name, "tools") || f.Name == "with" || f.Name == "for" {
					if !known[v.Name] {
						t.Errorf("aivet %s --%s 会补出 %q，但它不是已知工具", c.Name, f.Name, v.Name)
					}
				}
			}
		}
	}
}

// TakesValue 是参数解析和补全共用的判断，两边都指着它，错了会同时坏。
func TestTakesValue(t *testing.T) {
	cmds := Commands(nil)
	for _, name := range []string{"gateway", "key", "model", "tools", "with", "for"} {
		if !TakesValue(cmds, "--"+name) {
			t.Errorf("--%s 是要取值的，TakesValue 说不是", name)
		}
	}
	for _, name := range []string{"json", "live", "offline", "yes", "dry-run", "force", "init"} {
		if TakesValue(cmds, "--"+name) {
			t.Errorf("--%s 是开关，TakesValue 却说它要取值——解析会吃掉后面那个词", name)
		}
	}
}

// 每家 shell 都得有脚本和安装说明，少一样这个 shell 的用户就卡在半路。
func TestEveryShellHasScriptAndHint(t *testing.T) {
	for _, sh := range Shells {
		script, ok := Script(sh)
		if !ok || script == "" {
			t.Fatalf("%s 没有补全脚本", sh)
		}
		if !strings.Contains(script, "__complete") {
			t.Errorf("%s 的脚本没回头调 aivet __complete，那它就是写死的", sh)
		}
		if !strings.Contains(script, "--cur=") {
			t.Errorf("%s 的脚本没用 --cur= 递当前词，空词会被吞掉", sh)
		}
		if len(InstallHint(sh)) == 0 {
			t.Errorf("%s 没有安装说明", sh)
		}
	}
	if _, ok := Script("tcsh"); ok {
		t.Error("不该认识 tcsh")
	}
}
