package ccswitch

import (
	"fmt"
	"strings"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// verdict 是 cc-switch 对一件工具的结论。
type verdict struct {
	st     report.Status
	detail string
	hint   string
}

func ok(detail string) verdict         { return verdict{st: report.OK, detail: detail} }
func warn(detail, hint string) verdict { return verdict{st: report.Warn, detail: detail, hint: hint} }
func skip(detail string) verdict       { return verdict{st: report.Skip, detail: detail} }
func sameBase(a, b string) bool        { return probe.NormalizeBase(a) == probe.NormalizeBase(b) }
func useCmd(app string, p provider) string {
	return fmt.Sprintf("cc-switch use %q --app %s", p.ID, app)
}
func importCmd(app string) string { return "cc-switch provider import-live --app " + app }

// judge 是纯函数：所有事实从参数进来，方便把每种状态都穷举一遍。
//
// 判断顺序就是政策的顺序：原生能跑 → 不动，只说清谁在管；原生坏了 → 才看 cc-switch
// 里有没有能顶上的，而且先探再推荐。
func judge(app string, p harness.Posture, provs []provider, probe func(provider) (probeState, string)) verdict {
	if !p.Installed {
		return skip(app + " 没装，cc-switch 里的记录用不上")
	}
	cur, hasCur := currentOf(provs)
	standby := standbyNote(provs)

	// 原生坏了（或压根没配）—— 过渡时机。cc-switch 里存了什么都先探一遍。
	if p.Broken() || (p.Unconfigured() && !hasCur) {
		return fallback(app, p, provs, probe)
	}
	switch {
	case !hasCur && p.Official:
		return ok("原生官方登录（优先）· cc-switch 没接管" + standby)
	case !hasCur:
		return ok(fmt.Sprintf("原生自管 %s · cc-switch 没接管%s", p.BaseURL, standby))

	case cur.BaseURL == "" && p.Official:
		return ok(fmt.Sprintf("原生官方登录 · cc-switch 当前 %s 也是官方档%s", cur.ID, standby))
	case cur.BaseURL == "" && p.BaseURL != "":
		return warn(fmt.Sprintf("cc-switch 当前是官方档 %s，原生却指向 %s", cur.ID, p.BaseURL),
			"原生优先，照原生算。要让 cc-switch 的记录跟上："+importCmd(app))
	case cur.BaseURL == "":
		return ok(fmt.Sprintf("cc-switch 当前 %s（官方档）· 原生也没配网关，登录状态看上面 %s 那节", cur.ID, app))

	case p.Official:
		// 原生是官方，cc-switch 却指着网关：现在没事，但它下次一写回就把官方登录盖掉。
		return warn(fmt.Sprintf("原生是官方登录（优先），cc-switch 却记着 %s（%s）——记录过时了", cur.ID, cur.BaseURL),
			"现在不影响使用；但在 cc-switch 里再切一次会把官方登录盖掉。要让它跟上："+followOfficial(app, provs))
	case p.Unconfigured():
		// 原生什么都没有，cc-switch 却说它选了一个 —— 没写回成功。
		return fallback(app, p, provs, probe)
	case sameBase(cur.BaseURL, p.BaseURL):
		return ok(fmt.Sprintf("cc-switch 接管 · 与原生一致（%s）%s", p.BaseURL, standby))
	default:
		health := "能跑"
		if p.Healthy == nil {
			health = "没探"
		}
		return warn(fmt.Sprintf("原生 %s（%s），cc-switch 记着 %s（%s）", p.BaseURL, health, cur.ID, cur.BaseURL),
			fmt.Sprintf("原生优先，不用动。要让 cc-switch 跟上：%s；要以 cc-switch 为准：%s", importCmd(app), useCmd(app, cur)))
	}
}

// fallback 处理「原生指望不上」的情形：坏了、没配、或 cc-switch 说选了却没写回。
// 这是 cc-switch 作为 fallback 的用武之地 —— 但推荐之前必须探过。
func fallback(app string, p harness.Posture, provs []provider, probe func(provider) (probeState, string)) verdict {
	native := nativeText(app, p)
	cands := candidates(provs)
	if len(cands) == 0 {
		return ok(native + "；cc-switch 里也没存可用的备选，帮不上")
	}
	var tried []string
	for i, cand := range cands {
		if i == maxProbes {
			break
		}
		st, txt := probe(cand)
		switch st {
		case probeReachable:
			return warn(fmt.Sprintf("%s；cc-switch 里存着 %s（%s）%s", native, cand.ID, cand.BaseURL, txt),
				"切过去："+useCmd(app, cand))
		case probeUnknown:
			return warn(fmt.Sprintf("%s；cc-switch 里存着 %s（%s），%s", native, cand.ID, cand.BaseURL, txt),
				"切过去试试："+useCmd(app, cand))
		}
		tried = append(tried, fmt.Sprintf("%s（%s）%s", cand.ID, cand.BaseURL, txt))
	}
	return warn(native+"；cc-switch 里存的 "+strings.Join(tried, "、"),
		"两边都不通，多半是网关本身挂了——先看网关，再回来切")
}

// maxProbes 是 fallback 时最多探几个备选：够找到一个能用的就行，不把库里几十条全打一遍。
const maxProbes = 4

func nativeText(app string, p harness.Posture) string {
	switch {
	case p.Official:
		return fmt.Sprintf("原生官方登录有问题（看上面 %s 那节）", app)
	case p.BaseURL != "":
		return fmt.Sprintf("原生 %s 探不通（看上面 %s 那节）", p.BaseURL, app)
	default:
		return "原生没配置"
	}
}

// candidates 是可以顶上的备选：当前选中的排最前，官方档（没地址）排除 —— 官方档探不了也不是 fallback 的意思。
func candidates(provs []provider) []provider {
	var out []provider
	if cur, ok := currentOf(provs); ok && cur.BaseURL != "" {
		out = append(out, cur)
	}
	for _, p := range provs {
		if !p.Current && p.BaseURL != "" {
			out = append(out, p)
		}
	}
	return out
}

func currentOf(provs []provider) (provider, bool) {
	for _, p := range provs {
		if p.Current {
			return p, true
		}
	}
	return provider{}, false
}

// standbyNote 描述「存着但没启用」的那些 —— 政策允许备用不生效，这里只陈述，不催。
func standbyNote(provs []provider) string {
	var ids []string
	for _, p := range provs {
		if !p.Current {
			ids = append(ids, p.ID)
		}
	}
	switch n := len(ids); {
	case n == 0:
		return ""
	case n <= 3:
		return fmt.Sprintf("；另存 %d 个备用未启用（%s）", n, strings.Join(ids, "、"))
	default:
		return fmt.Sprintf("；另存 %d 个备用未启用（%s…）", n, strings.Join(ids[:3], "、"))
	}
}

// followOfficial 给出让 cc-switch 记录回到官方的命令：库里有官方档就切过去，没有就导入原生。
func followOfficial(app string, provs []provider) string {
	for _, p := range provs {
		if p.BaseURL == "" {
			return useCmd(app, p)
		}
	}
	return importCmd(app)
}
