package ui

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/shiftu/aivet/internal/report"
)

// Printer 把 report 渲染成终端文本。
type Printer struct {
	W   io.Writer
	P   Palette
	Wid int
}

// Banner 打印标题栏。
func (pr Printer) Banner(version, osLabel string, live bool) {
	mode := "配置 + 网关探测"
	if live {
		mode = "配置 + 网关探测 + 真实跑一次"
	}
	fmt.Fprintf(pr.W, "\n%s %s  %s\n", pr.P.Bold(pr.P.Cyan("aivet")), pr.P.Dim("v"+version), pr.P.Dim("· "+osLabel))
	fmt.Fprintf(pr.W, "%s\n", pr.P.Dim("给套着缰绳的 AI 看病 · "+mode))
}

// Tool 打印一个工具的分区。
func (pr Printer) Tool(t report.Tool) {
	fmt.Fprintln(pr.W)
	head := pr.P.Bold(t.Label)
	meta := ""
	if t.Installed {
		if t.Version != "" {
			meta = pr.P.Dim(t.Version)
		}
	} else {
		meta = pr.P.Dim("未安装")
	}
	fmt.Fprintf(pr.W, "%s %s  %s\n", pr.P.Glyph(string(t.Worst())), head, meta)
	for _, c := range t.Checks {
		pr.check(c)
	}
}

func (pr Printer) check(c report.Check) {
	title := padDisplay(c.Title, 22)
	detail := c.Detail
	switch c.Status {
	case report.Skip:
		title = pr.P.Dim(title)
		detail = pr.P.Dim(detail)
	case report.Fail:
		title = pr.P.Red(title)
	case report.Warn:
		title = pr.P.Yellow(title)
	}
	fmt.Fprintf(pr.W, "   %s %s %s\n", pr.P.Glyph(string(c.Status)), title, wrap(detail, pr.Wid-30, 29))
	if c.Hint != "" && c.Status != report.OK {
		fmt.Fprintf(pr.W, "     %s %s\n", pr.P.Glyph("info"), wrap(c.Hint, pr.Wid-10, 7))
	}
	if c.FixID != "" && c.Status != report.OK {
		fmt.Fprintf(pr.W, "     %s %s\n", pr.P.Glyph("fix"), pr.P.Magenta("可自动修复：aivet fix "+c.FixID))
	}
}

// Summary 打印汇总行。
func (pr Printer) Summary(r report.Report) {
	c := r.Count()
	fmt.Fprintln(pr.W)
	fmt.Fprintln(pr.W, pr.P.Rule(pr.Wid))
	parts := []string{pr.P.Green(fmt.Sprintf("%d 通过", c.OK))}
	if c.Warn > 0 {
		parts = append(parts, pr.P.Yellow(fmt.Sprintf("%d 提醒", c.Warn)))
	}
	if c.Fail > 0 {
		parts = append(parts, pr.P.Red(fmt.Sprintf("%d 故障", c.Fail)))
	}
	if c.Skip > 0 {
		parts = append(parts, pr.P.Dim(fmt.Sprintf("%d 跳过", c.Skip)))
	}
	fmt.Fprintf(pr.W, "%s\n", strings.Join(parts, pr.P.Dim("  ·  ")))
	switch {
	case c.Fail > 0:
		fmt.Fprintf(pr.W, "%s\n", pr.P.Red("有工具用不了。")+"先试 "+pr.P.Bold("aivet fix")+"，修不掉的交给能用的 agent："+pr.P.Bold("aivet ask"))
	case c.Warn > 0:
		fmt.Fprintf(pr.W, "%s\n", pr.P.Yellow("都能用，但有几处值得看一眼。"))
	default:
		fmt.Fprintf(pr.W, "%s\n", pr.P.Green("全部通过，去干活吧。"))
	}
	fmt.Fprintln(pr.W)
}

// Line 打印一条独立的状态行（fix / setup 过程用）。
func (pr Printer) Line(status, text string) {
	fmt.Fprintf(pr.W, " %s %s\n", pr.P.Glyph(status), text)
}

// Section 打印一个小节标题。
func (pr Printer) Section(text string) {
	fmt.Fprintf(pr.W, "\n%s\n", pr.P.Bold(text))
}

// padDisplay 按显示宽度补齐（中文按 2 列算）。
func padDisplay(s string, width int) string {
	w := displayWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || r == '，' || r == '。' || r == '：' || r == '；' || r == '（' || r == '）' || r == '、' || r == '「' || r == '」' {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// wrap 把长文本按显示宽度折行，续行缩进 indent 列。多行 detail 原样保留换行。
func wrap(s string, width, indent int) string {
	if width < 20 {
		width = 20
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		out = append(out, wrapLine(para, width)...)
	}
	return strings.Join(out, "\n"+strings.Repeat(" ", indent))
}

func wrapLine(s string, width int) []string {
	var lines []string
	var cur strings.Builder
	curW := 0
	for _, word := range strings.Fields(s) {
		ww := displayWidth(word)
		if curW > 0 && curW+1+ww > width {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
		if curW > 0 {
			cur.WriteByte(' ')
			curW++
		}
		cur.WriteString(word)
		curW += ww
	}
	if cur.Len() > 0 || len(lines) == 0 {
		lines = append(lines, cur.String())
	}
	return lines
}
