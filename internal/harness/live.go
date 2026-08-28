package harness

import (
	"os"
	"strings"
	"time"

	"github.com/shiftu/aivet/internal/probe"
)

// LiveTimeout 是「真跑一次」的上限；agent 冷启动 + 一次往返，两分钟够了。
const LiveTimeout = 120 * time.Second

// LivePrompt 让模型只回一个词，好判定。
const LivePrompt = "Reply with exactly: PONG"

// LiveRun 真的把工具跑一遍，看它能不能回 PONG。装上了 ≠ 跑得通。
// 在临时目录里跑：agent 可能会扫当前目录、建会话文件，别弄脏用户的工程。
func LiveRun(c *Context, b *Builder, label, bin string, args ...string) {
	if !c.Live {
		return
	}
	if c.Offline {
		b.Skip("live", "真实跑一次", "离线模式，跳过")
		return
	}
	dir, err := os.MkdirTemp("", "aivet-live-")
	if err != nil {
		dir = ""
	} else {
		defer os.RemoveAll(dir)
	}
	c.Say("run", label+" 正在真实跑一次（最多 2 分钟）…")
	out, runErr := probe.Run(c.Ctx, LiveTimeout, dir, bin, args...)
	if strings.Contains(strings.ToUpper(out), "PONG") {
		b.OK("live", "真实跑一次", label+" 回了 PONG")
		return
	}
	detail := probe.Tail(out, 4, 240)
	if runErr != nil && detail == "" {
		detail = runErr.Error()
	}
	if strings.Contains(runErr_(runErr), "deadline") || strings.Contains(runErr_(runErr), "killed") {
		detail = "超过 2 分钟没回话。" + detail
	}
	b.Fail("live", "真实跑一次", detail, "上面 HTTP 探测都绿但这里红，多半是工具自身的登录态 / 版本问题；把这段报错原样喂给 aivet ask")
}

func runErr_(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
