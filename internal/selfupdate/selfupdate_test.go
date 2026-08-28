package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// 资产名是构建产物名，由 Makefile 的 TARGETS 决定。这两处对不上的表现是 404 ——
// 用户看到的是「更新失败」，而不是「名字写错了」，所以只能靠测试盯着。
func TestAssetNamesMatchMakefileTargets(t *testing.T) {
	b, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`TARGETS := (.+)`).FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("Makefile 里找不到 TARGETS")
	}
	for _, target := range strings.Fields(m[1]) {
		goos, goarch, ok := strings.Cut(target, "/")
		if !ok {
			t.Fatalf("TARGETS 里的 %q 不是 os/arch", target)
		}
		want := "aivet_" + goos + "_" + goarch
		if goos == "windows" {
			want += ".exe"
		}
		if got := AssetName(goos, goarch); got != want {
			t.Errorf("AssetName(%s, %s) = %q，Makefile 产出的是 %q", goos, goarch, got, want)
		}
	}
	// 反过来也得成立：当前这台机器要能在 TARGETS 里找到自己。
	if !strings.Contains(m[1], runtime.GOOS+"/"+runtime.GOARCH) {
		t.Logf("注意：%s/%s 不在 TARGETS 里，这台机器上 update 会 404", runtime.GOOS, runtime.GOARCH)
	}
}

func TestParseSums(t *testing.T) {
	got := ParseSums("abc123  aivet_darwin_arm64\ndef456 *aivet_windows_amd64.exe\n\n垃圾行\n")
	if got["aivet_darwin_arm64"] != "abc123" {
		t.Errorf("普通行没解析出来：%v", got)
	}
	if got["aivet_windows_amd64.exe"] != "def456" {
		t.Errorf("二进制模式那个 * 没去掉：%v", got)
	}
	if len(got) != 2 {
		t.Errorf("把垃圾行也算进去了：%v", got)
	}
}

// 起一个假的 GitHub：/releases/latest、/releases/tags/x 和几个下载地址。
func fakeGitHub(t *testing.T, body []byte) (*httptest.Server, string) {
	t.Helper()
	sum := sha256.Sum256(body)
	mux := http.NewServeMux()
	var base string
	rel := func(w http.ResponseWriter, tag string) {
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[
			{"name":"aivet_darwin_arm64","browser_download_url":%q},
			{"name":"SHA256SUMS","browser_download_url":%q}]}`,
			tag, base+"/dl/bin", base+"/dl/sums")
	}
	mux.HandleFunc("/repos/shiftu/aivet/releases/latest", func(w http.ResponseWriter, r *http.Request) { rel(w, "v9.9.9") })
	mux.HandleFunc("/repos/shiftu/aivet/releases/tags/v0.1.0", func(w http.ResponseWriter, r *http.Request) { rel(w, "v0.1.0") })
	mux.HandleFunc("/dl/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  aivet_darwin_arm64\n", hex.EncodeToString(sum[:]))
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv, hex.EncodeToString(sum[:])
}

func TestFetchLatestAndTagged(t *testing.T) {
	srv, _ := fakeGitHub(t, []byte("hello"))
	ctx := context.Background()

	rel, err := Fetch(ctx, srv.Client(), srv.URL, DefaultRepo, "")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v9.9.9" {
		t.Errorf("最新版拿成了 %q", rel.Tag)
	}
	if rel.Assets["aivet_darwin_arm64"] == "" || rel.Assets[SumsAsset] == "" {
		t.Errorf("资产没收全：%v", rel.Assets)
	}
	if rel, err = Fetch(ctx, srv.Client(), srv.URL, DefaultRepo, "v0.1.0"); err != nil || rel.Tag != "v0.1.0" {
		t.Errorf("按 tag 查拿到 %q, %v", rel.Tag, err)
	}
	if _, err := Fetch(ctx, srv.Client(), srv.URL, DefaultRepo, "v0.0.404"); err == nil {
		t.Error("不存在的版本该报错")
	}
}

// 被限流跟网络坏了是两回事，错误里得说得出区别，不然用户会去查网络。
func TestFetchNamesRateLimitForWhatItIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), srv.Client(), srv.URL, DefaultRepo, "")
	if err == nil || !strings.Contains(err.Error(), "限流") {
		t.Errorf("403 该说成限流，实际是：%v", err)
	}
}

func TestDownloadWritesFileAndSum(t *testing.T) {
	body := strings.Repeat("x", 5000)
	srv, want := fakeGitHub(t, []byte(body))
	dir := t.TempDir()

	var counted int64
	path, sum, err := Download(context.Background(), srv.Client(), srv.URL+"/dl/bin", dir,
		writerFunc(func(p []byte) (int, error) { counted += int64(len(p)); return len(p), nil }))
	if err != nil {
		t.Fatal(err)
	}
	if sum != want {
		t.Errorf("算出的校验和 %s，实际 %s", sum, want)
	}
	if counted != int64(len(body)) {
		t.Errorf("喂给进度条 %d 字节，实际下了 %d", counted, len(body))
	}
	if filepath.Dir(path) != dir {
		t.Errorf("临时文件落在 %s，应该跟目标同目录（跨盘 rename 不是原子的）", filepath.Dir(path))
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != body {
		t.Errorf("落盘的内容不对：%v", err)
	}
	if _, _, err := Download(context.Background(), srv.Client(), srv.URL+"/dl/404", dir, nil); err == nil {
		t.Error("404 该报错")
	}
}

// 下坏了不能留下垃圾：临时文件跟目标同目录，攒着迟早被人当成真文件。
func TestDownloadCleansUpOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("只写一半就断"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		srvHijackClose(w)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if _, _, err := Download(context.Background(), srv.Client(), srv.URL, dir, nil); err == nil {
		t.Fatal("下到一半断了该报错")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Errorf("失败后留下了 %d 个临时文件", len(ents))
	}
}

func TestReplaceSwapsBinary(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "aivet")
	src := filepath.Join(dir, ".new")
	if err := os.WriteFile(dst, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(dst, src); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "new" {
		t.Fatalf("没换成新的：%q %v", b, err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("换上去的文件没有执行权限：%v", fi.Mode())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// srvHijackClose 掐断连接，模拟「说好 1000 字节只给了一半」。
func srvHijackClose(w http.ResponseWriter) {
	if h, ok := w.(http.Hijacker); ok {
		if c, _, err := h.Hijack(); err == nil {
			c.Close()
		}
	}
}
