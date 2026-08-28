// Package setup 是给新手的「一处来源」向导：
// 问三样东西（网关地址、key、模型），先验证能通，再把每件工具的原生配置一次写好。
//
// 默认**不覆盖**已有配置——老手手改过的东西不能被向导冲掉；要重来加 --force。
package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/ui"
)

// Options 来自命令行；空字段进入交互式提问。
type Options struct {
	BaseURL string
	Key     string
	Model   string
	Tools   []string // 空 = 所有已安装且可配置的
	Force   bool
	Yes     bool // 非交互：缺什么直接报错，不提问
}

// Run 执行向导。
func Run(c *harness.Context, all []harness.Harness, pr ui.Printer, in io.Reader, opt Options) error {
	rd := bufio.NewReader(in)
	ask := func(label, def string, secret bool) (string, error) {
		if opt.Yes {
			if def == "" {
				return "", fmt.Errorf("--yes 模式下缺少 %s", label)
			}
			return def, nil
		}
		prompt := label
		if def != "" {
			prompt += pr.P.Dim(" [" + def + "]")
		}
		fmt.Fprintf(pr.W, "  %s %s ", pr.P.Glyph("info"), prompt)
		var line string
		if secret && term.IsTerminal(int(os.Stdin.Fd())) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(pr.W)
			if err != nil {
				return "", err
			}
			line = string(b)
		} else {
			l, err := rd.ReadString('\n')
			if err != nil && l == "" {
				return "", err
			}
			line = l
		}
		line = strings.TrimSpace(line)
		if line == "" {
			line = def
		}
		return line, nil
	}

	pr.Section("① 网关")
	fmt.Fprintln(pr.W, pr.P.Dim("  一个 OpenAI 兼容的地址，例如 http://127.0.0.1:7421/v1 或 https://api.deepseek.com/v1"))
	base, err := ask("网关地址：", opt.BaseURL, false)
	if err != nil {
		return err
	}
	base = probe.NormalizeBase(base)
	if base == "" || !strings.HasPrefix(base, "http") {
		return fmt.Errorf("网关地址要以 http:// 或 https:// 开头")
	}
	key, err := ask("API key（输入不回显）：", opt.Key, true)
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("key 不能为空")
	}

	pr.Section("② 验证网关")
	ids, res := c.Gateways.Models(c.Ctx, probe.Endpoint{BaseURL: base, Key: key})
	switch {
	case res.OK:
		pr.Line("ok", fmt.Sprintf("网关可达，%s", res.Detail))
	case res.Status == 401 || res.Status == 403:
		return fmt.Errorf("网关拒绝了这把 key：%s", res.Detail)
	case res.Status == 0:
		return fmt.Errorf("连不上网关：%s", res.Detail)
	default:
		pr.Line("warn", "网关没有模型清单接口（"+res.Detail+"），跳过清单核对")
	}

	pr.Section("③ 模型")
	def := opt.Model
	if len(ids) > 0 {
		show := ids
		if len(show) > 12 {
			show = show[:12]
		}
		for i, id := range show {
			fmt.Fprintf(pr.W, "     %s %s\n", pr.P.Dim(fmt.Sprintf("%2d.", i+1)), id)
		}
		if len(ids) > 12 {
			fmt.Fprintf(pr.W, "     %s\n", pr.P.Dim(fmt.Sprintf("… 共 %d 个", len(ids))))
		}
		if def == "" {
			def = ids[0]
		}
	}
	model, err := ask("用哪个模型（可填序号或名字）：", def, false)
	if err != nil {
		return err
	}
	if n := parseIndex(model); n > 0 && n <= len(ids) {
		model = ids[n-1]
	}
	if len(ids) > 0 && !probe.HasModel(ids, model) {
		return fmt.Errorf("清单里没有 %q；照上面列表填", model)
	}
	ping := c.Gateways.Ping(c.Ctx, probe.Endpoint{BaseURL: base, Key: key, Protocol: probe.ChatCompletions}, model)
	if !ping.OK {
		return fmt.Errorf("用 %s 发一条请求失败：%s", model, ping.Detail)
	}
	pr.Line("ok", ping.Detail)

	plan := harness.Plan{BaseURL: base, Key: key, Model: model, Force: opt.Force}
	pr.Section("④ 写配置")
	wantTool := map[string]bool{}
	for _, t := range opt.Tools {
		wantTool[strings.ToLower(strings.TrimSpace(t))] = true
	}
	any := false
	for _, h := range all {
		cfg, ok := h.(harness.Configurer)
		if !ok {
			continue
		}
		if len(wantTool) > 0 && !wantTool[h.ID()] {
			continue
		}
		if !h.Detect(c).Installed && !wantTool[h.ID()] {
			pr.Line("skip", h.Label()+"：未安装，跳过")
			continue
		}
		any = true
		written, skipped, err := cfg.Configure(c, plan)
		if err != nil {
			pr.Line("fail", h.Label()+"："+err.Error())
			continue
		}
		for _, s := range skipped {
			pr.Line("warn", h.Label()+"：已有配置，未覆盖 → "+s+pr.P.Dim("（要重写加 --force）"))
		}
		for _, w := range written {
			pr.Line("ok", h.Label()+"：已写 "+w)
		}
	}
	if !any {
		pr.Line("warn", "没有一件可配置的工具是已安装的。装几件再来：aivet env")
	}
	fmt.Fprintln(pr.W)
	fmt.Fprintln(pr.W, pr.P.Dim("  原文件都留了 .aivet-bak-<时间> 备份。接下来跑 aivet check 验证。"))
	return nil
}

func parseIndex(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
