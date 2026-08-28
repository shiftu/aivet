// Package ui 负责终端排版：颜色、图标、对齐。
//
// 设计原则：一眼看出「哪件工具坏了、坏在哪、下一步敲什么」。
// 尊重 NO_COLOR；非 tty 或 Windows 老控制台自动退化为纯文本 / ASCII。
package ui

import (
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// Palette 是一组着色函数；关闭颜色时全部为恒等函数。
type Palette struct {
	enabled bool
	ascii   bool
}

// New 根据环境决定是否着色、是否用 Unicode 图标。
func New() Palette {
	color := term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	ascii := os.Getenv("AIVET_ASCII") == "1"
	if runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "" && os.Getenv("TERM_PROGRAM") == "" {
		// 旧版 conhost 常常不是 UTF-8 代码页，Unicode 图标会变成问号。
		ascii = true
	}
	if color {
		enableVT() // Windows 需要显式开启 ANSI 序列；其他平台是空操作
	}
	return Palette{enabled: color, ascii: ascii}
}

// Plain 返回不着色的调色板（--json / 非交互用）。
func Plain() Palette { return Palette{} }

// ASCII 说明这台终端画不了 Unicode 图形，进度条之类要退化成 ASCII。
func (p Palette) ASCII() bool { return p.ascii }

func (p Palette) wrap(code, s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p Palette) Bold(s string) string    { return p.wrap("1", s) }
func (p Palette) Dim(s string) string     { return p.wrap("2", s) }
func (p Palette) Green(s string) string   { return p.wrap("32", s) }
func (p Palette) Yellow(s string) string  { return p.wrap("33", s) }
func (p Palette) Red(s string) string     { return p.wrap("31", s) }
func (p Palette) Cyan(s string) string    { return p.wrap("36", s) }
func (p Palette) Blue(s string) string    { return p.wrap("34", s) }
func (p Palette) Magenta(s string) string { return p.wrap("35", s) }

// Glyph 返回状态图标（已着色）。
func (p Palette) Glyph(status string) string {
	uni := map[string]string{"ok": "✔", "warn": "▲", "fail": "✘", "skip": "○", "run": "…", "fix": "⚒", "info": "›"}
	asc := map[string]string{"ok": "+", "warn": "!", "fail": "x", "skip": "-", "run": "~", "fix": "*", "info": ">"}
	g := uni[status]
	if p.ascii {
		g = asc[status]
	}
	switch status {
	case "ok":
		return p.Green(g)
	case "warn":
		return p.Yellow(g)
	case "fail":
		return p.Red(g)
	case "fix":
		return p.Magenta(g)
	case "run", "info":
		return p.Cyan(g)
	default:
		return p.Dim(g)
	}
}

// Rule 返回一条水平分隔线。
func (p Palette) Rule(width int) string {
	ch := "─"
	if p.ascii {
		ch = "-"
	}
	return p.Dim(strings.Repeat(ch, width))
}

// Width 返回终端宽度（非 tty 时 80）。
func Width() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 40 {
		return 80
	}
	if w > 110 {
		return 110
	}
	return w
}
