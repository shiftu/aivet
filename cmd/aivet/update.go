package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shiftu/aivet/internal/platform"
	"github.com/shiftu/aivet/internal/probe"
	"github.com/shiftu/aivet/internal/selfupdate"
	"github.com/shiftu/aivet/internal/ui"
)

// updateEnv 是「跟谁要、换掉哪个文件、当自己是什么平台」。
//
// 单拿出来是为了能测：这段代码的失败模式（校验和对不上、这个版本没发你的平台、
// 目录不可写）恰恰是最需要测、又最不能拿真 GitHub 和真二进制去试的那几种。
type updateEnv struct {
	api    string
	repo   string
	target string // 要被换掉的那个文件；空 = 问 selfupdate.Target()
	goos   string
	goarch string
}

func liveEnv() updateEnv {
	return updateEnv{
		api: selfupdate.DefaultAPI, repo: selfupdate.DefaultRepo,
		goos: runtime.GOOS, goarch: runtime.GOARCH,
	}
}

// runUpdate 把 aivet 自己换成 GitHub 上的新版。
//
// 顺序是刻意的：先问清楚要装哪个版本、需不需要装，再动手下载；下完先对校验和，
// 对上了才碰目标文件。也就是说，任何一步失败，用户手里那个 aivet 还是完好的。
func runUpdate(ctx context.Context, args []string) int {
	return update(ctx, args, liveEnv(), newPrinter(false))
}

func update(ctx context.Context, args []string, env updateEnv, pr ui.Printer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "只看有没有新版")
	force := fs.Bool("force", false, "版本一样也重装")
	want := fs.String("version", "", "指定版本")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "参数不对：%v\n看用法：aivet help update\n", err)
		return 2
	}

	pr.Section("更新")
	pr.Line("info", "当前 "+pr.P.Bold("v"+strings.TrimPrefix(version, "v"))+pr.P.Dim("  ·  "+platform.Label()))

	c := selfupdate.Client()
	rel, err := selfupdate.Fetch(ctx, c, env.api, env.repo, *want)
	if err != nil {
		pr.Line("fail", err.Error())
		return 1
	}

	// 版本号比不出来（dev 构建、自己编的）时不能默认「已经最新」——
	// 那等于把一次明确的更新请求悄悄吃掉。说清楚比不了，然后照装。
	cmp, comparable := probe.CompareVersions(version, rel.Tag)
	switch {
	case *want != "":
		pr.Line("info", "要装 "+pr.P.Bold(rel.Tag))
	case !comparable:
		pr.Line("warn", "当前版本号读不出来（多半是自己编的 dev 构建），没法跟 "+rel.Tag+" 比。")
	case cmp >= 0 && !*force:
		pr.Line("ok", "已经是最新的了（"+rel.Tag+"）。")
		return 0
	default:
		pr.Line("info", "有新版 "+pr.P.Bold(rel.Tag))
	}
	if *check {
		if comparable && cmp < 0 {
			pr.Line("info", "装上它：aivet update")
		}
		fmt.Fprintln(pr.W)
		return 0
	}
	if comparable && cmp >= 0 && *force {
		pr.Line("info", "--force：版本一样也重装一次")
	}

	asset := selfupdate.AssetName(env.goos, env.goarch)
	binURL, ok := rel.Assets[asset]
	if !ok {
		pr.Line("fail", rel.Tag+" 里没有 "+asset+" —— 这个版本没发你这个平台的包。")
		return 1
	}
	sumsURL, ok := rel.Assets[selfupdate.SumsAsset]
	if !ok {
		// 有二进制没校验和，就没法确认下到的是不是发布的那个。宁可不装。
		pr.Line("fail", rel.Tag+" 没有 "+selfupdate.SumsAsset+"，校验不了。不装。")
		return 1
	}

	target := env.target
	if target == "" {
		t, err := selfupdate.Target()
		if err != nil {
			pr.Line("fail", err.Error())
			return 1
		}
		target = t
	}
	dir := filepath.Dir(target)
	if err := writable(dir); err != nil {
		pr.Line("fail", "没有写 "+dir+" 的权限。")
		pr.Line("info", "换个方式：sudo aivet update，或者重跑安装脚本装到 ~/.local/bin。")
		return 1
	}

	sums, err := fetchSums(ctx, c, sumsURL, dir)
	if err != nil {
		pr.Line("fail", err.Error())
		return 1
	}
	wantSum, ok := sums[asset]
	if !ok {
		pr.Line("fail", selfupdate.SumsAsset+" 里没有 "+asset+" 那一行，校验不了。不装。")
		return 1
	}

	bar := &ui.Progress{
		W: pr.W, P: pr.P, Live: ui.IsTTY(),
		Label: asset,
		Total: selfupdate.Size(ctx, c, binURL),
	}
	tmp, gotSum, err := selfupdate.Download(ctx, c, binURL, dir, bar)
	if err != nil {
		bar.Abort()
		pr.Line("fail", err.Error())
		return 1
	}
	bar.Done()

	if gotSum != wantSum {
		os.Remove(tmp)
		pr.Line("fail", "校验和对不上，下到的不是发布的那个文件。已经删掉，没有安装。")
		pr.Line("info", "期望 "+wantSum[:16]+"…  实际 "+gotSum[:16]+"…")
		return 1
	}
	pr.Line("ok", "校验和对上了 "+pr.P.Dim(wantSum[:16]+"…"))

	if err := selfupdate.Replace(target, tmp); err != nil {
		os.Remove(tmp)
		pr.Line("fail", err.Error())
		return 1
	}
	pr.Line("ok", "已更新 "+pr.P.Bold("v"+strings.TrimPrefix(version, "v"))+" → "+pr.P.Bold(rel.Tag)+pr.P.Dim("  ·  "+target))
	fmt.Fprintln(pr.W, pr.P.Dim("\n  改动看这里：https://github.com/"+selfupdate.DefaultRepo+"/releases/tag/"+rel.Tag+"\n"))
	return 0
}

// fetchSums 把校验和文件读进来。它只有几百字节，不值得给进度条。
func fetchSums(ctx context.Context, c *http.Client, url, dir string) (map[string]string, error) {
	path, _, err := selfupdate.Download(ctx, c, url, dir, nil)
	if err != nil {
		return nil, fmt.Errorf("拿不到 %s：%w", selfupdate.SumsAsset, err)
	}
	defer os.Remove(path)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return selfupdate.ParseSums(string(b)), nil
}

// writable 试着在目录里建个文件 —— 判断「能不能写」唯一靠得住的办法就是真去写一下。
// 光看权限位会在 root 拥有的目录、只读挂载、Windows ACL 上给出错的答案。
func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".aivet-perm-*")
	if err != nil {
		return errors.New("不可写")
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}
