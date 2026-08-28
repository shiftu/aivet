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
	if c.Offline {
		b.Skip("gateway", "网关探测", "离线模式，跳过")
		return
	}
	if ep.Key == "" {
		b.Skip("gateway", "网关探测", "没有 key，探不了")
		return
	}
	ids, pr := c.Gateways.Models(c.Ctx, ep)
	base := probe.NormalizeBase(ep.BaseURL)
	switch {
	case pr.OK:
		b.OK("gateway.reach", "网关可达", fmt.Sprintf("%s · %s", base, pr.Detail))
	case pr.Status == 404 || pr.Status == 405:
		// 不少网关 / 官方 Anthropic 端点没有 OpenAI 风格的清单接口，不算故障。
		b.Warn("gateway.reach", "网关可达", fmt.Sprintf("%s 通了，但没有模型清单接口（%d），跳过清单核对", base, pr.Status), "")
	case pr.Status == 401 || pr.Status == 403:
		b.Fail("gateway.reach", "网关认证", fmt.Sprintf("%s · %s", base, pr.Detail), "key 是不是贴错了 / 过期了？重新拿一把，或 aivet setup 重填")
		return
	case pr.Status == 0:
		b.Fail("gateway.reach", "网关可达", fmt.Sprintf("%s · %s", base, pr.Detail), "先在浏览器或 curl 里打开这个地址确认能连上；本机网关要先启动")
		return
	default:
		b.Fail("gateway.reach", "网关可达", fmt.Sprintf("%s · %s", base, pr.Detail), "")
		return
	}

	if model == "" {
		b.Warn("gateway.model", "模型名", "没有指定模型，工具会用自己的默认值——走网关时通常会 404", "把模型名写进配置（aivet setup 会一并写好）")
		return
	}
	if len(ids) > 0 {
		if probe.HasModel(ids, model) {
			b.OK("gateway.model", "模型在清单里", model)
		} else {
			b.Fail("gateway.model", "模型在清单里", fmt.Sprintf("网关没有 %q", model), "清单里长得像的："+strings.Join(closest(ids, model, 5), "、"))
			return
		}
	}
	ping := c.Gateways.Ping(c.Ctx, ep, model)
	if ping.OK {
		b.OK("gateway.ping", "真发一条请求", ping.Detail)
		return
	}
	if ping.Status == 403 && pr.OK {
		// 清单能拉说明 key 是对的；请求被 403 多半是网关按客户端指纹/模型放行
		// （例如「只允许 Codex 官方客户端」）。HTTP 探测被拒 ≠ 工具用不了。
		b.Warn("gateway.ping", "真发一条请求", ping.Detail, "网关按客户端或模型鉴权，aivet 的探测被拒不代表工具本身不能用；用 --live 让工具自己跑一次")
		return
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
}

// closest 按「共有子串」粗略挑出最像的几个模型名——够用就行，不上编辑距离。
func closest(ids []string, want string, n int) []string {
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
