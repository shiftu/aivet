package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaskKey(t *testing.T) {
	if MaskKey("") != "(空)" || MaskKey("short") != "****" {
		t.Fatal("短串应全遮")
	}
	if got := MaskKey("sk-abcdefghijklmnop"); got != "sk-abc****mnop" {
		t.Fatalf("mask = %q", got)
	}
}

func TestDotenvRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# comment\nexport A=1\nB=\"two\"\n"), 0o600)
	m := ParseDotenv(p)
	if m["A"] != "1" || m["B"] != "two" {
		t.Fatalf("parse = %v", m)
	}
	if err := UpsertDotenv(p, "A", "9"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertDotenv(p, "C", "3"); err != nil {
		t.Fatal(err)
	}
	m = ParseDotenv(p)
	if m["A"] != "9" || m["B"] != "two" || m["C"] != "3" {
		t.Fatalf("after upsert = %v", m)
	}
	b, _ := os.ReadFile(p)
	if string(b)[:9] != "# comment" {
		t.Fatalf("注释应保留: %q", b)
	}
}

func TestBackupMissingFileIsNoop(t *testing.T) {
	bak, err := Backup(filepath.Join(t.TempDir(), "nope"))
	if err != nil || bak != "" {
		t.Fatalf("bak=%q err=%v", bak, err)
	}
}

func TestTail(t *testing.T) {
	got := Tail("a\nb\nc\nd", 2, 100)
	if got != "c d" {
		t.Fatalf("tail = %q", got)
	}
	if got := Tail("xxxxxxxxxx", 1, 4); got != "xxxx…" {
		t.Fatalf("truncate = %q", got)
	}
}
