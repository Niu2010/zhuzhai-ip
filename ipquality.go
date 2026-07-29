package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ipQualityInfo 是 ip-api.com 返回结果里跟"是不是机房 IP"相关的字段。
type ipQualityInfo struct {
	Status  string `json:"status"`
	Hosting bool   `json:"hosting"`
	ISP     string `json:"isp"`
	Org     string `json:"org"`
}

type ipQualityResult struct {
	residential bool
	checkedAt   time.Time
}

var (
	ipQualityCache   = map[string]ipQualityResult{}
	ipQualityCacheMu sync.Mutex
)

// ipQualityCacheTTL 缓存有效期。VPN Gate 同一个节点短时间内会被反复
// 选中做候选，缓存住避免重复打 ip-api.com 撞到免费额度的限流。
const ipQualityCacheTTL = 30 * time.Minute

// isResidentialIP 判断这个出口 IP 是不是家宽（非机房）IP。
//
// 查询失败（超时/限流/网络问题）时默认放行：宁可漏过几个没查清楚的机房 IP，
// 也不能让第三方查询接口不稳定直接卡死整条换节点流程。
func isResidentialIP(ip string) bool {
	ipQualityCacheMu.Lock()
	if cached, ok := ipQualityCache[ip]; ok && time.Since(cached.checkedAt) < ipQualityCacheTTL {
		ipQualityCacheMu.Unlock()
		return cached.residential
	}
	ipQualityCacheMu.Unlock()

	residential, checked := queryIPQuality(ip)
	if checked {
		ipQualityCacheMu.Lock()
		ipQualityCache[ip] = ipQualityResult{residential: residential, checkedAt: time.Now()}
		ipQualityCacheMu.Unlock()
	}
	return residential
}

// queryIPQuality 查一次 ip-api.com。
// 第二个返回值表示"这次查询是否真的拿到了结果"，拿不到时上层不应该缓存。
func queryIPQuality(ip string) (residential bool, checked bool) {
	client := &http.Client{Timeout: 6 * time.Second}
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,hosting,isp,org", ip)
	resp, err := client.Get(url)
	if err != nil {
		return true, false
	}
	defer resp.Body.Close()

	var info ipQualityInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil || info.Status != "success" {
		return true, false
	}
	return !info.Hosting, true
}
