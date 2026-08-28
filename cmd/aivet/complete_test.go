package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/cli"
	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/skill"
)

// 补全会告诉用户「这里能填这几个」。这几份名单要是和真实情况对不上，
// 结果不是报错，是补出一个命令根本不收的值 —— 用户按了 Tab、拿到一个词、
// 然后被告知不认识。所以在这里拿真实的注册表钉死，别处再抄一份都不行。

func idsWhere(keep func(harness.Harness) bool) []string {
	var out []string
	for _, h := range registry() {
		if keep(h) {
			out = append(out, h.ID())
		}
	}
	return out
}

func same(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s：说明书写的是 %v，实际是 %v", what, got, want)
	}
}

func TestToolNamesMatchRegistry(t *testing.T) {
	same(t, "cli.Tools（aivet check 能填的）", cli.Tools,
		idsWhere(func(harness.Harness) bool { return true }))

	same(t, "cli.Agents（aivet ask --with 能填的）", cli.Agents,
		idsWhere(func(h harness.Harness) bool { _, ok := h.(harness.Launcher); return ok }))

	same(t, "cli.Configurable（aivet setup --tools 能填的）", cli.Configurable,
		idsWhere(func(h harness.Harness) bool { _, ok := h.(harness.Configurer); return ok }))

	want := make([]string, 0, len(skill.Targets))
	for id := range skill.Targets {
		want = append(want, id)
	}
	got := append([]string(nil), cli.SkillTools...)
	sort.Strings(want)
	sort.Strings(got)
	same(t, "cli.SkillTools（aivet skill --for 能填的）", got, want)
}

// 补出来的修复项得真能修 —— fix 那一页和补全用的是同一份现拿的 id。
func TestFixIDsCompleteToRealFixers(t *testing.T) {
	all := registry()
	got := cli.Complete(commands(), []string{"fix", ""})
	if len(got) == 0 {
		t.Skip("这个版本没有内置修复项")
	}
	for _, v := range got {
		if _, _, ok := harness.FindFixer(all, v.Name); !ok {
			t.Errorf("补全会给出修复项 %q，但 FindFixer 找不到它", v.Name)
		}
	}
}

// 每条能分发的命令都得能被补出来，否则用户按 Tab 会以为它不存在。
func TestEveryCommandIsCompletable(t *testing.T) {
	got := map[string]bool{}
	for _, v := range cli.Complete(commands(), []string{""}) {
		got[v.Name] = true
	}
	for _, name := range []string{"check", "fix", "setup", "ask", "skill", "env", "knowledge", "help", "completion", "version"} {
		if !got[name] {
			t.Errorf("%s 能跑，但补不出来", name)
		}
	}
}

// detectShell 只认它真的能给出脚本的那几个，认错了会让用户装上一个跑不了的文件。
func TestDetectShellOnlyClaimsSupportedOnes(t *testing.T) {
	for _, tc := range []struct{ shell, want string }{
		{"/bin/zsh", "zsh"},
		{"/bin/bash", "bash"},
		{"/usr/local/bin/fish", "fish"},
		{"/opt/homebrew/bin/pwsh", "powershell"},
		{"/bin/tcsh", ""},
		{"", ""},
	} {
		t.Setenv("SHELL", tc.shell)
		if got := detectShell(); got != tc.want {
			t.Errorf("SHELL=%q 认成 %q，期望 %q", tc.shell, got, tc.want)
		}
		if tc.want != "" {
			if _, ok := cli.Script(tc.want); !ok {
				t.Errorf("认出了 %q 却没有对应脚本", tc.want)
			}
		}
	}
}
