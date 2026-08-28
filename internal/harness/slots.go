package harness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shiftu/aivet/internal/probe"
)

// ModelSlot 是「主模型之外、配置里还写着的一个模型名」。
//
// 每件工具都不止一个模型槽：codex 有 review_model，Claude Code 有 sonnet/opus/haiku
// 三个别名，hermes / pi / dsh 各自声明了一份可切换的菜单。这些名字用户随时会切过去，
// 切过去才 404 的话，之前那份「全部通过」就是骗人的 —— 只验主模型等于只验了一条路。
type ModelSlot struct {
	From  string // 这个名字是从哪来的：review_model / sonnet 别名 / 已启用 / 声明的
	Model string
}

// Slot 是构造 ModelSlot 的糖。
func Slot(from, model string) ModelSlot { return ModelSlot{From: from, Model: model} }

// CheckOtherModels 把副槽一次性对着网关清单核对，产出一条 <tool>.models。
//
// 只做清单核对，不发真请求：清单这时已经缓存了，等于免费；而挨个发请求会让
// 一次体检从两秒变成一分钟。核对不上报 Warn 不报 Fail —— 用户未必用得上这些槽，
// 但迟早会切过去，所以也不能不说。
//
// primary 是已经单独验过的主模型，从副槽里剔掉，免得重复报一遍。
func CheckOtherModels(c *Context, b *Builder, ep probe.Endpoint, primary string, slots []ModelSlot) {
	wanted, from := dedupeSlots(primary, slots)
	if len(wanted) == 0 {
		return
	}
	list := strings.Join(wanted, "、")
	if c.Offline || ep.Key == "" || ep.BaseURL == "" {
		b.Skip("models", "其他模型", fmt.Sprintf("配置里还有 %d 个模型（%s），没探", len(wanted), list))
		return
	}
	ids, pr := c.Gateways.Models(c.Ctx, ep)
	if !pr.OK || len(ids) == 0 {
		// 清单拉不到时不能说这些模型有问题 —— 那是替一个查不到的东西下结论。
		b.Skip("models", "其他模型", fmt.Sprintf("配置里还有 %d 个模型（%s），但网关没给出清单，核对不了", len(wanted), list))
		return
	}
	var missing []string
	for _, m := range wanted {
		if !probe.HasModel(ids, m) {
			missing = append(missing, fmt.Sprintf("%s（%s）", m, from[m]))
		}
	}
	if len(missing) == 0 {
		b.OK("models", "其他模型", fmt.Sprintf("%d 个都在清单里：%s", len(wanted), list))
		return
	}
	b.Warn("models", "其他模型", fmt.Sprintf("网关清单里没有：%s", strings.Join(missing, "、")),
		"现在不影响主模型；但在工具里切到这几个会 404。改成清单里有的，或让网关补上")
}

// dedupeSlots 去重、剔掉主模型和空值，返回排好序的模型名和「它是从哪来的」。
// 同一个模型名被多处引用时，来源合并起来一起说，不然用户不知道该改哪儿。
func dedupeSlots(primary string, slots []ModelSlot) ([]string, map[string]string) {
	froms := map[string][]string{}
	for _, s := range slots {
		if s.Model == "" || s.Model == primary {
			continue
		}
		froms[s.Model] = appendUnique(froms[s.Model], s.From)
	}
	names := make([]string, 0, len(froms))
	for m := range froms {
		names = append(names, m)
	}
	sort.Strings(names)
	from := make(map[string]string, len(froms))
	for m, fs := range froms {
		from[m] = strings.Join(fs, " / ")
	}
	return names, from
}

func appendUnique(xs []string, x string) []string {
	for _, e := range xs {
		if e == x {
			return xs
		}
	}
	return append(xs, x)
}
