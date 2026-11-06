package upstream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// globalAuth 构造 realm=global 的账号（Realm() 依赖 globalEnabled 开关 + BackfillRealmFor）。
func globalAuthForVersion(t *testing.T) *auth.Auth {
	t.Helper()
	a := &auth.Auth{AccessToken: "at", UID: "u1"}
	if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
		t.Fatalf("BackfillRealmFor: %v", err)
	}
	return a
}

// cnAuth 构造 realm=cn 的账号。
func cnAuthForVersion() *auth.Auth {
	return &auth.Auth{AccessToken: "at", UID: "u1"}
}

// TestClientVersionForRealm 钉住双版本号的优先级与回落语义：
//   - 域专属版本优先于统一 client_version；
//   - 域专属留空回落到统一版本；
//   - 全部留空回落内置默认；
//   - 无账号上下文（clientVersion）只看统一版本。
func TestClientVersionForRealm(t *testing.T) {
	cases := []struct {
		name                              string
		unified, cn, global               string
		wantCN, wantGlobal, wantNoContext string
	}{
		{"all empty", "", "", "", defaultClientVersion, defaultClientVersion, defaultClientVersion},
		{"unified only", "5.9.3", "", "", "5.9.3", "5.9.3", "5.9.3"},
		{"cn override", "5.9.3", "5.9.9", "", "5.9.9", "5.9.3", "5.9.3"},
		{"global override", "5.9.3", "", "5.9.4", "5.9.3", "5.9.4", "5.9.3"},
		{"both override", "5.9.3", "5.9.9", "5.9.4", "5.9.9", "5.9.4", "5.9.3"},
		{"per-realm only", "", "5.9.9", "5.9.4", "5.9.9", "5.9.4", defaultClientVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{
				ClientVersion:       tc.unified,
				ClientVersionCN:     tc.cn,
				ClientVersionGlobal: tc.global,
			}
			if got := c.clientVersionForRealm("cn"); got != tc.wantCN {
				t.Errorf("cn = %q want %q", got, tc.wantCN)
			}
			if got := c.clientVersionForRealm("global"); got != tc.wantGlobal {
				t.Errorf("global = %q want %q", got, tc.wantGlobal)
			}
			if got := c.clientVersion(); got != tc.wantNoContext {
				t.Errorf("no-context = %q want %q", got, tc.wantNoContext)
			}
		})
	}
}

// TestSetClientVersions 热更新：面板保存配置后无需重启即可让下一请求用上新版本。
func TestSetClientVersions(t *testing.T) {
	c := &Client{ClientVersion: "5.5.4"}
	if got := c.clientVersionForRealm("global"); got != "5.5.4" {
		t.Fatalf("before set: %q", got)
	}
	c.SetClientVersions("5.9.3", "5.9.9", "5.9.4")
	if got := c.clientVersionForRealm("cn"); got != "5.9.9" {
		t.Errorf("cn after set = %q want 5.9.9", got)
	}
	if got := c.clientVersionForRealm("global"); got != "5.9.4" {
		t.Errorf("global after set = %q want 5.9.4", got)
	}
	if got := c.clientVersion(); got != "5.9.3" {
		t.Errorf("unified after set = %q want 5.9.3", got)
	}
	// 清空域专属 → 回落统一版本
	c.SetClientVersions("5.9.3", "", "")
	if got := c.clientVersionForRealm("cn"); got != "5.9.3" {
		t.Errorf("cn after clear = %q want 5.9.3", got)
	}
}

// TestDefaultWorkBuddyUAPerRealmVersion 出站 UA 三段式按账号域取对应版本：
// CN 账号用国内版、global 账号用国际版；platform 段品牌仍按域切。
func TestDefaultWorkBuddyUAPerRealmVersion(t *testing.T) {
	c := &Client{
		ClientVersionCN:     "5.9.3",
		ClientVersionGlobal: "5.9.4",
		CliVersion:          "2.137.1",
	}
	cn := c.defaultWorkBuddyUAFor(cnAuthForVersion())
	if !strings.Contains(cn, "WorkBuddy/5.9.3 WorkBuddy/5.9.3 CLI/2.137.1") {
		t.Errorf("cn UA = %q", cn)
	}
	if strings.Contains(cn, "WorkBuddy AI") {
		t.Errorf("cn UA 不该含 WorkBuddy AI: %q", cn)
	}
	gl := c.defaultWorkBuddyUAFor(globalAuthForVersion(t))
	if !strings.Contains(gl, "WorkBuddy/5.9.4 WorkBuddy AI/5.9.4 CLI/2.137.1") {
		t.Errorf("global UA = %q", gl)
	}
}

// TestXIDEVersionPerRealm ChatHeaders 注入的 X-IDE-Version 按账号域取对应版本。
func TestXIDEVersionPerRealm(t *testing.T) {
	c := &Client{
		ClientVersionCN:     "5.9.3",
		ClientVersionGlobal: "5.9.4",
	}
	check := func(a *auth.Auth, want string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "https://www.workbuddy.ai/v2/chat/completions", nil)
		c.ChatHeaders(req, a, "", ChatMeta{})
		if got := req.Header.Get("X-IDE-Version"); got != want {
			t.Errorf("X-IDE-Version = %q want %q", got, want)
		}
	}
	check(cnAuthForVersion(), "5.9.3")
	check(globalAuthForVersion(t), "5.9.4")
}

// TestBillingUAPerRealmVersion billing 单段 UA 也按账号域取对应版本。
func TestBillingUAPerRealmVersion(t *testing.T) {
	c := &Client{
		ClientVersionCN:     "5.9.3",
		ClientVersionGlobal: "5.9.4",
		ClientName:          "WorkBuddy",
	}
	if got := c.billingUAFor(cnAuthForVersion()); got != "WorkBuddy/5.9.3" {
		t.Errorf("cn billing UA = %q", got)
	}
	if got := c.billingUAFor(globalAuthForVersion(t)); got != "WorkBuddy/5.9.4" {
		t.Errorf("global billing UA = %q", got)
	}
}

// TestOfficialVersionReportPerRealm 分域落后判定：报告按域取 curVer 各自比较，
// 兼容字段 Outdated = 两域有任一落后（探测走外网在 CI 不可靠，这里验证比较口径）。
func TestOfficialVersionReportPerRealm(t *testing.T) {
	c := &Client{ClientVersionCN: "5.9.3", ClientVersionGlobal: "5.8.0"}
	curCN := c.clientVersionForRealm("cn")
	curGlb := c.clientVersionForRealm("global")
	if curCN != "5.9.3" || curGlb != "5.8.0" {
		t.Fatalf("setup: cn=%q global=%q", curCN, curGlb)
	}
	outdatedCN := isSemverOutdated(curCN, "5.9.3")
	outdatedGlobal := isSemverOutdated(curGlb, "5.9.4")
	if outdatedCN {
		t.Error("cn 5.9.3 shouldn't be outdated vs 5.9.3")
	}
	if !outdatedGlobal {
		t.Error("global 5.8.0 should be outdated vs 5.9.4")
	}
	if !(outdatedCN || outdatedGlobal) {
		t.Error("compat Outdated should be true when any realm is behind")
	}
}
