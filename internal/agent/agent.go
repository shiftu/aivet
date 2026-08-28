// Package agent 把体检报告交给一个「还能用的」agent 去修剩下的问题。
//
// 这是 aivet 挂进 agent 工作流的入口：aivet 负责确定性的诊断和安全的自动修复，
// 诊断得出但修不了的（装软件、改 shell 配置、看日志），交给会动手的 agent。
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/report"
)

// Prompt 生成交给 agent 的提示词（中文，带 JSON 报告路径）。
func Prompt(r report.Report, reportPath string) string {
	var sb strings.Builder
	sb.WriteString("我用 aivet 给本机的 AI coding 工具做了体检，下面是没通过的项。请逐条帮我修好：\n")
	sb.WriteString("先读报告，解释每条的根因，再给出最小改动并执行；改配置文件前先备份；改完运行 `aivet check --json` 复验，直到没有 fail。\n")
	sb.WriteString("不要打印或粘贴任何 API key。装软件前先告诉我要装什么。\n\n")
	sb.WriteString("完整报告（JSON）：" + reportPath + "\n\n未通过的项：\n")
	for _, c := range r.Problems() {
		sb.WriteString(fmt.Sprintf("- [%s] %s / %s：%s", strings.ToUpper(string(c.Status)), c.Tool, c.Title, c.Detail))
		if c.Hint != "" {
			sb.WriteString("（提示：" + c.Hint + "）")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// SaveReport 把报告写到 ~/.aivet/last-report.json，返回路径。
func SaveReport(home string, r report.Report) (string, error) {
	dir := filepath.Join(home, ".aivet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "last-report.json")
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return p, os.WriteFile(p, b, 0o600)
}

// Pick 选一个能接手的 agent：优先用户指定；否则挑体检里没有 fail 的第一个。
func Pick(all []harness.Harness, r report.Report, prefer string) (harness.Harness, error) {
	byID := map[string]harness.Harness{}
	for _, h := range all {
		byID[h.ID()] = h
	}
	if prefer != "" {
		h, ok := byID[prefer]
		if !ok {
			return nil, fmt.Errorf("不认识 %q；可选：claude / codex / hermes / pi / dsh", prefer)
		}
		if _, ok := h.(harness.Launcher); !ok {
			return nil, fmt.Errorf("%s 不能作为 agent 接手", prefer)
		}
		return h, nil
	}
	for _, t := range r.Tools {
		if !t.Installed || t.Worst() == report.Fail {
			continue
		}
		if h, ok := byID[t.ID]; ok {
			if _, can := h.(harness.Launcher); can {
				return h, nil
			}
		}
	}
	return nil, fmt.Errorf("没有一件 agent 是健康的，没人能接手。先手动把一件修到能用（aivet setup 是最快的路）")
}

// Launch 前台拉起 agent，stdin/stdout 直通，用户接管对话。
func Launch(h harness.Harness, prompt string) error {
	args := h.(harness.Launcher).LaunchArgs(prompt)
	bin, err := exec.LookPath(args[0])
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
