// Package selfupdate 让 aivet 从 GitHub release 把自己换成新版。
//
// 一件事得先说清楚：这是在往用户机器上放一个会被直接执行的文件。所以流程里
// 没有「大概对了就行」—— 发布时一起传的 SHA256SUMS 是必对的一环，对不上就
// 当场删掉不装。跳过校验的自更新等于给自己开了一条后门。
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultRepo 是 aivet 自己的仓库。
const DefaultRepo = "shiftu/aivet"

// DefaultAPI 是 GitHub 的 API 根地址；测试会指到 httptest 上。
const DefaultAPI = "https://api.github.com"

// SumsAsset 是发布时一起传的校验和文件名。
const SumsAsset = "SHA256SUMS"

// Release 是一次发布里我们关心的那部分。
type Release struct {
	Tag    string
	Assets map[string]string // 资产名 -> 下载地址
}

// AssetName 返回当前平台该下哪个文件。这几个名字由 Makefile 的 TARGETS 决定，
// 对不上的表现是 404 而不是「装错了」，所以有测试拿 Makefile 钉着。
func AssetName(goos, goarch string) string {
	name := "aivet_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Client 是带超时的 http 客户端。刻意不设总超时 —— 下载几 MB 在慢网上要好几分钟，
// 一刀切的总超时会把正常的下载掐断。只卡建连和「服务器迟迟不回头」。
func Client() *http.Client {
	return &http.Client{Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
	}}
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Fetch 查一次 release。tag 为空查最新的。
func Fetch(ctx context.Context, c *http.Client, api, repo, tag string) (Release, error) {
	url := api + "/repos/" + repo + "/releases/latest"
	if tag != "" {
		url = api + "/repos/" + repo + "/releases/tags/" + tag
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("连不上 GitHub：%w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound && tag != "":
		return Release{}, fmt.Errorf("没有 %s 这个版本", tag)
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests:
		// 匿名调 GitHub API 是 60 次/小时。说清楚是被限流了，不然看着像网络坏了。
		return Release{}, fmt.Errorf("GitHub 拒绝了（%s）—— 多半是匿名调用被限流，过会儿再试", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("GitHub 返回 %s", resp.Status)
	}
	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&gr); err != nil {
		return Release{}, fmt.Errorf("读不懂 GitHub 的回复：%w", err)
	}
	if gr.TagName == "" {
		return Release{}, fmt.Errorf("GitHub 没给版本号")
	}
	rel := Release{Tag: gr.TagName, Assets: make(map[string]string, len(gr.Assets))}
	for _, a := range gr.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// Download 把 url 下到 dir 下的一个临时文件里，边下边算 SHA256，同时把字节数
// 喂给 prog（进度条，可以为 nil）。返回临时文件路径和十六进制校验和。
//
// 临时文件放在 dir 而不是系统临时目录：待会儿要 rename 成目标文件，
// 跨盘的 rename 不是原子的，在有些系统上直接就失败。
func Download(ctx context.Context, c *http.Client, url, dir string, prog io.Writer) (path, sum string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("下载失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("下载 %s 返回 %s", filepath.Base(url), resp.Status)
	}

	f, err := os.CreateTemp(dir, ".aivet-update-*")
	if err != nil {
		return "", "", fmt.Errorf("在 %s 建临时文件失败：%w", dir, err)
	}
	h := sha256.New()
	dst := []io.Writer{f, h}
	if prog != nil {
		dst = append(dst, prog)
	}
	_, cErr := io.Copy(io.MultiWriter(dst...), resp.Body)
	closeErr := f.Close()
	if cErr != nil || closeErr != nil {
		os.Remove(f.Name())
		if cErr != nil {
			return "", "", fmt.Errorf("下载中断：%w", cErr)
		}
		return "", "", fmt.Errorf("写临时文件失败：%w", closeErr)
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// Size 问一下这个文件多大，好让进度条有个总数。问不到返回 0 ——
// 进度条会退化成转圈，不该因为这个就不给下。
func Size(ctx context.Context, c *http.Client, url string) int64 {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength < 0 {
		return 0
	}
	return resp.ContentLength
}

// ParseSums 解析 shasum 那种「校验和 <空格> 文件名」的文本。
func ParseSums(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		// shasum 的二进制模式会在文件名前加个 *，去掉。
		out[strings.TrimPrefix(f[len(f)-1], "*")] = f[0]
	}
	return out
}

// Target 找出「该被替换掉的那个文件」。
//
// 走一次 EvalSymlinks 是必要的：aivet 要是个软链（brew 装的、~/bin 里手工链的），
// 直接覆盖软链会把链接本身变成一个真文件，原来的指向就断了。要换的是它指向的那个。
func Target() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("找不到自己在哪：%w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real, nil
	}
	return exe, nil
}

// Replace 用 src 换掉 dst（正在跑的那个二进制）。
//
// 同目录的 rename 是原子的：要么还是旧的、要么已经是新的，中间不会出现半个文件。
// Windows 例外 —— 正在跑的 exe 不能被覆盖，只能先把旧的挪开再放新的。
func Replace(dst, src string) error {
	if err := os.Chmod(src, 0o755); err != nil {
		return fmt.Errorf("给新文件加执行权限失败：%w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("换掉 %s 失败：%w", dst, err)
		}
		return nil
	}
	old := dst + ".old"
	os.Remove(old) // 上次留下的，能删就删
	if err := os.Rename(dst, old); err != nil {
		return fmt.Errorf("挪开旧文件失败：%w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		os.Rename(old, dst) // 放不进去就把旧的放回来，别让用户手里什么都不剩
		return fmt.Errorf("放入新文件失败：%w", err)
	}
	// 正在跑的那个删不掉很正常，下次更新时会被清掉。
	os.Remove(old)
	return nil
}
