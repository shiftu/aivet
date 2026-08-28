package ccswitch

// 这组测试把政策穷举一遍：原生优先、官方为正、cc-switch 是 fallback。
// 每一行是一种「谁在管 × 原生能不能跑 × cc-switch 存了什么」的组合，
// 断言的是结论的档次和关键措辞 —— 措辞就是用户看到的政策。

import (
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/report"
)

func bp(v bool) *bool { return &v }

var (
	official = harness.Posture{Installed: true, Official: true, Healthy: bp(true)}
	gwOK     = harness.Posture{Installed: true, BaseURL: "http://localhost:7421/v1", Healthy: bp(true)}
	gwBroken = harness.Posture{Installed: true, BaseURL: "http://localhost:7421/v1", Healthy: bp(false)}
	gwUnseen = harness.Posture{Installed: true, BaseURL: "http://localhost:7421/v1"} // 只读了配置，没探
	nothing  = harness.Posture{Installed: true, Healthy: bp(false)}
	absent   = harness.Posture{}

	offCard = provider{ID: "claude-official"}
	gwCard  = provider{ID: "gw", BaseURL: "http://localhost:7421/v1"}
	gwCur   = provider{ID: "gw", BaseURL: "http://localhost:7421/v1", Current: true}
	other   = provider{ID: "other", BaseURL: "https://proxy.iher.ai", Current: true}
)

func reachable(provider) (probeState, string) {
	return probeReachable, "探过是通的（3 个模型）"
}
func unreachable(provider) (probeState, string) { return probeUnreachable, "也探不通（401）" }
func unknown(provider) (probeState, string)     { return probeUnknown, "离线没探" }
func mustNotProbe(t *testing.T) func(provider) (probeState, string) {
	return func(p provider) (probeState, string) {
		t.Fatalf("原生能跑就不该去探 cc-switch 里的 %s", p.ID)
		return probeUnknown, ""
	}
}

func TestJudgeFollowsThePolicy(t *testing.T) {
	cases := []struct {
		name   string
		p      harness.Posture
		provs  []provider
		probe  func(provider) (probeState, string)
		want   report.Status
		detail string // 必须出现的片段
		hint   string // 必须出现的片段（空 = 不检查）
	}{
		{"没装就跳过", absent, []provider{gwCur}, nil, report.Skip, "没装", ""},

		// 官方登录为正：cc-switch 空着、或存着备用不启用，都是 OK，不催。
		{"官方 · cc-switch 空", official, nil, nil, report.OK, "官方登录（优先）", ""},
		{"官方 · 备用未启用", official, []provider{gwCard}, nil, report.OK, "另存 1 个备用未启用（gw）", ""},
		{"官方 · cc-switch 也是官方档", official, []provider{{ID: "claude-official", Current: true}}, nil, report.OK, "也是官方档", ""},
		// 原生官方、cc-switch 却指着网关：现在没事，但下次写回会盖掉 —— 提醒，不算故障。
		{"官方 · cc-switch 记着网关", official, []provider{gwCur, offCard}, nil, report.Warn, "记录过时了", `cc-switch use "claude-official"`},
		{"官方 · cc-switch 记着网关 · 库里没官方档", official, []provider{gwCur}, nil, report.Warn, "记录过时了", "import-live"},

		// 原生网关能跑：谁在管说清楚，不推销。
		{"原生自管 · cc-switch 空", gwOK, nil, nil, report.OK, "原生自管", ""},
		{"原生自管 · 备用未启用", gwOK, []provider{gwCard}, mustNotProbe(t), report.OK, "备用未启用", ""},
		{"接管一致", gwOK, []provider{gwCur}, mustNotProbe(t), report.OK, "接管 · 与原生一致", ""},
		{"漂移 · 原生能跑", gwOK, []provider{other}, mustNotProbe(t), report.Warn, "能跑", "原生优先，不用动"},
		{"漂移 · 原生没探", gwUnseen, []provider{other}, mustNotProbe(t), report.Warn, "没探", "原生优先"},
		{"cc-switch 官方档 · 原生网关", gwOK, []provider{{ID: "claude-official", Current: true}}, nil, report.Warn, "原生却指向", "import-live"},

		// 原生坏了：这才是 fallback 的时刻 —— 先探再推荐。
		{"原生坏 · 备选通", gwBroken, []provider{other}, reachable, report.Warn, "探过是通的", `cc-switch use "other"`},
		{"原生坏 · 备选也不通", gwBroken, []provider{other}, unreachable, report.Warn, "也探不通", "先看网关"},
		{"原生坏 · 备选没探成", gwBroken, []provider{other}, unknown, report.Warn, "离线没探", "试试"},
		{"原生坏 · 备选是未启用的", gwBroken, []provider{gwCard}, reachable, report.Warn, "存着 gw", `cc-switch use "gw"`},
		{"原生坏 · 只有官方档", gwBroken, []provider{offCard}, mustNotProbe(t), report.OK, "帮不上", ""},
		{"原生坏 · cc-switch 空", gwBroken, nil, nil, report.OK, "帮不上", ""},
		{"原生没配 · cc-switch 有备选", nothing, []provider{gwCard}, reachable, report.Warn, "原生没配置", "cc-switch use"},
		{"原生没配 · cc-switch 说选了却没写回", nothing, []provider{gwCur}, reachable, report.Warn, "原生没配置", `cc-switch use "gw"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := judge("claude", tc.p, tc.provs, tc.probe)
			if v.st != tc.want {
				t.Fatalf("status = %s, want %s\n  detail: %s\n  hint:   %s", v.st, tc.want, v.detail, v.hint)
			}
			if !strings.Contains(v.detail, tc.detail) {
				t.Fatalf("detail %q 缺 %q", v.detail, tc.detail)
			}
			if tc.hint != "" && !strings.Contains(v.hint, tc.hint) {
				t.Fatalf("hint %q 缺 %q", v.hint, tc.hint)
			}
		})
	}
}

// 探测预算：库里存了一堆都不通时，不该把全部打一遍，也不该漏掉排在前面能用的那个。
func TestFallbackProbesCurrentFirstAndStops(t *testing.T) {
	provs := []provider{
		{ID: "a", BaseURL: "http://a"}, {ID: "b", BaseURL: "http://b"},
		{ID: "cur", BaseURL: "http://cur", Current: true},
		{ID: "c", BaseURL: "http://c"}, {ID: "d", BaseURL: "http://d"}, {ID: "e", BaseURL: "http://e"},
	}
	var order []string
	probe := func(p provider) (probeState, string) {
		order = append(order, p.ID)
		if p.ID == "b" {
			return probeReachable, "通"
		}
		return probeUnreachable, "不通"
	}
	v := judge("codex", gwBroken, provs, probe)
	if strings.Join(order, ",") != "cur,a,b" {
		t.Fatalf("探测顺序 %v：当前选中的先探，找到能用的就停", order)
	}
	if v.st != report.Warn || !strings.Contains(v.hint, `cc-switch use "b"`) {
		t.Fatalf("%+v", v)
	}

	order = nil
	allDead := func(p provider) (probeState, string) { order = append(order, p.ID); return probeUnreachable, "不通" }
	v = judge("codex", gwBroken, provs, allDead)
	if len(order) != maxProbes {
		t.Fatalf("全不通时探了 %d 个，预算是 %d", len(order), maxProbes)
	}
	if !strings.Contains(v.hint, "先看网关") {
		t.Fatalf("%+v", v)
	}
}

func TestKeyFromSettings(t *testing.T) {
	c := &harness.Context{Home: t.TempDir(), Env: func(k string) string {
		if k == "MY_KEY" {
			return "from-env"
		}
		return ""
	}}
	if got := keyFromSettings(c, "claude", `{"env":{"ANTHROPIC_AUTH_TOKEN":"tok"}}`); got != "tok" {
		t.Fatalf("claude = %q", got)
	}
	if got := keyFromSettings(c, "codex", `{"auth":{"OPENAI_API_KEY":"sk"}}`); got != "sk" {
		t.Fatalf("codex = %q", got)
	}
	if got := keyFromSettings(c, "hermes", `{"key_env":"MY_KEY"}`); got != "from-env" {
		t.Fatalf("hermes key_env = %q", got)
	}
	if got := keyFromSettings(c, "hermes", `{"api_key":"plain"}`); got != "plain" {
		t.Fatalf("hermes api_key = %q", got)
	}
	if got := keyFromSettings(c, "codex", `not json`); got != "" {
		t.Fatalf("garbage = %q", got)
	}
}
