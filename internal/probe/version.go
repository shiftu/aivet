package probe

import (
	"regexp"
	"strconv"
)

// verRe 从一行输出里抠出第一个 x.y.z / x.y 形式的版本号。
//
// 各工具的 --version 输出五花八门：`0.45.0`、`codex-cli 0.137.0`、
// `hermes 1.2.3 · build abc`。与其约定格式，不如把数字抠出来 ——
// 这是唯一一种对所有工具都成立的读法。
var verRe = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion 从任意一行里解析版本号；解析不出来返回 ok=false。
func ParseVersion(s string) (v [3]int, ok bool) {
	m := verRe.FindStringSubmatch(s)
	if m == nil {
		return v, false
	}
	for i := 0; i < 3; i++ {
		if m[i+1] != "" {
			v[i], _ = strconv.Atoi(m[i+1])
		}
	}
	return v, true
}

// CompareVersions 比较两个版本串：a<b 返回 -1，相等 0，a>b 返回 1。
// 任何一边解析不出版本号就返回 ok=false —— 调用方必须自己决定
// 「读不出版本」该怎么办，绝不能默认当成「版本够新」。
func CompareVersions(a, b string) (int, bool) {
	va, ok1 := ParseVersion(a)
	vb, ok2 := ParseVersion(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		switch {
		case va[i] < vb[i]:
			return -1, true
		case va[i] > vb[i]:
			return 1, true
		}
	}
	return 0, true
}
