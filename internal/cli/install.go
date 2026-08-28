package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// 把补全「装好」而不是「打印出来让你自己装」。
//
// 各家 shell 的规矩不一样，但形状是同一个：补全脚本落一个文件；除了 fish
// 会自动加载，其余还得往 rc 文件里塞一段加载它的话。那一段用首尾标记框起来，
// 是为了能重装 —— 升级时整段换掉，而不是一次装一次多一份。
const (
	rcBegin = "# >>> aivet completion >>>"
	rcEnd   = "# <<< aivet completion <<<"
)

// InstallPlan 是「装到哪」的完整答案。RCPath 为空表示这家 shell 会自己发现脚本，不用动 rc。
type InstallPlan struct {
	Shell      string
	ScriptPath string
	RCPath     string
	RCLines    []string
}

// InstallResult 记录实际动了什么。重装时该说「已经是最新的」，不该谎称刚写入。
type InstallResult struct {
	InstallPlan
	ScriptWritten bool
	RCWritten     bool
	Notes         []string
}

// Plan 给出某家 shell 的安装位置。home 是家目录；psProfile 只有 powershell 用得上
// （PowerShell 的 $PROFILE 会被 OneDrive 之类的重定向，只能问它本人要，见 PowerShellProfile）。
func Plan(shell, home, psProfile string) (InstallPlan, bool) {
	switch strings.ToLower(shell) {
	case "bash":
		script := filepath.Join(home, ".local", "share", "bash-completion", "completions", "aivet")
		return InstallPlan{
			Shell:      "bash",
			ScriptPath: script,
			RCPath:     filepath.Join(home, ".bashrc"),
			// 装了 bash-completion 的机器会自己加载上面那个目录，但不能指望人人都装了，
			// 所以 rc 里再显式 source 一次。重复加载没有副作用：complete 是覆盖登记。
			RCLines: []string{
				`[ -f "$HOME/.local/share/bash-completion/completions/aivet" ] && . "$HOME/.local/share/bash-completion/completions/aivet"`,
			},
		}, true
	case "zsh":
		script := filepath.Join(home, ".zsh", "completions", "_aivet")
		return InstallPlan{
			Shell:      "zsh",
			ScriptPath: script,
			RCPath:     filepath.Join(home, ".zshrc"),
			// 这段是加在 .zshrc 末尾的，也就是 oh-my-zsh 那类框架跑完 compinit 之后。
			// compdef 已经有了就直接 source（脚本自带的双模守卫会去登记）；
			// 没有说明这个 .zshrc 根本没开补全，那就自己开一次。
			RCLines: []string{
				`fpath=("$HOME/.zsh/completions" $fpath)`,
				`if (( ! $+functions[compdef] )); then autoload -Uz compinit; compinit; fi`,
				`source "$HOME/.zsh/completions/_aivet"`,
			},
		}, true
	case "fish":
		// fish 认 completions 目录，放进去就完事，不用碰配置。
		return InstallPlan{
			Shell:      "fish",
			ScriptPath: filepath.Join(home, ".config", "fish", "completions", "aivet.fish"),
		}, true
	case "powershell", "pwsh":
		if psProfile == "" {
			return InstallPlan{}, false
		}
		script := filepath.Join(filepath.Dir(psProfile), "aivet.completion.ps1")
		return InstallPlan{
			Shell:      "powershell",
			ScriptPath: script,
			RCPath:     psProfile,
			RCLines:    []string{`. "` + script + `"`},
		}, true
	}
	return InstallPlan{}, false
}

// Install 按 Plan 把脚本写到位，需要的话再往 rc 里补上加载它的那几行。
// 重复跑是安全的：内容没变就不动文件，rc 里的标记块是整段替换而不是追加。
func Install(shell, home, psProfile string) (InstallResult, error) {
	plan, ok := Plan(shell, home, psProfile)
	if !ok {
		return InstallResult{}, fmt.Errorf("不认识的 shell %q；可选：%s", shell, strings.Join(Shells, " "))
	}
	script, ok := Script(plan.Shell)
	if !ok {
		return InstallResult{}, fmt.Errorf("%s 没有补全脚本", plan.Shell)
	}
	res := InstallResult{InstallPlan: plan}

	wrote, err := writeIfChanged(plan.ScriptPath, script)
	if err != nil {
		return res, err
	}
	res.ScriptWritten = wrote

	if plan.RCPath != "" {
		wrote, err = upsertBlock(plan.RCPath, plan.RCLines)
		if err != nil {
			return res, err
		}
		res.RCWritten = wrote
	}
	res.Notes = notes(plan, home)
	return res, nil
}

// notes 是「装完了但可能还是不生效」的那几种情况 —— 与其让用户自己撞上，不如当场说。
func notes(plan InstallPlan, home string) []string {
	var out []string
	if plan.Shell == "bash" && runtime.GOOS == "darwin" {
		// macOS 的终端默认开登录 shell，登录 shell 只读 .bash_profile，不读 .bashrc。
		// 这种情况改的是对的文件，但不会被念到，只能提醒 —— 替用户改 .bash_profile 太越界了。
		bp := filepath.Join(home, ".bash_profile")
		if b, err := os.ReadFile(bp); err == nil && !strings.Contains(string(b), ".bashrc") {
			out = append(out, "macOS 的登录 shell 只读 ~/.bash_profile。里面加一行 [ -f ~/.bashrc ] && . ~/.bashrc，补全才会被念到。")
		}
	}
	return out
}

// PowerShellProfile 问 PowerShell 本人要 $PROFILE 的真实路径。
// 不自己拼 Documents\PowerShell\... 是因为 OneDrive 会把「文档」整个重定向走，拼出来的往往不是真的那个。
func PowerShellProfile() string {
	for _, exe := range []string{"pwsh", "powershell"} {
		bin, err := exec.LookPath(exe)
		if err != nil {
			continue
		}
		out, err := exec.Command(bin, "-NoProfile", "-Command", "$PROFILE").Output()
		if p := strings.TrimSpace(string(out)); err == nil && p != "" {
			return p
		}
	}
	return ""
}

// writeIfChanged 内容一样就不动文件（省得每次安装都刷新 mtime，也好看出到底有没有变）。
func writeIfChanged(path, content string) (bool, error) {
	if b, err := os.ReadFile(path); err == nil && string(b) == content {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("建目录 %s：%w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("写 %s：%w", path, err)
	}
	return true, nil
}

// upsertBlock 把带标记的那一段写进 rc 文件：已经有了就整段替换，没有就追加。
// 用户自己写在标记外面的东西一律不动。
func upsertBlock(path string, lines []string) (bool, error) {
	block := rcBegin + "\n" + strings.Join(lines, "\n") + "\n" + rcEnd
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("读 %s：%w", path, err)
	}
	text := string(old)

	if b := strings.Index(text, rcBegin); b >= 0 {
		if e := strings.Index(text[b:], rcEnd); e >= 0 {
			e += b + len(rcEnd)
			if text[b:e] == block {
				return false, nil
			}
			text = text[:b] + block + text[e:]
			return true, writeFile(path, text)
		}
		// 只有开头标记没有结尾 —— 用户手改坏了。不猜从哪结束，当成没有，追加一份新的。
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += block + "\n"
	return true, writeFile(path, text)
}

func writeFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("建目录 %s：%w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("写 %s：%w", path, err)
	}
	return nil
}
