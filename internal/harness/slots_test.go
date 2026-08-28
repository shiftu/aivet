package harness_test

// 副槽核对盯的是一类沉默失效：配置里明明还写着别的模型（codex 的 review_model、
// pi 的 enabledModels、各家声明的菜单），aivet 只验主模型就说「全部通过」。
// 用户切过去才 404 —— 报告当时是绿的，而且没人会回来问。

import (
	"context"
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/harness"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/report"
)

func one(t *testing.T, checks []report.Check) report.Check {
	t.Helper()
	if len(checks) != 1 {
		t.Fatalf("要一条聚合结论，实际 %d 条：%+v", len(checks), checks)
	}
	return checks[0]
}

func run(t *testing.T, c *harness.Context, ep probe.Endpoint, primary string, slots ...harness.ModelSlot) []report.Check {
	t.Helper()
	b := harness.NewBuilder("codex")
	harness.CheckOtherModels(c, b, ep, primary, slots)
	return b.Checks()
}

func onlineCtx(t *testing.T, ids ...string) (*harness.Context, probe.Endpoint) {
	t.Helper()
	srv := modelsServer(t, ids...)
	return &harness.Context{Ctx: context.Background(), Env: func(string) string { return "" },
			Gateways: probe.NewGatewayCache()},
		probe.Endpoint{BaseURL: srv + "/v1", Key: "k", Protocol: probe.ChatCompletions}
}

func TestOtherModelsStaysQuietWhenThereIsNothingToSay(t *testing.T) {
	c, ep := onlineCtx(t, "a", "b")
	// 没有副槽 —— 不该凭空多一条检查项。
	if got := run(t, c, ep, "a"); len(got) != 0 {
		t.Fatalf("没有副槽时应当沉默，得到 %+v", got)
	}
	// 副槽就是主模型本身 —— 已经单独验过了，不重复报。
	if got := run(t, c, ep, "a", harness.Slot("review_model", "a")); len(got) != 0 {
		t.Fatalf("副槽等于主模型时应当沉默，得到 %+v", got)
	}
	// 空值不算槽。
	if got := run(t, c, ep, "a", harness.Slot("review_model", "")); len(got) != 0 {
		t.Fatalf("空模型名不该产生检查项，得到 %+v", got)
	}
}

func TestOtherModelsWarnsButNeverFails(t *testing.T) {
	c, ep := onlineCtx(t, "a", "b")
	got := one(t, run(t, c, ep, "a",
		harness.Slot("review_model", "ghost"),
		harness.Slot("已启用", "b")))
	// 副槽对不上是 Warn 不是 Fail：用户未必用得上它，不该把整台机器判成故障。
	if got.Status != report.Warn {
		t.Fatalf("要 Warn，得到 %s：%s", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "ghost（review_model）") {
		t.Fatalf("要说清哪个模型、从哪来的：%q", got.Detail)
	}
	if strings.Contains(got.Detail, "\"b\"") || strings.Contains(got.Detail, "没有：b") {
		t.Fatalf("在清单里的不该被报出来：%q", got.Detail)
	}
}

func TestOtherModelsAllPresent(t *testing.T) {
	c, ep := onlineCtx(t, "a", "b", "c")
	got := one(t, run(t, c, ep, "a", harness.Slot("已启用", "b"), harness.Slot("已启用", "c")))
	if got.Status != report.OK || !strings.Contains(got.Detail, "2 个都在清单里") {
		t.Fatalf("%s %q", got.Status, got.Detail)
	}
}

// 同一个模型被多处引用时，来源要合起来说 —— 否则用户不知道该改哪个字段。
func TestOtherModelsMergesSources(t *testing.T) {
	c, ep := onlineCtx(t, "a")
	got := one(t, run(t, c, ep, "a",
		harness.Slot("review_model", "ghost"), harness.Slot("sonnet 别名", "ghost")))
	if !strings.Contains(got.Detail, "review_model / sonnet 别名") {
		t.Fatalf("来源没合并：%q", got.Detail)
	}
	if strings.Count(got.Detail, "ghost") != 1 {
		t.Fatalf("同一个模型报了不止一次：%q", got.Detail)
	}
}

// 探不到清单的时候必须说「没核对」，不能说「没问题」——
// 这正是 v0.1.3 那条规矩：不给查不到的东西打包票。
func TestOtherModelsRefusesToVouchWhenItCannotCheck(t *testing.T) {
	c, ep := onlineCtx(t, "a")
	for _, tc := range []struct {
		name string
		mut  func(*harness.Context, *probe.Endpoint)
	}{
		{"离线", func(c *harness.Context, _ *probe.Endpoint) { c.Offline = true }},
		{"没有 key", func(_ *harness.Context, e *probe.Endpoint) { e.Key = "" }},
		{"没有地址", func(_ *harness.Context, e *probe.Endpoint) { e.BaseURL = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cc := *c
			cc.Gateways = probe.NewGatewayCache()
			e := ep
			tc.mut(&cc, &e)
			got := one(t, run(t, &cc, e, "a", harness.Slot("review_model", "ghost")))
			if got.Status != report.Skip {
				t.Fatalf("查不了就该报 Skip，得到 %s：%s", got.Status, got.Detail)
			}
			if !strings.Contains(got.Detail, "ghost") {
				t.Fatalf("至少要说出有哪些没核对：%q", got.Detail)
			}
		})
	}
}

// 网关活着但没有清单接口：同样不能下结论。
func TestOtherModelsSkipsWhenGatewayHasNoCatalog(t *testing.T) {
	c := &harness.Context{Ctx: context.Background(), Env: func(string) string { return "" }, Gateways: probe.NewGatewayCache()}
	ep := probe.Endpoint{BaseURL: noCatalogServer(t) + "/v1", Key: "k", Protocol: probe.ChatCompletions}
	got := one(t, run(t, c, ep, "a", harness.Slot("review_model", "ghost")))
	if got.Status != report.Skip || !strings.Contains(got.Detail, "核对不了") {
		t.Fatalf("%s %q", got.Status, got.Detail)
	}
}
