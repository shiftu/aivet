package probe

import (
	"context"
	"sync"
)

// GatewayCache 去重：六件工具常指向同一个网关，同一组合只探一次。
type GatewayCache struct {
	mu     sync.Mutex
	models map[string]modelsEntry
	pings  map[string]PingResult
}

type modelsEntry struct {
	ids []string
	pr  PingResult
}

// NewGatewayCache 建缓存。
func NewGatewayCache() *GatewayCache {
	return &GatewayCache{models: map[string]modelsEntry{}, pings: map[string]PingResult{}}
}

// Models 拉一次模型清单（缓存）。
func (g *GatewayCache) Models(ctx context.Context, ep Endpoint) ([]string, PingResult) {
	k := NormalizeBase(ep.BaseURL) + "|" + ep.Key
	g.mu.Lock()
	if e, ok := g.models[k]; ok {
		g.mu.Unlock()
		return e.ids, e.pr
	}
	g.mu.Unlock()
	ids, pr := ListModels(ctx, ep)
	g.mu.Lock()
	g.models[k] = modelsEntry{ids, pr}
	g.mu.Unlock()
	return ids, pr
}

// Ping 发一次最小请求（缓存）。
func (g *GatewayCache) Ping(ctx context.Context, ep Endpoint, model string) PingResult {
	k := NormalizeBase(ep.BaseURL) + "|" + ep.Key + "|" + string(ep.Protocol) + "|" + model
	g.mu.Lock()
	if pr, ok := g.pings[k]; ok {
		g.mu.Unlock()
		return pr
	}
	g.mu.Unlock()
	pr := Ping(ctx, ep, model)
	g.mu.Lock()
	g.pings[k] = pr
	g.mu.Unlock()
	return pr
}

// HasModel 判断模型是否在清单里（精确匹配）。
func HasModel(ids []string, model string) bool {
	for _, id := range ids {
		if id == model {
			return true
		}
	}
	return false
}
