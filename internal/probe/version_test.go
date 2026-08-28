package probe

import "testing"

func TestParseVersionAcceptsRealWorldOutput(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"0.45.0", [3]int{0, 45, 0}, true},
		{"codex-cli 0.137.0", [3]int{0, 137, 0}, true},
		{"hermes 1.2.3 · build abc", [3]int{1, 2, 3}, true},
		{"v2.10", [3]int{2, 10, 0}, true},
		{"仅数据目录（GUI）", [3]int{}, false},
		{"", [3]int{}, false},
	} {
		got, ok := ParseVersion(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseVersion(%q) = %v,%v；想要 %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCompareVersionsOrdersNumerically(t *testing.T) {
	// 字符串比较会把 0.9 排在 0.137 后面 —— 版本断言正是靠这里不出错。
	if c, ok := CompareVersions("0.9.0", "0.137.0"); !ok || c != -1 {
		t.Errorf("0.9.0 应小于 0.137.0，得到 %d,%v", c, ok)
	}
	if c, ok := CompareVersions("codex-cli 0.140.0", "0.137.0"); !ok || c != 1 {
		t.Errorf("0.140.0 应大于 0.137.0，得到 %d,%v", c, ok)
	}
	if c, ok := CompareVersions("1.2.3", "1.2.3"); !ok || c != 0 {
		t.Errorf("相等判断错了：%d,%v", c, ok)
	}
}

// 读不出版本时必须明说读不出来。悄悄当成「够新」或「够旧」，
// 就是拿一个猜测去下确定性的结论 —— 那正是版本断言要避免的事。
func TestCompareVersionsRefusesToGuess(t *testing.T) {
	if _, ok := CompareVersions("", "0.137.0"); ok {
		t.Error("空版本号不该给出比较结果")
	}
	if _, ok := CompareVersions("Error: spawn codex ENOENT", "0.137.0"); ok {
		t.Error("跑挂的输出不该被当成版本号比较")
	}
}
