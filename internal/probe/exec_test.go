package probe

import (
	"runtime"
	"testing"
)

// 跑不起来的东西不能把错误信息当版本号显示——那正是 codex 装坏时发生的事。
func TestVersionOrSeparatesBrokenFromVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("依赖 /bin/sh")
	}
	v, broken := VersionOr("/bin/sh", "-c", "echo 'Error: spawn codex ENOENT' >&2; exit 1")
	if v != "" {
		t.Fatalf("跑挂了不该有版本号，得到 %q", v)
	}
	if broken == "" || !contains(broken, "ENOENT") {
		t.Fatalf("broken 应带上原始报错：%q", broken)
	}
	v, broken = VersionOr("/bin/sh", "-c", "echo 1.2.3")
	if v != "1.2.3" || broken != "" {
		t.Fatalf("正常情况 v=%q broken=%q", v, broken)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
