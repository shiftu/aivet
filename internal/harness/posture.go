package harness

// Posture 是一件工具「现在靠什么在跑」的摘要，给 cc-switch 这类切换器对照用。
//
// 政策：原生配置优先，官方登录为正；cc-switch 是 fallback。所以切换器那一节要回答的
// 不是「同步没同步」，而是「谁在管、原生这份能不能跑」。后一个问题只有跑过那件工具
// 自己的体检才知道 —— 切换器不该再读一遍原生文件去猜。
type Posture struct {
	Installed bool
	Official  bool   // 官方登录 / 官方端点，没有自定义网关
	BaseURL   string // 自定义网关地址；官方或没配置时为空
	// Healthy 是本次体检的结论：nil = 没探（只读了配置，没发请求）。
	// 没探就说没探 —— 不能因为配置长得对就当它能跑。
	Healthy *bool
}

// Unconfigured 既不是官方也没网关。
func (p Posture) Unconfigured() bool { return !p.Official && p.BaseURL == "" }

// Broken 是「探过了，不通」；没探过不算坏。
func (p Posture) Broken() bool { return p.Healthy != nil && !*p.Healthy }

// PostureReporter 是能报姿态的工具。只读配置，不发请求。
type PostureReporter interface {
	Posture(c *Context) Posture
}

// Correlator 是要对照别的工具姿态的工具（cc-switch）。
type Correlator interface {
	Correlates() []string
}

// SetPosture 记下一件工具的姿态。
func (c *Context) SetPosture(id string, p Posture) {
	if c.Postures == nil {
		c.Postures = map[string]Posture{}
	}
	c.Postures[id] = p
}

// PostureOf 取一件工具的姿态；这次没跑过它就返回 false。
func (c *Context) PostureOf(id string) (Posture, bool) {
	p, ok := c.Postures[id]
	return p, ok
}

// Prime 给 running 里的 Correlator 补齐它要对照的工具的姿态。
//
// 用户只 `check ccswitch` 时 claude/codex/hermes 不会跑，cc-switch 那节就没东西可对照。
// 这里只读它们的配置补一份（不发请求，Healthy 留空），让 cc-switch 至少知道「谁在管」。
func Prime(c *Context, all, running []Harness) {
	isRunning := map[string]bool{}
	for _, h := range running {
		isRunning[h.ID()] = true
	}
	var wanted []string
	for _, h := range running {
		if cr, ok := h.(Correlator); ok {
			wanted = append(wanted, cr.Correlates()...)
		}
	}
	for _, id := range wanted {
		if isRunning[id] {
			continue
		}
		for _, h := range all {
			pr, ok := h.(PostureReporter)
			if !ok || h.ID() != id {
				continue
			}
			d := h.Detect(c)
			p := Posture{Installed: d.Installed}
			if d.Installed {
				p = pr.Posture(c)
				p.Installed = true
			}
			c.SetPosture(id, p)
		}
	}
}
