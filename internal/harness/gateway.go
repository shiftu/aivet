package harness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shiftu/aivet/internal/probe"
)

// ProbeGateway 是所有工具共用的「网关三连」：清单 → 模型在不在 → 真发一条。
//
// 顺序有讲究：清单拉不到多半是地址/网络问题，此时不再发请求，避免一个根因刷出三条红。
func ProbeGateway(c *Context, b *Builder, ep probe.Endpoint, model string) {
	ProbeGatewayWith(c, b, ep, model, Bypass{})
}

// ProbeGatewayWith 同上，但知道这件工具自己会带哪些自定义头、以及被 Cloudflare 拦了能指向哪个修复项。
//
// 返回「后续探测该用的端点」：aivet 默认不带工具的自定义头去探（探测路径 ≠ 工具路径，得诚实），
// 但要是不带头被 Cloudflare 拦、带上配置里的头就通，那工具本身是好的 —— 后面的探测就都按带头的走，
// 调用方拿这个端点继续做副槽核对之类，别再撞一次 403。
func ProbeGatewayWith(c *Context, b *Builder, ep probe.Endpoint, model string, bp Bypass) probe.Endpoint {
	ids, ep, ok := probeReach(c, b, ep, bp)
	if !ok {
		return ep
	}
	return probeModel(c, b, ep, model, ids, bp)
}

// ProbeReach 只做第一连：网关到底通不通。返回模型清单（可能为空）和「能不能继续往下探」。
// 拆出来是因为有一类情况只能查到这里为止 —— 比如 Claude Code 的模型别名，
// 真正发给网关的名字是工具内部解析出来的，我们看不到，再往下探就是猜。
func ProbeReach(c *Context, b *Builder, ep probe.Endpoint) ([]string, bool) {
	ids, _, ok := probeReach(c, b, ep, Bypass{})
	return ids, ok
}

func probeReach(c *Context, b *Builder, ep probe.Endpoint, bp Bypass) ([]string, probe.Endpoint, bool) {
	if c.Offline {
		b.Skip("gateway", "网关探测", "离线模式，跳过")
		return nil, ep, false
	}
	if ep.Key == "" {
		b.Skip("gateway", "网关探测", "没有 key，探不了")
		return nil, ep, false
	}
	ids, pr := c.Gateways.Models(c.Ctx, ep)
	base := probe.NormalizeBase(ep.BaseURL)
	note := ""
	if pr.BotBlocked && len(bp.Headers) > 0 && len(ep.Headers) == 0 {
		// 不带头被拦，但工具自己是带头发的 —— 按工具的样子再探一次，才知道工具到底能不能用。
		ep = ep.WithHeaders(bp.Headers)
		ids, pr = c.Gateways.Models(c.Ctx, ep)
		note = "（不带自定义头被 Cloudflare 拦，改按配置里的请求头探）"
	}
	switch {
	case pr.OK:
		b.OK("gateway.reach", "网关可达", fmt.Sprintf("%s · %s%s", base, pr.Detail, note))
	case pr.Status == 404 || pr.Status == 405:
		// 不少网关 / 官方 Anthropic 端点没有 OpenAI 风格的清单接口，不算故障。
		b.Warn("gateway.reach", "网关可达", fmt.Sprintf("%s 通了，但没有模型清单接口（%d），跳过清单核对%s", base, pr.Status, note), "")
	case pr.BotBlocked:
		botBlocked(b, "gateway.reach", "网关可达", report403{base: base, detail: pr.Detail, ep: ep, bp: bp, fail: true})
		return nil, ep, false
	case pr.Status == 401 || pr.Status == 403:
		b.Fail("gateway.reach", "网关认证", fmt.Sprintf("%s · %s", base, pr.Detail), "key 是不是贴错了 / 过期了？重新拿一把，或 aivet setup 重填")
		return nil, ep, false
	case pr.Status == 0:
		b.Fail("gateway.reach", "网关可达", fmt.Sprintf("%s · %s", base, pr.Detail), "先在浏览器或 curl 里打开这个地址确认能连上；本机网关要先启动")
		return nil, ep, false
	default:
		b.Fail("gateway.reach", "网关可达", fmt.Sprintf("%s · %s", base, pr.Detail), "")
		return nil, ep, false
	}
	return ids, ep, true
}

// report403 是 botBlocked 要的材料。
type report403 struct {
	base, detail string
	ep           probe.Endpoint
	bp           Bypass
	fail         bool // true = Fail；false = Warn（清单能拉、只是请求被拦）
}

// botBlocked 记一条「Cloudflare 把 aivet 当 bot 拦了」，并按这件工具的情况给出路：
//
//   - 有配置位、还没配 → 指向 fixer（aivet fix <id> 写浏览器头后重验）
//   - 已经配了、带上也被拦 → 头不够像浏览器，或 Cloudflare 规则更严，得去网关侧放行
//   - 没有配置位（claude / codex / dsh） → 说清楚 aivet 探测被拦 ≠ 工具被拦，让工具自己跑一次
func botBlocked(b *Builder, id, title string, r report403) {
	detail := r.base + " · " + r.detail
	add := func(hint, fix string) {
		switch {
		case r.fail && fix != "":
			b.FailFix(id, title, detail, hint, fix)
		case r.fail:
			b.Fail(id, title, detail, hint)
		case fix != "":
			b.WarnFix(id, title, detail, hint, fix)
		default:
			b.Warn(id, title, detail, hint)
		}
	}
	switch {
	case len(r.ep.Headers) > 0:
		add("配置里的自定义请求头带上了也被拦 —— 换一个更像浏览器的 User-Agent，或在 Cloudflare 控制台给这个路径加 WAF 放行规则；aivet 的探测被拦不等于工具一定被拦，--live 让它自己跑一次", "")
	case r.bp.FixID != "":
		add(fmt.Sprintf("网关前面的 Cloudflare 把 aivet 的请求当 bot 拦了；aivet fix %s 会往配置写一个浏览器 User-Agent 并立刻重验", r.bp.FixID), r.bp.FixID)
	default:
		add("网关前面的 Cloudflare 把 aivet 的探测当 bot 拦了；这件工具没有自定义请求头的配置位，只能在 Cloudflare 侧放行。aivet 被拦不等于工具被拦，--live 让它自己跑一次", "")
	}
}

// probeModel 是第二、三连：模型在不在清单里、真发一条能不能通。返回后续该用的端点。
func probeModel(c *Context, b *Builder, ep probe.Endpoint, model string, ids []string, bp Bypass) probe.Endpoint {
	if model == "" {
		b.Warn("gateway.model", "模型名", "没有指定模型，工具会用自己的默认值——走网关时通常会 404", "把模型名写进配置（aivet setup 会一并写好）")
		return ep
	}
	if len(ids) > 0 {
		if probe.HasModel(ids, model) {
			b.OK("gateway.model", "模型在清单里", model)
		} else {
			b.Fail("gateway.model", "模型在清单里", fmt.Sprintf("网关没有 %q", model), "清单里长得像的："+strings.Join(Similar(ids, model, 5), "、"))
			return ep
		}
	}
	ping := c.Gateways.Ping(c.Ctx, ep, model)
	note := ""
	if ping.BotBlocked && len(bp.Headers) > 0 && len(ep.Headers) == 0 {
		// 清单能拉但 POST 被拦（Cloudflare 常只挑战写请求）：按工具自己的头再发一次。
		ep = ep.WithHeaders(bp.Headers)
		ping = c.Gateways.Ping(c.Ctx, ep, model)
		note = "（不带自定义头被 Cloudflare 拦，改按配置里的请求头发）"
	}
	if ping.OK {
		b.OK("gateway.ping", "真发一条请求", ping.Detail+note)
		return ep
	}
	if ping.BotBlocked {
		// 清单能拉说明 key 是对的，只是 POST 被 Cloudflare 拦 —— 降成 Warn，并指向修复项。
		botBlocked(b, "gateway.ping", "真发一条请求", report403{base: probe.NormalizeBase(ep.BaseURL), detail: ping.Detail, ep: ep, bp: bp, fail: len(ids) == 0})
		return ep
	}
	if ping.Status == 403 && len(ids) > 0 {
		// 清单能拉说明 key 是对的；请求被 403 多半是网关按客户端指纹/模型放行
		// （例如「只允许 Codex 官方客户端」）。HTTP 探测被拒 ≠ 工具用不了。
		b.Warn("gateway.ping", "真发一条请求", ping.Detail, "网关按客户端或模型鉴权，aivet 的探测被拒不代表工具本身不能用；用 --live 让工具自己跑一次")
		return ep
	}
	hint := ""
	switch ping.Status {
	case 404:
		hint = fmt.Sprintf("网关可能不支持 %s 协议；或 base_url 多/少了 /v1", ep.Protocol)
	case 400, 422:
		hint = "多半是模型名对不上；看看上面清单里有没有它"
	case 401, 403:
		hint = "清单能拉、请求被拒——key 对这个模型没权限，或网关按模型鉴权"
	}
	b.Fail("gateway.ping", "真发一条请求", ping.Detail, hint)
	return ep
}

// Similar 按「共有子串」粗略挑出最像的几个模型名——够用就行，不上编辑距离。
func Similar(ids []string, want string, n int) []string {
	type scored struct {
		id    string
		score int
	}
	w := strings.ToLower(want)
	var out []scored
	for _, id := range ids {
		l := strings.ToLower(id)
		s := 0
		for _, part := range strings.FieldsFunc(w, func(r rune) bool { return r == '-' || r == '/' || r == '.' || r == '_' }) {
			if len(part) >= 2 && strings.Contains(l, part) {
				s += len(part)
			}
		}
		out = append(out, scored{id, s})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	var res []string
	for i := 0; i < len(out) && i < n; i++ {
		res = append(res, out[i].id)
	}
	return res
}
