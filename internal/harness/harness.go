// Package harness 定义「一件 AI harness 工具」对 aivet 的契约。
//
// 每个工具包（claude / codex / hermes / pi / dsh / ccswitch）实现 Harness；
// 体检、修复、初始化三条流程都只跟这个接口打交道。
package harness

import (
	"context"
	"os"

	"github.com/shiftu/aivet/internal/knowledge"
	"github.com/shiftu/aivet/internal/platform"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

// Context 是一次体检/修复的运行环境。
type Context struct {
	Ctx      context.Context
	Home     string              // 用户主目录（AIVET_HOME 可覆盖）
	Live     bool                // 是否真的把每件工具跑一次
	Offline  bool                // 不打任何网络
	Env      func(string) string // 读环境变量（可注入，便于测试）
	Gateways *probe.GatewayCache
	Log      func(status, text string) // 进度输出（可为 nil）
	// Knowledge 是那些会过时的外部事实（提供方地址、配置文件位置、版本断言）。
	// 留空则首次用到时按 Home 现加载 —— 测试可以直接塞一份假的进来。
	Knowledge *knowledge.K
	// Postures 是这次跑过（或 Prime 补过）的工具的姿态，按工具 ID 存；cc-switch 拿来对照。
	Postures map[string]Posture
}

// K 返回生效的知识，必要时现加载。
func (c *Context) K() *knowledge.K {
	if c.Knowledge == nil {
		c.Knowledge = knowledge.Load(c.Home)
	}
	return c.Knowledge
}

// Path 按知识里的候选列表定位一个配置文件：返回第一个存在的，
// 都不存在时返回第一个候选（报「不存在」时该指向默认位置）。
//
// 走这里而不是直接 filepath.Join，是为了让「工具换了配置文件位置」这件事
// 可以由用户补一条 knowledge 解决，而不必等 aivet 发新版。
func (c *Context) Path(key string) string { return c.K().Path(key) }

// NewContext 用真实环境构造。
func NewContext(ctx context.Context) *Context {
	return &Context{Ctx: ctx, Home: probe.Home(), Env: os.Getenv, Gateways: probe.NewGatewayCache()}
}

// Say 打一条进度（Log 为 nil 时静默）。
func (c *Context) Say(status, text string) {
	if c.Log != nil {
		c.Log(status, text)
	}
}

// Detection 是「装没装」的结论。
type Detection struct {
	Installed bool
	Path      string
	Version   string
	// Broken 非空 = 可执行文件在 PATH 上，但跑不起来（装坏了）。
	Broken string
}

// Plan 是 setup 向导收集到的「一处来源」：所有工具都从它渲染配置。
type Plan struct {
	BaseURL string // OpenAI 兼容网关地址，带或不带 /v1 都行
	Key     string
	Model   string
	Force   bool // 覆盖已有配置（默认只补缺）
}

// Fixer 是一个可自动执行的修复。
type Fixer struct {
	ID    string
	Title string
	// Apply 返回改动了哪些文件；dry 为 true 时只描述不写。
	Apply func(c *Context, dry bool) ([]string, error)
}

// Harness 是一件工具。
type Harness interface {
	ID() string
	Label() string
	Detect(c *Context) Detection
	Check(c *Context, d Detection) []report.Check
	Fixers() []Fixer
}

// Configurer 是可由 setup 向导写配置的工具（cc-switch 这类不实现）。
type Configurer interface {
	// Configure 按 Plan 渲染原生配置。返回写了哪些文件、跳过了哪些（已存在且非 force）。
	Configure(c *Context, p Plan) (written, skipped []string, err error)
}

// Launcher 是能被 `aivet ask` 拉起来接手修复的 agent。
type Launcher interface {
	// LaunchArgs 返回交互式启动并带上首条提示词的命令行。
	LaunchArgs(prompt string) []string
}

// Run 对一件工具跑完整体检，产出 report.Tool。
func Run(c *Context, h Harness) report.Tool {
	d := h.Detect(c)
	t := report.Tool{ID: h.ID(), Label: h.Label(), Installed: d.Installed, Path: d.Path, Version: d.Version}
	if !d.Installed {
		t.Checks = []report.Check{{ID: h.ID() + ".installed", Tool: h.ID(), Title: "安装", Status: report.Skip, Detail: "没找到可执行文件，跳过"}}
		if _, ok := h.(PostureReporter); ok {
			c.SetPosture(h.ID(), Posture{})
		}
		return t
	}
	var pre []report.Check
	if d.Broken != "" {
		// 连 --version 都跑不出来，后面配置查得再绿也没意义 —— 这条必须排在最前面。
		// 配置项还是照查（对修复有用），但不再浪费两分钟去 --live 跑一个跑不起来的东西。
		pre = append(pre, report.Check{ID: h.ID() + ".runnable", Tool: h.ID(), Title: "可执行", Status: report.Fail,
			Detail: d.Broken,
			Hint:   "文件在 PATH 上却跑不起来 —— 多半是装坏了（npm 的平台专用包没装全，或升级中断）。重装：" + platform.Install(h.ID())})
		live := c.Live
		c.Live = false
		defer func() { c.Live = live }()
	}
	t.Checks = append(pre, h.Check(c, d)...)
	// 体检完顺手记姿态：配置由工具自己判断，Healthy 用这次的结论 —— 有一条 Fail 就不算能跑。
	if pr, ok := h.(PostureReporter); ok {
		p := pr.Posture(c)
		p.Installed = true
		healthy := t.Worst() != report.Fail
		p.Healthy = &healthy
		c.SetPosture(h.ID(), p)
	}
	return t
}

// FindFixer 在所有工具里找 fixer。
func FindFixer(all []Harness, id string) (Harness, Fixer, bool) {
	for _, h := range all {
		for _, f := range h.Fixers() {
			if f.ID == id {
				return h, f, true
			}
		}
	}
	return nil, Fixer{}, false
}
