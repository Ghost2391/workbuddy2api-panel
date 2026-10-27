// official_version.go 探测 WorkBuddy 官方客户端最新分发版本（/v2/update）。
//
// 目的：防风控指纹老化——上游每逢中版本迭代（如 5.5 → 5.6 → 5.8），风控可能逐步
// 淘汰过老版本的客户端 UA。面板通过本探测知晓官方当前发布的最新版本，并在当前
// 模拟版本过旧时提醒运维跟进配置（upstream.client_version），保持出站指纹新鲜。
//
// 上游接口（实测）：
//   - GET https://copilot.tencent.com/v2/update?platform=workbuddy-win32-x64-user&format=exe
//   - GET https://www.workbuddy.ai/v2/update?platform=workbuddy-win32-x64-user&format=exe
//   - 请求头带 X-Product: SaaS；免鉴权，零消耗。
package upstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OfficialUpdateInfo 官方单个渠道的更新元数据。
type OfficialUpdateInfo struct {
	Version        string `json:"version"`
	ProductVersion string `json:"productVersion"`
	URL            string `json:"url"`
	Timestamp      int64  `json:"timestamp"`
}

// OfficialVersionReport 官方版本探测报告。
type OfficialVersionReport struct {
	CN                   *OfficialUpdateInfo `json:"cn,omitempty"`
	Global               *OfficialUpdateInfo `json:"global,omitempty"`
	CurrentClientVersion string              `json:"current_client_version"`
	Outdated             bool                `json:"outdated"`
	LatestCN             string              `json:"latest_cn"`
	LatestGlobal         string              `json:"latest_global"`
	LastChecked          time.Time           `json:"last_checked"`
}

type officialVersionCache struct {
	sync.Mutex
	report  OfficialVersionReport
	fetched time.Time
}

var officialCache officialVersionCache

const officialVersionTTL = 6 * time.Hour

const updatePlatformParam = "workbuddy-win32-x64-user"

// CheckOfficialVersions 查询官方国内版与国际版最新客户端版本，并与当前模拟版本对比。
// 6 小时进程内缓存，避免频繁打官方更新服务。
func (c *Client) CheckOfficialVersions() OfficialVersionReport {
	officialCache.Lock()
	defer officialCache.Unlock()

	curVer := c.clientVersion()
	if time.Since(officialCache.fetched) < officialVersionTTL && officialCache.report.CurrentClientVersion == curVer {
		return officialCache.report
	}

	report := OfficialVersionReport{
		CurrentClientVersion: curVer,
		LastChecked:          time.Now(),
	}

	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 10 * time.Second}
	}

	// 探测国内版
	cnURL := "https://copilot.tencent.com/v2/update?platform=" + updatePlatformParam + "&format=exe"
	if info, err := fetchOfficialUpdate(httpc, cnURL); err == nil && info != nil {
		report.CN = info
		report.LatestCN = baseSemver(info.Version)
	}

	// 探测国际版
	globalURL := "https://www.workbuddy.ai/v2/update?platform=" + updatePlatformParam + "&format=exe"
	if info, err := fetchOfficialUpdate(httpc, globalURL); err == nil && info != nil {
		report.Global = info
		report.LatestGlobal = baseSemver(info.Version)
	}

	// 比较当前版本是否落后于任一官方版本
	if (report.LatestCN != "" && isSemverOutdated(curVer, report.LatestCN)) ||
		(report.LatestGlobal != "" && isSemverOutdated(curVer, report.LatestGlobal)) {
		report.Outdated = true
	}

	officialCache.report = report
	officialCache.fetched = time.Now()
	return report
}

func fetchOfficialUpdate(httpc *http.Client, url string) (*OfficialUpdateInfo, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Product", "SaaS")
	req.Header.Set("User-Agent", "WorkBuddy/5.5.4 WorkBuddy/5.5.4 CLI/2.137.1")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}

	var info OfficialUpdateInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, err
	}
	if info.Version == "" {
		return nil, fmt.Errorf("empty version in update response")
	}
	return &info, nil
}

// baseSemver 提取版本号前三段主版本（例如 "5.8.3.41334564" → "5.8.3"）。
func baseSemver(v string) string {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) >= 3 {
		return strings.Join(parts[:3], ".")
	}
	return strings.TrimSpace(v)
}

// isSemverOutdated 判定 current 是否严格小于 target（三段比较）。
func isSemverOutdated(current, target string) bool {
	parse := func(s string) [3]int {
		var out [3]int
		parts := strings.Split(baseSemver(s), ".")
		for i := 0; i < len(parts) && i < 3; i++ {
			n, _ := strconv.Atoi(parts[i])
			out[i] = n
		}
		return out
	}
	cur, tgt := parse(current), parse(target)
	for i := 0; i < 3; i++ {
		if cur[i] < tgt[i] {
			return true
		}
		if cur[i] > tgt[i] {
			return false
		}
	}
	return false
}
