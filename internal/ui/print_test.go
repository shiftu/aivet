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
