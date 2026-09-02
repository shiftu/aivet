package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Protocol 是工具与网关之间的线协议。
type Protocol string

const (
	ChatCompletions Protocol = "chat"      // POST /chat/completions
	Responses       Protocol = "responses" // POST /responses（codex 0.137+ 只认这个）
	Anthropic       Protocol = "anthropic" // POST /v1/messages（claude code）
)

// Endpoint 描述一个「网关 + 凭证」组合。
type Endpoint struct {
	BaseURL  string   // 例：http://127.0.0.1:7421/v1 或 https://api.anthropic.com
	Key      string   // Bearer / x-api-key
	Protocol Protocol //
	// Headers 是额外要带上的请求头（工具配置里的 default_headers / headers 之类）。
	// aivet 自己的探测默认不带 —— 探测路径和工具路径本来就不是一回事，
	// 只有在「模拟工具自己会怎么发」时才填进来，比如修完自定义头之后重验。
	Headers map[string]string
}

// WithHeaders 返回一个带上这些请求头的副本（不改原来的）。
func (ep Endpoint) WithHeaders(h map[string]string) Endpoint {
	out := ep
	out.Headers = make(map[string]string, len(ep.Headers)+len(h))
	for k, v := range ep.Headers {
		out.Headers[k] = v
	}
	for k, v := range h {
		out.Headers[k] = v
	}
	return out
}

// PingResult 是一次探测结果。
type PingResult struct {
	OK      bool
	Status  int    // HTTP 状态码（0 = 没连上）
	Detail  string // 一句人话
	Elapsed time.Duration
	// BotBlocked：这个 403 不是网关自己拒的，是前面的 Cloudflare 把请求当 bot 拦了。
	// 区分这个很要紧 —— 前者要换 key，后者只要请求头长得像浏览器一点。
	BotBlocked bool
}

var httpClient = &http.Client{Timeout: 25 * time.Second}

// NormalizeBase 去掉尾部斜杠。
func NormalizeBase(u string) string {
	return strings.TrimRight(strings.TrimSpace(u), "/")
}

// ModelsURL 推断 /models 的地址：base 已带 /v1 就直接拼，否则补 /v1。
func ModelsURL(base string) string {
	base = NormalizeBase(base)
	if strings.HasSuffix(base, "/v1") {
		return base + "/models"
	}
	return base + "/v1/models"
}

// ModelInfo 是清单里的一条模型。
//
// 除了 id，不少网关还会给出上下文长度和最大输出 —— 有就用它，
// 别再让 aivet 拿一个写死的保守值去猜（猜小了工具会白白截断长上下文）。
// 网关没给就是 0，调用方自己决定退回什么默认值。
type ModelInfo struct {
	ID                  string `json:"id"`
	ContextLength       int    `json:"context_length"`
	MaxCompletionTokens int    `json:"max_completion_tokens"`
}

// ModelIDs 只取 id。
func ModelIDs(ms []ModelInfo) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// FindModel 在清单里按 id 找一条；找不到返回 false。
func FindModel(ms []ModelInfo, id string) (ModelInfo, bool) {
	for _, m := range ms {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// ListModels 拉网关的模型清单（OpenAI 风格 {data:[{id}]}）。
// Anthropic 官方也有 /v1/models，但要 x-api-key 头。
func ListModels(ctx context.Context, ep Endpoint) ([]ModelInfo, PingResult) {
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ModelsURL(ep.BaseURL), nil)
	setAuth(req, ep)
	setHeaders(req, ep)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, PingResult{Detail: connErr(err), Elapsed: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	pr := PingResult{Status: resp.StatusCode, Elapsed: time.Since(start)}
	if resp.StatusCode != 200 {
		pr.Detail, pr.BotBlocked = describeErr(resp, body)
		return nil, pr
	}
	var parsed struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Data) == 0 {
		pr.Detail = "返回了 200 但不是模型清单（不像 OpenAI 兼容接口）"
		return nil, pr
	}
	pr.OK = true
	pr.Detail = fmt.Sprintf("%d 个模型", len(parsed.Data))
	return parsed.Data, pr
}

// Ping 用指定协议发一条最小请求（max_tokens=1），验证 key + 模型 + 端点三者一起能通。
func Ping(ctx context.Context, ep Endpoint, model string) PingResult {
	start := time.Now()
	var url string
	var body any
	switch ep.Protocol {
	case Responses:
		url = apiURL(ep.BaseURL, "responses")
		// input 必须是 item 数组，且 item 要带 type:"message"、content 用 input_text。
		// 实测（llm-gateway 0.3）：字符串 input、缺 type、content type:"text" 三种写法都 400。
		// 这是 Responses API 的规范形状，OpenAI 官方端点同样接受。
		body = map[string]any{
			"model": model,
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "ping"}},
			}},
			"max_output_tokens": 16,
			"stream":            false,
		}
	case Anthropic:
		url = apiURL(ep.BaseURL, "messages")
		body = map[string]any{"model": model, "max_tokens": 1, "messages": []map[string]string{{"role": "user", "content": "ping"}}}
	default:
		url = apiURL(ep.BaseURL, "chat/completions")
		body = map[string]any{"model": model, "max_tokens": 1, "messages": []map[string]string{{"role": "user", "content": "ping"}}}
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	setAuth(req, ep)
	setHeaders(req, ep)
	resp, err := httpClient.Do(req)
	if err != nil {
		return PingResult{Detail: connErr(err), Elapsed: time.Since(start)}
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	pr := PingResult{Status: resp.StatusCode, Elapsed: time.Since(start)}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		pr.OK = true
		pr.Detail = fmt.Sprintf("%s 通了（%dms）", string(ep.Protocol), pr.Elapsed.Milliseconds())
		return pr
	}
	pr.Detail, pr.BotBlocked = describeErr(resp, rb)
	return pr
}

func apiURL(base, path string) string {
	base = NormalizeBase(base)
	if strings.HasSuffix(base, "/v1") {
		return base + "/" + path
	}
	return base + "/v1/" + path
}

func setAuth(req *http.Request, ep Endpoint) {
	if ep.Key == "" {
		return
	}
	if ep.Protocol == Anthropic {
		req.Header.Set("x-api-key", ep.Key)
		req.Header.Set("anthropic-version", "2023-06-01")
		// 网关常常只认 Bearer；两个都带，谁认谁用。
		req.Header.Set("Authorization", "Bearer "+ep.Key)
		return
	}
	req.Header.Set("Authorization", "Bearer "+ep.Key)
}

// setHeaders 把 Endpoint.Headers 设上；放在 setAuth 之后，用户明确写的头优先。
func setHeaders(req *http.Request, ep Endpoint) {
	for k, v := range ep.Headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
}

// describeErr 把非 2xx 响应翻成人话，并顺手判断是不是 Cloudflare 的 bot 拦截。
func describeErr(resp *http.Response, body []byte) (string, bool) {
	if evidence := CloudflareBlock(resp.StatusCode, resp.Header, body); evidence != "" {
		return botErr(resp.StatusCode, evidence, body), true
	}
	return httpErr(resp.StatusCode, body), false
}

func connErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"):
		return "连接被拒绝——地址对吗？服务起了吗？"
	case strings.Contains(s, "no such host"):
		return "域名解析不了——拼错了，或者没网 / 需要代理"
	case strings.Contains(s, "Client.Timeout"), strings.Contains(s, "deadline exceeded"):
		return "连接超时——网络不通，或需要代理"
	case strings.Contains(s, "certificate"):
		return "TLS 证书有问题——自签证书或系统时间不对"
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// httpErr 把 HTTP 错误翻译成人话，并带上响应片段。
func httpErr(code int, body []byte) string {
	snippet := strings.Join(strings.Fields(string(body)), " ")
	if len(snippet) > 140 {
		snippet = snippet[:140] + "…"
	}
	var why string
	switch code {
	case 401:
		why = "401 认证失败——key 不对或过期"
	case 403:
		why = "403 被拒绝——key 没有这个权限 / 这个模型"
	case 404:
		why = "404 接口不存在——base_url 路径不对（少了或多了 /v1？），或网关不支持这个协议"
	case 402:
		why = "402 要付费——账户没余额或额度用完"
	case 429:
		why = "429 限流 / 额度用完"
	case 400, 422:
		why = fmt.Sprintf("%d 请求被拒——多半是模型名不对", code)
	case 502, 503, 504:
		why = fmt.Sprintf("%d 网关上游挂了", code)
	default:
		why = fmt.Sprintf("HTTP %d", code)
	}
	if snippet != "" {
		return why + "：" + snippet
	}
	return why
}

// cfStrongMarks 是只会出现在 Cloudflare 自己吐的拦截页 / 挑战页里的字样。
// 见到任何一个就能下结论，不必再看是不是 JSON。
var cfStrongMarks = []string{
	"cf-chl", "cf_chl", "__cf", "challenge-platform",
	"just a moment", "attention required", "checking your browser",
	"you have been blocked", "cf-error", "error code: 10",
}

// CloudflareBlock 判断一个 403/503 是不是 Cloudflare 在网关前面把请求当 bot 拦掉了。
// 返回证据（空串 = 不是）。
//
// 难点在于「网关自己的 403」和「Cloudflare 的 403」长得很像：网关挂在 Cloudflare 后面时，
// 它自己拒绝的响应也会带 Server: cloudflare / cf-ray。所以边缘头只是弱信号，
// 得配上「响应不是 JSON」（网关拒绝会给 JSON，Cloudflare 拦截页是 HTML / 纯文本）才算数；
// 拦截页 / 挑战页特有的字样则是强信号，单独就够。
func CloudflareBlock(status int, hdr http.Header, body []byte) string {
	if status != 403 && status != 503 {
		return ""
	}
	var ev []string
	if v := hdr.Get("Server"); strings.Contains(strings.ToLower(v), "cloudflare") {
		ev = append(ev, "server="+v)
	}
	for _, h := range []string{"cf-ray", "cf-cache-status", "cf-mitigated"} {
		if v := hdr.Get(h); v != "" {
			if h == "cf-mitigated" {
				ev = append(ev, h+"="+v)
			} else {
				ev = append(ev, h)
			}
		}
	}
	edge := len(ev) > 0
	strong := hdr.Get("cf-mitigated") != ""
	low := strings.ToLower(string(body))
	for _, m := range cfStrongMarks {
		if strings.Contains(low, m) {
			ev = append(ev, "body:"+m)
			strong = true
			break
		}
	}
	if !strong {
		for _, h := range []string{"Content-Type", "Set-Cookie", "Location"} {
			if v := strings.ToLower(hdr.Get(h)); strings.Contains(v, "cf-chl") || strings.Contains(v, "cf_chl") || strings.Contains(v, "__cf") {
				ev = append(ev, "header:"+h)
				strong = true
				break
			}
		}
	}
	if strong {
		return strings.Join(ev, ", ")
	}
	// 只有边缘头（或正文提了一嘴 cloudflare）：要求正文不是 JSON 才算拦截。
	if !edge && strings.Contains(low, "cloudflare") {
		ev = append(ev, "body:cloudflare")
		edge = true
	}
	if edge && !looksJSON(body) {
		return strings.Join(ev, ", ")
	}
	return ""
}

func looksJSON(body []byte) bool {
	t := bytes.TrimSpace(body)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

// botErr 是 Cloudflare 拦截时的人话。
func botErr(code int, evidence string, body []byte) string {
	snippet := strings.Join(strings.Fields(string(body)), " ")
	if len(snippet) > 80 {
		snippet = snippet[:80] + "…"
	}
	s := fmt.Sprintf("%d 疑似 Cloudflare bot 拦截（%s）——不是 key 的问题，是请求头太像机器", code, evidence)
	if snippet != "" && !strings.HasPrefix(snippet, "<") {
		s += "：" + snippet
	}
	return s
}
