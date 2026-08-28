package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// Progress 是一条会动的下载进度条。
//
// 它本身就是个 io.Writer —— io.Copy(io.MultiWriter(dst, bar), src) 写进去多少字节
// 就走多少进度，调用方不用自己数。
//
// Live=false 时一个字都不画，只在 Done 时说一句结果：管道、CI、重定向到文件的场景下，
// \r 刷出来的那几百行是垃圾而不是动效。
type Progress struct {
	W     io.Writer
	P     Palette
	Live  bool
	Label string
	Total int64 // 0 = 服务器没给 Content-Length，画不了百分比，改画转圈
	Width int   // 进度条本身占几列；0 走默认

	start   time.Time
	last    time.Time
	n       int64
	frame   int
	painted bool
	done    bool
}

// IsTTY 说明标准输出是不是终端。会动的东西只在终端里画 ——
// 这跟 NO_COLOR 是两件事：不要颜色的人也未必不要进度。
func IsTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

const (
	progressWidth = 24
	// 重画间隔。太密会闪，太疏看着像卡住。
	progressTick = 70 * time.Millisecond
)

func (b *Progress) Write(p []byte) (int, error) {
	b.Add(int64(len(p)))
	return len(p), nil
}

// Add 记下又下来了 n 个字节，够久了就重画一次。
func (b *Progress) Add(n int64) {
	if b.start.IsZero() {
		b.start = time.Now()
	}
	b.n += n
	if !b.Live || b.done {
		return
	}
	if now := time.Now(); now.Sub(b.last) >= progressTick {
		b.last = now
		b.frame++
		b.paint(false)
	}
}

// Done 收尾：满格画最后一帧（Live）或补一行结果（非 Live），然后换行。
func (b *Progress) Done() {
	if b.done {
		return
	}
	b.done = true
	if b.Live {
		b.paint(true)
		fmt.Fprintln(b.W)
		return
	}
	fmt.Fprintf(b.W, " %s %s  %s\n", b.P.Glyph("ok"), b.Label, b.P.Dim(HumanBytes(b.n)+" · "+b.elapsed()))
}

// Abort 把这行擦掉。下载失败时紧接着要打错误，进度条留在那儿只会碍事。
func (b *Progress) Abort() {
	b.done = true
	if b.Live && b.painted {
		fmt.Fprint(b.W, "\r\x1b[K")
	}
}

// N 是已经过去的字节数。
func (b *Progress) N() int64 { return b.n }

func (b *Progress) paint(full bool) {
	b.painted = true
	w := b.Width
	if w <= 0 {
		w = progressWidth
	}
	var mid string
	switch {
	case b.Total > 0:
		frac := float64(b.n) / float64(b.Total)
		if full || frac > 1 {
			frac = 1
		}
		mid = fmt.Sprintf("%s %3.0f%%  %s/%s",
			b.bar(w, frac), frac*100, HumanBytes(b.n), HumanBytes(b.Total))
	case full:
		mid = HumanBytes(b.n)
	default:
		// 不知道总大小就没有「还剩多少」可言，只能表示「还活着」。
		mid = b.spin() + "  " + HumanBytes(b.n)
	}
	fmt.Fprintf(b.W, "\r\x1b[K %s %s  %s  %s",
		b.P.Glyph("run"), b.Label, mid, b.P.Dim(b.rate()))
}

func (b *Progress) bar(w int, frac float64) string {
	full, empty := "█", "░"
	open, close := "▕", "▏"
	if b.P.ASCII() {
		full, empty, open, close = "#", ".", "[", "]"
	}
	n := int(frac*float64(w) + 0.5)
	if n > w {
		n = w
	}
	if n < 0 {
		n = 0
	}
	return open + b.P.Cyan(strings.Repeat(full, n)) + b.P.Dim(strings.Repeat(empty, w-n)) + close
}

func (b *Progress) spin() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if b.P.ASCII() {
		frames = []string{"|", "/", "-", "\\"}
	}
	return b.P.Cyan(frames[b.frame%len(frames)])
}

func (b *Progress) rate() string {
	d := time.Since(b.start)
	if b.start.IsZero() || d < 200*time.Millisecond {
		return ""
	}
	return HumanBytes(int64(float64(b.n)/d.Seconds())) + "/s"
}

func (b *Progress) elapsed() string {
	if b.start.IsZero() {
		return "0.0s"
	}
	return fmt.Sprintf("%.1fs", time.Since(b.start).Seconds())
}

// HumanBytes 把字节数写成人能读的样子。
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %sB", float64(n)/float64(div), []string{"K", "M", "G", "T"}[exp])
}
