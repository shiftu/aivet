package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shiftu/aivet/internal/ui"
)

// fakeHub 起一个假的 GitHub。sum 传空串就故意给一份对不上的校验和。
func fakeHub(t *testing.T, tag string, body string, badSum bool, omit string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	hexSum := hex.EncodeToString(sum[:])
	if badSum {
		hexSum = strings.Repeat("0", 64)
	}
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/repos/x/y/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		var assets []string
		if omit != "aivet_testos_testarch" {
			assets = append(assets, fmt.Sprintf(`{"name":"aivet_testos_testarch","browser_download_url":%q}`, base+"/dl/bin"))
		}
		if omit != "SHA256SUMS" {
			assets = append(assets, fmt.Sprintf(`{"name":"SHA256SUMS","browser_download_url":%q}`, base+"/dl/sums"))
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, tag, strings.Join(assets, ","))
	})
	mux.HandleFunc("/dl/bin", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  aivet_testos_testarch\n", hexSum)
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv.URL
}

// run 跑一次 update，返回退出码和它说了什么。target 是被换掉的那个「二进制」。
func run(t *testing.T, api, target string, args ...string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	pr := ui.Printer{W: &buf, P: ui.Plain(), Wid: 80}
	env := updateEnv{api: api, repo: "x/y", target: target, goos: "testos", goarch: "testarch"}
	return update(context.Background(), args, env, pr), buf.String()
}

func newTarget(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "aivet")
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUpdateReplacesBinary(t *testing.T) {
	api := fakeHub(t, "v9.9.9", "新的二进制", false, "")
	target := newTarget(t, "旧的")
	code, out := run(t, api, target)
	if code != 0 {
		t.Fatalf("退出码 %d：%s", code, out)
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "新的二进制" {
		t.Fatalf("没换成新的：%q %v", b, err)
	}
	if !strings.Contains(out, "v9.9.9") || !strings.Contains(out, "校验和对上了") {
		t.Errorf("没说清楚做了什么：%s", out)
	}
	// 临时文件不许留在目标旁边 —— 攒着迟早被人当成真东西。
	ents, _ := os.ReadDir(filepath.Dir(target))
	if len(ents) != 1 {
		t.Errorf("目录里剩下 %d 个文件，应该只有换好的那个", len(ents))
	}
}

// 这是整个命令最要紧的一条：校验和对不上就绝不安装。
// 少了它，任何能改写下载响应的人都能往用户机器上放一个会被执行的文件。
func TestUpdateRefusesToInstallOnChecksumMismatch(t *testing.T) {
	api := fakeHub(t, "v9.9.9", "被换过的东西", true, "")
	target := newTarget(t, "旧的")
	code, out := run(t, api, target)
	if code == 0 {
		t.Fatal("校验和对不上却装了")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "旧的" {
		t.Fatalf("原来的二进制被动过了：%q", b)
	}
	if !strings.Contains(out, "校验和对不上") {
		t.Errorf("没说是校验和的问题：%s", out)
	}
	ents, _ := os.ReadDir(filepath.Dir(target))
	if len(ents) != 1 {
		t.Errorf("下坏的那份没删掉，目录里有 %d 个文件", len(ents))
	}
}

// 没有 SHA256SUMS 就没法确认下到的是不是发布的那个。宁可不装，也不能「凑合装上」。
func TestUpdateRefusesWhenThereIsNothingToVerifyAgainst(t *testing.T) {
	api := fakeHub(t, "v9.9.9", "内容", false, "SHA256SUMS")
	target := newTarget(t, "旧的")
	code, out := run(t, api, target)
	if code == 0 {
		t.Fatal("没有校验和文件却装了")
	}
	if !strings.Contains(out, "SHA256SUMS") {
		t.Errorf("没说清缺的是什么：%s", out)
	}
	if b, _ := os.ReadFile(target); string(b) != "旧的" {
		t.Error("原来的二进制被动过了")
	}
}

// 这个版本没发你这个平台的包时，要说的是「没发」，不是「下载失败」。
func TestUpdateSaysWhenPlatformIsMissingFromRelease(t *testing.T) {
	api := fakeHub(t, "v9.9.9", "内容", false, "aivet_testos_testarch")
	target := newTarget(t, "旧的")
	code, out := run(t, api, target)
	if code == 0 {
		t.Fatal("没有对应平台的包却装了")
	}
	if !strings.Contains(out, "aivet_testos_testarch") {
		t.Errorf("没说缺的是哪个包：%s", out)
	}
}

// --check 是「只看」。它要是动了文件，名字就是骗人的。
func TestUpdateCheckTouchesNothing(t *testing.T) {
	api := fakeHub(t, "v9.9.9", "新的", false, "")
	target := newTarget(t, "旧的")
	code, out := run(t, api, target, "--check")
	if code != 0 {
		t.Fatalf("--check 退出码 %d：%s", code, out)
	}
	if b, _ := os.ReadFile(target); string(b) != "旧的" {
		t.Error("--check 却把文件换了")
	}
	if !strings.Contains(out, "v9.9.9") {
		t.Errorf("--check 没报出新版本：%s", out)
	}
}

// 已经是最新时不该白下一遍；--force 时又必须真的下。
func TestUpdateSkipsWhenCurrentIsNotOlder(t *testing.T) {
	api := fakeHub(t, "v0.0.1", "远端的", false, "")
	target := newTarget(t, "旧的")

	old := version
	version = "v9.9.9"
	defer func() { version = old }()

	code, out := run(t, api, target)
	if code != 0 || !strings.Contains(out, "已经是最新") {
		t.Errorf("比远端还新却没打住：%d %s", code, out)
	}
	if b, _ := os.ReadFile(target); string(b) != "旧的" {
		t.Error("什么都不该做，文件却被换了")
	}

	if code, out = run(t, api, target, "--force"); code != 0 {
		t.Fatalf("--force 退出码 %d：%s", code, out)
	}
	if b, _ := os.ReadFile(target); string(b) != "远端的" {
		t.Errorf("--force 没真的重装：%q", b)
	}
}

// 版本号读不出来（自己编的 dev 构建）时不能默认「已经最新」——
// 那等于把一次明确的更新请求悄悄吃掉。
func TestUpdateDoesNotSilentlySkipOnUnparseableVersion(t *testing.T) {
	api := fakeHub(t, "v9.9.9", "新的", false, "")
	target := newTarget(t, "旧的")

	old := version
	version = "dev"
	defer func() { version = old }()

	code, out := run(t, api, target)
	if code != 0 {
		t.Fatalf("退出码 %d：%s", code, out)
	}
	if b, _ := os.ReadFile(target); string(b) != "新的" {
		t.Errorf("dev 构建下没装成：%q", b)
	}
	if !strings.Contains(out, "读不出来") {
		t.Errorf("该明说比不了版本号：%s", out)
	}
}
