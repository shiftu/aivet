package report

import "testing"

func TestExitCodeOnlyFailsOnFail(t *testing.T) {
	r := Report{Tools: []Tool{{Installed: true, Checks: []Check{{Status: Warn}, {Status: OK}}}}}
	if r.ExitCode() != 0 {
		t.Fatalf("warn 不应导致退出码 1")
	}
	r.Tools[0].Checks = append(r.Tools[0].Checks, Check{Status: Fail})
	if r.ExitCode() != 1 {
		t.Fatalf("fail 应导致退出码 1")
	}
}

func TestWorstAndCount(t *testing.T) {
	tl := Tool{Installed: true, Checks: []Check{{Status: OK}, {Status: Warn}, {Status: Skip}}}
	if tl.Worst() != Warn {
		t.Fatalf("worst = %s, want warn", tl.Worst())
	}
	c := tl.Count()
	if c.OK != 1 || c.Warn != 1 || c.Skip != 1 || c.Fail != 0 {
		t.Fatalf("count = %+v", c)
	}
	if (Tool{Installed: false}).Worst() != Skip {
		t.Fatalf("未安装的工具 worst 应为 skip")
	}
}

func TestFixableSkipsOK(t *testing.T) {
	r := Report{Tools: []Tool{{Installed: true, Checks: []Check{
		{ID: "a", Status: OK, FixID: "x"},
		{ID: "b", Status: Fail, FixID: "y"},
		{ID: "c", Status: Fail},
	}}}}
	f := r.Fixable()
	if len(f) != 1 || f[0].ID != "b" {
		t.Fatalf("fixable = %+v", f)
	}
}
