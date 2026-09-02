package harness

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/shiftu/aivet/internal/probe"
)

// Bypass 描述「这件工具自己发请求时会带什么自定义头」，以及能把浏览器头写进配置的修复项。
//
// 背景：公网网关前面挂了 Cloudflare 时，默认 Go / Python http 客户端那种 UA 会被当 bot 拦成 403，
// 而 curl / 浏览器都是好的。hermes（providers.<p>.default_headers）和 pi（providers.<p>.headers）
// 都有往请求里塞自定义头的配置位，写个浏览器 UA 就能过；claude / codex / dsh 没有这种配置位。
type Bypass struct {
	Headers map[string]string // 工具配置里已经写好的自定义头；空 = 没配
	FixID   string            // 能写入浏览器头的 fixer；空 = 这件工具没有这个配置位
}

// BrowserUA 是写进配置的 User-Agent：一个平平无奇的桌面浏览器。
const BrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

// BypassHeaders 是修复时要写进配置的那几个头；client 用来标明是哪件工具（网关日志里好认）。
func BypassHeaders(client string) map[string]string {
	return map[string]string{"User-Agent": BrowserUA, "X-Client-Name": client}
}

// MergeHeaders 把 add 合并进 base（副本），add 里的键优先 —— 用户原来写的别的头一个不动。
func MergeHeaders(base, add map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(add))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range add {
		out[k] = v
	}
	return out
}

// IsPublicHTTPS 判断 base_url 是不是「公网 HTTPS 域名」—— 只有这种地址前面才可能挂着 Cloudflare。
// 本机 / 内网 / 明文 http 都不算，别在本地网关上唠叨自定义头的事。
func IsPublicHTTPS(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
	}
	return strings.Contains(host, ".")
}

// HeadersSummary 把头写成一行，给报告看：User-Agent=Mozilla/5.0 … · X-Client-Name=…。
func HeadersSummary(h map[string]string) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := h[k]
		if len(v) > 40 {
			v = v[:40] + "…"
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " · ")
}

// ReportHeaders 报一条「自定义请求头」的状态。只在公网 HTTPS 网关上报；本地网关一个字都不说。
//
// 没配不是错 —— 网关前面未必有 Cloudflare。所以没配也记 OK，只是把修复项的名字放在明面上；
// 真被拦了，网关探测那条会用 FixID 把它升级成红的。
func ReportHeaders(b *Builder, base, field string, headers map[string]string, fixID string) {
	if !IsPublicHTTPS(base) {
		return
	}
	if len(headers) > 0 {
		b.OK("headers", "自定义请求头", fmt.Sprintf("%s：%s", field, HeadersSummary(headers)))
		return
	}
	b.OK("headers", "自定义请求头", fmt.Sprintf("没配 %s —— 公网网关前面有 Cloudflare bot 拦截时才需要；被拦了跑 aivet fix %s", field, fixID))
}

// Reverify 是 fixer 写完自定义头之后的「回马枪」：按工具自己的样子（带头）再探一次，
// 把修好没修好当场说出来，别让用户写完还得自己再跑一遍 check 才知道。
// 结果走 c.Say 打给用户；offline / 没 key 时说明为什么没验。
func Reverify(c *Context, ep probe.Endpoint, model string) {
	switch {
	case c.Offline:
		c.Say("skip", "离线模式，没有重验；aivet check 复验")
		return
	case ep.Key == "" || ep.BaseURL == "":
		c.Say("skip", "没有 key 或 base_url，没法重验；aivet check 复验")
		return
	}
	var pr probe.PingResult
	if model != "" {
		pr = c.Gateways.Ping(c.Ctx, ep, model)
	} else {
		_, pr = c.Gateways.Models(c.Ctx, ep)
	}
	switch {
	case pr.OK:
		c.Say("ok", "重验：带上自定义请求头后通了 · "+pr.Detail)
	case pr.BotBlocked:
		c.Say("fail", "重验：带上自定义请求头仍被 Cloudflare 拦 · "+pr.Detail)
		c.Say("info", "换一个更像浏览器的 User-Agent 试试，或在 Cloudflare 控制台给这个路径加 WAF 放行规则")
	default:
		c.Say("warn", "重验：不再是 Cloudflare 拦截，但还有别的问题 · "+pr.Detail)
	}
}
