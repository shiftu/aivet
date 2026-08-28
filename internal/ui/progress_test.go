package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"}, {512, "512 B"}, {1024, "1.0 KB"},
		{1536, "1.5 KB"}, {4_300_000, "4.1 MB"}, {2 << 30, "2.0 GB"},
	}
	for _, c := range cases {
		if got := HumanBytes(c.n); got != c.want {
			t.Errorf("HumanBytes(%d) = %q，期望 %q", c.n, got, c.want)
		}
	}
}

// 进度条得是个 io.Writer —— 调用方 io.Copy 一下就行，不用自己数字节。
func TestProgressCountsWhatIsWrittenThroughIt(t *testing.T) {
	var buf bytes.Buffer
	b := &Progress{W: &buf, P: Plain(), Live: true, Label: "x", Total: 100}
	if n, err := b.Write(make([]byte, 40)); n != 40 || err != nil {
		t.Fatalf("Write 返回 %d, %v", n, err)
	}
	b.Write(make([]byte, 60))
	if b.N() != 100 {
		t.Errorf("数出来 %d 字节，实际写了 100", b.N())
	}
	b.Done()
	if !strings.Contains(buf.String(), "100%") {
		t.Errorf("收尾那帧没画满：%q", buf.String())
	}
}

// 非终端下一个字都不许动画：管道、CI、重定向到文件时，\r 刷出来的是垃圾不是动效。
func TestProgressStaysQuietWhenNotLive(t *testing.T) {
	var buf bytes.Buffer
	b := &Progress{W: &buf, P: Plain(), Live: false, Label: "aivet_darwin_arm64", Total: 100}
	for i := 0; i < 100; i++ {
		b.Write([]byte{0})
	}
	if buf.Len() != 0 {
		t.Fatalf("还没收尾就已经吐了东西：%q", buf.String())
	}
	b.Done()
	out := buf.String()
	if strings.Contains(out, "\r") {
		t.Error("非 Live 模式不该用 \\r 回车重画")
	}
	if !strings.Contains(out, "aivet_darwin_arm64") || !strings.Contains(out, "100 B") {
		t.Errorf("收尾这行既没说下的是什么也没说下了多少：%q", out)
	}
}

// 下载失败时紧接着要打错误，半截的进度条留在那儿只会碍事。
func TestProgressAbortClearsTheLine(t *testing.T) {
	var buf bytes.Buffer
	b := &Progress{W: &buf, P: Plain(), Live: true, Label: "x", Total: 100}
	b.Write(make([]byte, 10))
	b.paint(false) // 逼它画一帧，不然节流会把这次吞掉
	buf.Reset()
	b.Abort()
	if !strings.Contains(buf.String(), "\x1b[K") {
		t.Errorf("Abort 没把那行擦掉：%q", buf.String())
	}
	buf.Reset()
	b.Done()
	if buf.Len() != 0 {
		t.Error("Abort 之后 Done 不该再吐东西")
	}
}

// 服务器不给 Content-Length 时画不了百分比，但不能因此就一动不动 ——
// 那样看着像卡死了。
func TestProgressSpinsWhenSizeUnknown(t *testing.T) {
	var buf bytes.Buffer
	b := &Progress{W: &buf, P: Plain(), Live: true, Label: "x", Total: 0}
	b.Write(make([]byte, 2048))
	b.paint(false)
	out := buf.String()
	if strings.Contains(out, "%") {
		t.Error("不知道总大小却画了百分比，那个数是编的")
	}
	if !strings.Contains(out, "2.0 KB") {
		t.Errorf("至少得说已经下了多少：%q", out)
	}
}

// ASCII 终端画不出 █ 和 ▕，得退化掉，不然满屏问号。
func TestProgressFallsBackToASCII(t *testing.T) {
	var buf bytes.Buffer
	b := &Progress{W: &buf, P: Palette{ascii: true}, Live: true, Label: "x", Total: 10}
	b.Write(make([]byte, 5))
	b.paint(false)
	out := buf.String()
	if strings.ContainsAny(out, "█░▕▏") {
		t.Errorf("ASCII 模式下还在用 Unicode 图形：%q", out)
	}
	if !strings.Contains(out, "#") {
		t.Errorf("ASCII 模式该用 # 画：%q", out)
	}
}
