package probe

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Which 在 PATH 上找可执行文件；给多个候选名时按序返回第一个命中。
// Windows 上 exec.LookPath 会自动尝试 PATHEXT（.exe/.cmd/.bat）。
func Which(names ...string) (string, bool) {
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, true
		}
	}
	return "", false
}

// Version 跑 `<bin> args...` 取第一行非空输出；跑不起来返回空串。
func Version(bin string, args ...string) string {
	v, _ := VersionOr(bin, args...)
	return v
}

// VersionOr 同 Version，但把「文件在 PATH 上却跑不起来」单独报出来。
//
// 为什么要分开：npm 装的工具（codex / pi / dsh）经常出现 shim 在、平台包没装全的情况，
// `codex --version` 会打出 `Error: spawn …/codex ENOENT` 并非零退出。
// 老实现把这行错误当成版本号显示，于是标题上写着一串 ENOENT，
// 底下的配置项还一路绿灯 —— 根因被埋在最不显眼的地方。
func VersionOr(bin string, args ...string) (version, broken string) {
	out, err := Run(context.Background(), 20*time.Second, "", bin, args...)
	first := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(strings.ToLower(line), "warning") {
			continue
		}
		first = line
		break
	}
	if err != nil {
		reason := Tail(out, 3, 200)
		if reason == "" {
			reason = err.Error()
		}
		return "", reason
	}
	return first, ""
}

// Run 带超时执行命令，返回合并后的 stdout+stderr。
// dir 为空则继承当前目录。
func Run(ctx context.Context, timeout time.Duration, dir, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.Stdin = nil
	err := cmd.Run()
	return buf.String(), err
}

// Tail 取输出末尾 n 行、压成一行、限长——报错信息要能一眼看完。
func Tail(out string, n, maxLen int) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	s := strings.Join(lines, " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}
