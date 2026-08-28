package ui

import (
	"strings"
	"testing"
)

func TestDisplayWidthCountsCJKAsTwo(t *testing.T) {
	if got := displayWidth("ab中文"); got != 6 {
		t.Fatalf("width = %d, want 6", got)
	}
}

// 全角标点也要算两列——漏掉 ？ 会让整栏歪掉。
func TestDisplayWidthCoversFullwidthPunctuation(t *testing.T) {
	cases := map[string]int{
		"第一次用？":     10,
		"你是 agent？": 12,
		"想看某个命令":    12,
		"、。「」（）？！":  16,
		"ascii":     5,
	}
	for in, want := range cases {
		if got := displayWidth(in); got != want {
			t.Errorf("displayWidth(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPadDisplay(t *testing.T) {
	got := padDisplay("中文", 6)
	if got != "中文  " {
		t.Fatalf("pad = %q", got)
	}
}

func TestWrapKeepsNewlinesAndIndents(t *testing.T) {
	got := wrap("one two three four five\nsix", 10, 2)
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected wrapping, got %q", got)
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Fatalf("续行应缩进: %q", lines[1])
	}
}

func TestPlainPaletteIsIdentity(t *testing.T) {
	p := Plain()
	if p.Red("x") != "x" || p.Bold("y") != "y" {
		t.Fatalf("plain palette 不应着色")
	}
}
