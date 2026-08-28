// aivet：给套着缰绳的 AI 看病。
//
// 命令的权威说明在 internal/cli（一处来源：人看的帮助、agent 看的 JSON、
// 还有下面的参数解析都对着它）。加命令时那边和这里的 switch 要一起改，
// 有测试盯着两边不许对不上。
//
//	aivet help        命令总览
//	aivet help <命令>  详细用法
//	aivet help --json  给 agent 的完整规格
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/shiftu/aivet/internal/agent"
	"github.com/shiftu/aivet/internal/cli"
	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/harness/ccswitch"
	"github.com/shiftu/aivet/internal/harness/claude"
	"github.com/shiftu/aivet/internal/harness/codex"
	"github.com/shiftu/aivet/internal/harness/dsh"
	"github.com/shiftu/aivet/internal/harness/hermes"
	"github.com/shiftu/aivet/internal/harness/pi"
	"github.com/shiftu/aivet/internal/platform"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
	"github.com/shiftu/aivet/internal/setup"
	"github.com/shiftu/aivet/internal/skill"
	"github.com/shiftu/aivet/internal/ui"
)

// version 由构建时 -ldflags 注入。
var version = "dev"

// registry 是体检顺序：先 agent，最后切换器。
func registry() []harness.Harness {
	return []harness.Harness{claude.H{}, codex.H{}, hermes.H{}, pi.H{}, dsh.H{}, ccswitch.H{}}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	args := os.Args[1:]
	cmd := "check"
	switch {
	case len(args) > 0 && !strings.HasPrefix(args[0], "-"):
		cmd, args = args[0], args[1:]
	case len(args) > 0 && isHelpFlag(args[0]):
		// `aivet --help` 以前会掉进 check 的 flag 解析，吐一坨 Go 默认的选项表。
		cmd, args = "help", args[1:]
	case len(args) > 0 && (args[0] == "-v" || args[0] == "--version"):
		cmd, args = "version", args[1:]
	}
	// 任何子命令加 --help 都走帮助，而不是让 flag 包打印它那套。
	if cmd != "help" && hasHelpFlag(args) {
		code := runHelp([]string{cmd})
		os.Exit(code)
	}
	var code int
	switch cmd {
	case "check", "doctor":
		code = runCheck(ctx, args)
	case "fix":
		code = runFix(ctx, args)
	case "setup", "init":
		code = runSetup(ctx, args)
	case "ask":
		code = runAsk(ctx, args)
	case "skill":
		code = runSkill(args)
	case "env":
		code = runEnv(ctx)
	case "version":
		fmt.Printf("aivet %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	case "help":
		code = runHelp(args)
	default:
		fmt.Fprintf(os.Stderr, "不认识的命令 %q。", cmd)
		if s := cli.Suggest(commands(), cmd); s != "" {
			fmt.Fprintf(os.Stderr, "你是想写 %s 吗？\n", s)
		} else {
			fmt.Fprintf(os.Stderr, "看看有哪些命令：aivet help\n")
		}
		code = 2
	}
	os.Exit(code)
}

// isHelpFlag 判断一个参数是不是求助。
func isHelpFlag(a string) bool { return a == "-h" || a == "--help" || a == "-help" }

// hasHelpFlag 判断参数里有没有求助标志。
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if isHelpFlag(a) {
			return true
		}
	}
	return false
}

// commands 是命令说明书；fix 那一页要列出当前真实可用的修复项，
// 所以从各工具的 Fixers() 现拿，不写死。
func commands() []cli.Command {
	var ids []string
	for _, h := range registry() {
		for _, f := range h.Fixers() {
			ids = append(ids, f.ID)
		}
	}
	return cli.Commands(ids)
}

func runHelp(args []string) int {
	asJSON := false
	var want string
	for _, a := range args {
		switch {
		case a == "--json" || a == "-json":
			asJSON = true
		case isHelpFlag(a):
		case !strings.HasPrefix(a, "-") && want == "":
			want = a
		}
	}
	cmds := commands()
	r := cli.Renderer{W: os.Stdout, P: ui.New(), Wid: ui.Width(), Version: version}
	if asJSON {
		r.P = ui.Plain()
		if err := r.JSON(cmds); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if want == "" {
		r.Overview(cmds)
		return 0
	}
	c, ok := cli.Lookup(cmds, want)
	if !ok {
		fmt.Fprintf(os.Stderr, "没有 %q 这个命令。", want)
		if s := cli.Suggest(cmds, want); s != "" {
			fmt.Fprintf(os.Stderr, "你是想看 %s 吗？\n", s)
		} else {
			fmt.Fprintln(os.Stderr, "看看有哪些命令：aivet help")
		}
		return 2
	}
	r.Detail(c)
	return 0
}

// newFlagSet 让解析失败时提示 aivet help <命令>，而不是甩一坨 Go 默认的选项表 ——
// 那套输出不解释任何东西，还会把命令名印成 "Usage of check:"。
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parseErr 把解析失败统一成一句人话 + 一条出路。
func parseErr(name string, err error) int {
	fmt.Fprintf(os.Stderr, "aivet %s: %v\n用法看这里：aivet help %s\n", name, err, name)
	return 2
}

func newPrinter(json bool) ui.Printer {
	if json {
		return ui.Printer{W: os.Stderr, P: ui.Plain(), Wid: 80}
	}
	return ui.Printer{W: os.Stdout, P: ui.New(), Wid: ui.Width()}
}

func selectTools(all []harness.Harness, names []string) ([]harness.Harness, error) {
	if len(names) == 0 {
		return all, nil
	}
	var out []harness.Harness
	for _, n := range names {
		n = strings.ToLower(n)
		if n == "cc-switch" {
			n = "ccswitch"
		}
		found := false
		for _, h := range all {
			if h.ID() == n {
				out, found = append(out, h), true
			}
		}
		if !found {
			return nil, fmt.Errorf("不认识的工具 %q；可选：claude codex hermes pi dsh ccswitch", n)
		}
	}
	return out, nil
}

func runReport(ctx context.Context, hs []harness.Harness, live, offline bool, pr ui.Printer) report.Report {
	c := harness.NewContext(ctx)
	c.Live, c.Offline = live, offline
	c.Log = pr.Line
	r := report.Report{AivetVersion: version, OS: runtime.GOOS, Arch: runtime.GOARCH, Time: time.Now(), Live: live}
	for _, h := range hs {
		r.Tools = append(r.Tools, harness.Run(c, h))
	}
	return r
}

func runCheck(ctx context.Context, args []string) int {
	fs := newFlagSet("check")
	asJSON := fs.Bool("json", false, "输出 JSON（给 agent / 脚本）")
	live := fs.Bool("live", false, "真的把每件工具跑一次（慢）")
	offline := fs.Bool("offline", false, "不打网络")
	if err := fs.Parse(reorder(args)); err != nil {
		return parseErr("check", err)
	}
	hs, err := selectTools(registry(), fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	pr := newPrinter(*asJSON)
	if !*asJSON {
		pr.Banner(version, platform.Label(), *live)
	}
	r := runReport(ctx, hs, *live, *offline, pr)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
		return r.ExitCode()
	}
	for _, t := range r.Tools {
		pr.Tool(t)
	}
	pr.Summary(r)
	if _, err := agent.SaveReport(probe.Home(), r); err != nil {
		fmt.Fprintln(os.Stderr, "保存报告失败：", err)
	}
	return r.ExitCode()
}

func runFix(ctx context.Context, args []string) int {
	fs := newFlagSet("fix")
	yes := fs.Bool("yes", false, "不逐条确认")
	dry := fs.Bool("dry-run", false, "只说要改什么，不写")
	if err := fs.Parse(reorder(args)); err != nil {
		return parseErr("fix", err)
	}
	pr := newPrinter(false)
	all := registry()
	ids := fs.Args()
	if len(ids) == 0 {
		pr.Section("先体检，找可自动修复的项…")
		r := runReport(ctx, all, false, false, pr)
		for _, c := range r.Fixable() {
			ids = append(ids, c.FixID)
		}
		if len(ids) == 0 {
			pr.Line("ok", "没有可自动修复的项。")
			if r.Count().Fail > 0 {
				pr.Line("info", "但有故障——看 aivet check 的提示，或 aivet ask 交给 agent。")
				return 1
			}
			return 0
		}
	}
	pr.Section("修复")
	failed := 0
	for _, id := range uniq(ids) {
		h, f, ok := harness.FindFixer(all, id)
		if !ok {
			pr.Line("fail", "没有这个修复项："+id)
			failed++
			continue
		}
		label := fmt.Sprintf("%s · %s", h.Label(), f.Title)
		if !*yes && !*dry && !confirm(pr, label) {
			pr.Line("skip", "跳过 "+id)
			continue
		}
		c := harness.NewContext(ctx)
		changed, err := f.Apply(c, *dry)
		if err != nil {
			pr.Line("fail", label+"："+err.Error())
			failed++
			continue
		}
		verb := "已改"
		if *dry {
			verb = "将改"
		}
		pr.Line("fix", fmt.Sprintf("%s → %s %s", label, verb, strings.Join(changed, ", ")))
	}
	if failed > 0 {
		return 1
	}
	if !*dry {
		fmt.Fprintln(pr.W, pr.P.Dim("\n  备份在同目录的 *.aivet-bak-<时间>。复验：aivet check"))
	}
	return 0
}

func confirm(pr ui.Printer, what string) bool {
	fmt.Fprintf(pr.W, " %s %s  %s ", pr.P.Glyph("fix"), what, pr.P.Dim("[y/N]"))
	var s string
	fmt.Scanln(&s)
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes"
}

func runSetup(ctx context.Context, args []string) int {
	fs := newFlagSet("setup")
	var o setup.Options
	var tools string
	fs.StringVar(&o.BaseURL, "gateway", "", "网关地址")
	fs.StringVar(&o.Key, "key", os.Getenv("AIVET_KEY"), "API key（也可用环境变量 AIVET_KEY）")
	fs.StringVar(&o.Model, "model", "", "模型名")
	fs.StringVar(&tools, "tools", "", "只配这些工具，逗号分隔")
	fs.BoolVar(&o.Force, "force", false, "覆盖已有配置")
	fs.BoolVar(&o.Yes, "yes", false, "非交互")
	if err := fs.Parse(reorder(args)); err != nil {
		return parseErr("setup", err)
	}
	if tools != "" {
		o.Tools = strings.Split(tools, ",")
	}
	pr := newPrinter(false)
	pr.Banner(version, platform.Label(), false)
	fmt.Fprintln(pr.W, pr.P.Bold("\n新手向导：")+" 问三样东西，验证能通，然后把所有工具的配置一次写好。")
	c := harness.NewContext(ctx)
	if err := setup.Run(c, registry(), pr, os.Stdin, o); err != nil {
		pr.Line("fail", err.Error())
		return 1
	}
	return 0
}

func runAsk(ctx context.Context, args []string) int {
	fs := newFlagSet("ask")
	with := fs.String("with", "", "指定接手的 agent")
	printOnly := fs.Bool("print", false, "只打印提示词，不启动")
	if err := fs.Parse(reorder(args)); err != nil {
		return parseErr("ask", err)
	}
	pr := newPrinter(false)
	pr.Banner(version, platform.Label(), false)
	all := registry()
	r := runReport(ctx, all, false, false, pr)
	for _, t := range r.Tools {
		pr.Tool(t)
	}
	pr.Summary(r)
	if len(r.Problems()) == 0 {
		pr.Line("ok", "没有需要交接的问题。")
		return 0
	}
	path, err := agent.SaveReport(probe.Home(), r)
	if err != nil {
		pr.Line("fail", "保存报告失败："+err.Error())
		return 1
	}
	prompt := agent.Prompt(r, path)
	if *printOnly {
		fmt.Println(prompt)
		return 0
	}
	h, err := agent.Pick(all, r, *with)
	if err != nil {
		// 没人能接手时也要把提示词交到用户手上 —— 他手边可能有网页版、
		// 或者另一台机器上的 agent。让他重跑一次 --print 是白费一轮体检。
		pr.Line("fail", err.Error())
		fmt.Fprintln(pr.W, pr.P.Dim("\n  下面这段可以直接粘给任何一个 agent（网页版也行）：\n"))
		fmt.Fprintln(pr.W, prompt)
		return 1
	}
	pr.Line("run", "交给 "+h.Label()+" 接手，报告在 "+path)
	fmt.Fprintln(pr.W)
	if err := agent.Launch(h, prompt); err != nil {
		pr.Line("fail", "启动失败："+err.Error())
		return 1
	}
	return 0
}

func runSkill(args []string) int {
	fs := newFlagSet("skill")
	forTools := fs.String("for", "", "装给哪些 agent，逗号分隔（默认：已安装的）")
	if err := fs.Parse(reorder(args)); err != nil {
		return parseErr("skill", err)
	}
	pr := newPrinter(false)
	sub := "install"
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}
	switch sub {
	case "show", "print":
		fmt.Print(skill.Content)
		return 0
	case "install":
	default:
		fmt.Fprintln(os.Stderr, "用法：aivet skill install [--for claude,codex,hermes,pi] | aivet skill show")
		return 2
	}
	var targets []string
	if *forTools != "" {
		targets = strings.Split(*forTools, ",")
	} else {
		c := harness.NewContext(context.Background())
		for _, h := range registry() {
			if _, ok := skill.Targets[h.ID()]; ok && h.Detect(c).Installed {
				targets = append(targets, h.ID())
			}
		}
	}
	if len(targets) == 0 {
		pr.Line("warn", "没有发现可装技能的 agent（claude/codex/hermes/pi）")
		return 1
	}
	for _, t := range targets {
		p, err := skill.Install(probe.Home(), strings.TrimSpace(t))
		if err != nil {
			pr.Line("fail", t+"："+err.Error())
			continue
		}
		pr.Line("ok", t+" → "+p)
	}
	fmt.Fprintln(pr.W, pr.P.Dim("\n  之后在 agent 里说「用 aivet 检查一下我的 AI 环境」即可。"))
	return 0
}

func runEnv(ctx context.Context) int {
	pr := newPrinter(false)
	pr.Banner(version, platform.Label(), false)
	pr.Section("系统")
	pr.Line("info", "HOME = "+probe.Home())
	for _, b := range []string{"node", "npm", "python3", "git", "curl", "sqlite3"} {
		if p, ok := probe.Which(b); ok {
			pr.Line("ok", fmt.Sprintf("%-8s %s", b, p))
		} else {
			pr.Line("skip", fmt.Sprintf("%-8s 未找到", b))
		}
	}
	pr.Section("工具")
	c := harness.NewContext(ctx)
	for _, h := range registry() {
		d := h.Detect(c)
		if d.Installed {
			pr.Line("ok", fmt.Sprintf("%-24s %s  %s", h.Label(), pr.P.Dim(d.Version), pr.P.Dim(d.Path)))
		} else {
			pr.Line("skip", fmt.Sprintf("%-24s 未安装 → %s", h.Label(), platform.Install(h.ID())))
		}
	}
	if _, ok := probe.Which("node"); !ok {
		pr.Section("提示")
		pr.Line("info", "codex / pi / dsh 都是 Node 程序。"+platform.NodeHint())
	}
	fmt.Fprintln(pr.W)
	return 0
}

// reorder 把 flag 挪到位置参数前面，让 `aivet check codex --live` 和 `aivet check --live codex` 都行。
func reorder(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// 带值的 flag（--gateway URL）把下一个也带上
			if !strings.Contains(a, "=") && i+1 < len(args) && needsValue(a) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

func needsValue(flagName string) bool {
	switch strings.TrimLeft(flagName, "-") {
	case "gateway", "key", "model", "tools", "with", "for":
		return true
	}
	return false
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
