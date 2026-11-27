// Package mobile 是 Android 签到 App 的 gomobile 绑定入口。
// 只暴露 string/error，数据以 JSON 字符串交换；签到逻辑复用 internal/auth 与
// internal/upstream，不在 Kotlin 侧重写协议。
package mobile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

const (
	requestTimeout     = 35 * time.Second
	dnsPrimary         = "223.5.5.5"
	dnsSecondary       = "119.29.29.29"
	refreshWindow      = 2 * time.Hour
	expiringSoonWindow = 7 * 24 * time.Hour
	stateFileName      = "checkin_state.json"

	loginBaseCN       = "https://copilot.tencent.com"
	loginBaseGlobal   = "https://www.workbuddy.ai"
	loginOriginCN     = "https://www.codebuddy.cn"
	loginOriginGlobal = "https://www.workbuddy.ai"
	loginClientUA     = "CLI/2.63.2 CodeBuddy/2.63.2"
	loginTTL          = 10 * time.Minute
)

type loginSession struct {
	created time.Time
	realm   string
}

// Engine 持有账号目录与上游客户端；每个方法都可从任意线程调用，
// 内部用互斥锁串行化操作，避免并发签到互相覆盖已轮换的 token。
type Engine struct {
	mu  sync.Mutex
	dir string
	up  *upstream.Client

	logins          map[string]loginSession
	loginBaseCN     string
	loginBaseGlobal string
	now             func() time.Time

	// testHook 仅供测试观察串行化；生产恒为 nil。
	testHook func()
}

// NewEngine 创建引擎；dir 为 App 私有账号目录。
// 显式 DNS：Android 没有 /etc/resolv.conf，Go 解析器会退回 [::1]:53，
// 传入公共 DNS 绕开（见 internal/upstream/dns.go 注释）。
func NewEngine(dir string) *Engine {
	up := upstream.New()
	up.HTTP.Timeout = requestTimeout
	up.SetDNSDialer(upstream.NewDNSDialer([]string{dnsPrimary, dnsSecondary}))
	return &Engine{
		dir:             dir,
		up:              up,
		logins:          map[string]loginSession{},
		loginBaseCN:     loginBaseCN,
		loginBaseGlobal: loginBaseGlobal,
		now:             time.Now,
	}
}

// newEngineWith 仅供测试注入替换后的上游客户端。
func newEngineWith(dir string, up *upstream.Client) *Engine {
	return &Engine{
		dir:             dir,
		up:              up,
		logins:          map[string]loginSession{},
		loginBaseCN:     loginBaseCN,
		loginBaseGlobal: loginBaseGlobal,
		now:             time.Now,
	}
}

type accountJSON struct {
	UID             string         `json:"uid"`
	Nickname        string         `json:"nickname"`
	Realm           string         `json:"realm"`
	Enterprise      bool           `json:"enterprise"`
	File            string         `json:"file"`
	LastCheckin     *checkinResult `json:"last_checkin,omitempty"`
	HasCredits      bool           `json:"has_credits,omitempty"`
	CreditsRemain   int64          `json:"credits_remain,omitempty"`
	CreditsTotal    int64          `json:"credits_total,omitempty"`
	CreditsExpiring int64          `json:"credits_expiring,omitempty"`
	CreditsAt       string         `json:"credits_at,omitempty"`
}

type checkinResult struct {
	UID             string `json:"uid"`
	Nickname        string `json:"nickname"`
	Status          string `json:"status"`
	Message         string `json:"message"`
	CheckedAt       string `json:"checked_at"`
	HasCredits      bool   `json:"has_credits,omitempty"`
	CreditsRemain   int64  `json:"credits_remain,omitempty"`
	CreditsTotal    int64  `json:"credits_total,omitempty"`
	CreditsExpiring int64  `json:"credits_expiring,omitempty"`
	CreditsAt       string `json:"credits_at,omitempty"`
	CreditsError    string `json:"credits_error,omitempty"`
}

type persistedAccountState struct {
	LastCheckinDay  string `json:"last_checkin_day,omitempty"`
	Status          string `json:"status,omitempty"`
	Message         string `json:"message,omitempty"`
	CheckedAt       string `json:"checked_at,omitempty"`
	HasCredits      bool   `json:"has_credits,omitempty"`
	CreditsRemain   int64  `json:"credits_remain,omitempty"`
	CreditsTotal    int64  `json:"credits_total,omitempty"`
	CreditsExpiring int64  `json:"credits_expiring,omitempty"`
	CreditsAt       string `json:"credits_at,omitempty"`
}

type startLoginResult struct {
	State string `json:"state"`
	URL   string `json:"url"`
	Realm string `json:"realm"`
}

type pollLoginResult struct {
	Done    bool         `json:"done"`
	Message string       `json:"message,omitempty"`
	Account *accountJSON `json:"account,omitempty"`
}

// ImportAccount 解析并保存单个账号文件内容；同 UID 覆盖旧文件。
func (e *Engine) ImportAccount(content string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	a, err := auth.Parse([]byte(content))
	if err != nil {
		return "", fmt.Errorf("账号文件解析失败: %w", err)
	}
	a.UID = strings.TrimSpace(a.UID)
	if a.UID == "" {
		return "", fmt.Errorf("缺少 uid，无法导入")
	}
	if !validUID(a.UID) {
		return "", fmt.Errorf("uid 含非法字符，无法导入")
	}
	if err := os.MkdirAll(e.dir, 0o700); err != nil {
		return "", fmt.Errorf("创建账号目录失败: %w", err)
	}
	a.FilePath = filepath.Join(e.dir, "workbuddy-"+a.UID+".json")
	if a.RealmStored() == "" {
		a.BackfillRealm()
	}
	if err := a.SaveAtomic(); err != nil {
		return "", fmt.Errorf("账号文件写入失败: %w", err)
	}
	states := e.loadStatesLocked()
	return marshal(accountOf(a, states[a.UID], e.today()))
}

// ListAccounts 返回全部账号的 JSON 数组，按文件名排序，并附带当日签到状态与最近积分快照。
func (e *Engine) ListAccounts() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	list, err := auth.LoadDir(e.dir)
	if err != nil {
		return "", fmt.Errorf("读取账号目录失败: %w", err)
	}
	states := e.loadStatesLocked()
	today := e.today()
	out := make([]accountJSON, 0, len(list))
	for _, a := range list {
		out = append(out, accountOf(a, states[a.UID], today))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return marshal(out)
}

func accountOf(a *auth.Auth, st persistedAccountState, today string) accountJSON {
	out := accountJSON{
		UID:             a.UID,
		Nickname:        a.Nickname,
		Realm:           a.Realm(),
		Enterprise:      a.IsEnterprise(),
		File:            filepath.Base(a.FilePath),
		HasCredits:      st.HasCredits,
		CreditsRemain:   st.CreditsRemain,
		CreditsTotal:    st.CreditsTotal,
		CreditsExpiring: st.CreditsExpiring,
		CreditsAt:       st.CreditsAt,
	}
	if st.LastCheckinDay == today && st.Status != "" {
		out.LastCheckin = &checkinResult{
			UID:             a.UID,
			Nickname:        a.Nickname,
			Status:          st.Status,
			Message:         st.Message,
			CheckedAt:       st.CheckedAt,
			HasCredits:      st.HasCredits,
			CreditsRemain:   st.CreditsRemain,
			CreditsTotal:    st.CreditsTotal,
			CreditsExpiring: st.CreditsExpiring,
			CreditsAt:       st.CreditsAt,
		}
	}
	return out
}

// validUID 与面板登录的字符白名单一致：字母/数字/连字符/下划线，长度 <= 64。
func validUID(uid string) bool {
	if uid == "" || len(uid) > 64 {
		return false
	}
	for _, c := range uid {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func marshal(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Checkin 对单号执行签到：企业号 / 国际版号本地跳过，其余调 DailyCheckin，
// 完成后顺带刷新积分快照并把今日签到状态持久化到本地 checkin_state.json。
func (e *Engine) Checkin(uid string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.testHook != nil {
		e.testHook()
	}

	a, err := e.load(uid)
	if err != nil {
		return "", err
	}
	now := e.nowTime()
	r := checkinResult{
		UID:       a.UID,
		Nickname:  a.Nickname,
		CheckedAt: now.Format(time.RFC3339),
	}
	switch {
	case a.IsEnterprise():
		r.Status, r.Message = "skipped", "企业账号无签到体系（已跳过）"
	case a.IsGlobal():
		r.Status, r.Message = "skipped", "国际版账号无签到体系（已跳过）"
	default:
		if st, msg, done := e.refreshIfNeeded(a); done {
			r.Status, r.Message = st, msg
		} else if err := e.up.DailyCheckin(a); err != nil {
			if upstream.IsAlreadyCheckin(err) {
				r.Status, r.Message = "already", "今天已签到"
			} else {
				r.Status, r.Message = "failed", checkinErrorMessage(err)
			}
		} else {
			r.Status, r.Message = "ok", "签到成功"
		}
	}

	states := e.loadStatesLocked()
	st := states[a.UID]
	st.LastCheckinDay = now.Format("2006-01-02")
	st.Status = r.Status
	st.Message = r.Message
	st.CheckedAt = r.CheckedAt

	if r.Status == "ok" || r.Status == "already" || r.Status == "skipped" {
		if err := e.populateCreditsLocked(a, &st, now); err != nil {
			r.CreditsError = short(err.Error())
		}
	}
	if st.HasCredits {
		r.HasCredits = true
		r.CreditsRemain = st.CreditsRemain
		r.CreditsTotal = st.CreditsTotal
		r.CreditsExpiring = st.CreditsExpiring
		r.CreditsAt = st.CreditsAt
	}
	states[a.UID] = st
	_ = e.saveStatesLocked(states)
	return marshal(r)
}

// RefreshCredits 单独刷新指定账号的积分快照并持久化，返回更新后的 Account JSON。
func (e *Engine) RefreshCredits(uid string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	a, err := e.load(uid)
	if err != nil {
		return "", err
	}
	if _, msg, done := e.refreshIfNeeded(a); done {
		return "", errors.New(msg)
	}
	now := e.nowTime()
	states := e.loadStatesLocked()
	st := states[a.UID]
	if err := e.populateCreditsLocked(a, &st, now); err != nil {
		return "", fmt.Errorf("查询积分失败：%s", short(err.Error()))
	}
	states[a.UID] = st
	if err := e.saveStatesLocked(states); err != nil {
		return "", fmt.Errorf("保存积分状态失败：%w", err)
	}
	return marshal(accountOf(a, st, now.Format("2006-01-02")))
}

func (e *Engine) populateCreditsLocked(a *auth.Auth, st *persistedAccountState, now time.Time) error {
	if _, msg, done := e.refreshIfNeeded(a); done {
		return errors.New(msg)
	}
	remain, total, expiring, _, _, _, err := e.up.UserResourceSnapshot(a, expiringSoonWindow)
	if err != nil {
		return err
	}
	st.HasCredits = true
	st.CreditsRemain = remain
	st.CreditsTotal = total
	st.CreditsExpiring = expiring
	st.CreditsAt = now.Format(time.RFC3339)
	return nil
}

// DeleteAccount 删除指定账号文件并清理其本地签到/积分状态。
func (e *Engine) DeleteAccount(uid string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	a, err := e.load(uid)
	if err != nil {
		return err
	}
	if err := os.Remove(a.FilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除账号文件失败: %w", err)
	}
	states := e.loadStatesLocked()
	if _, ok := states[uid]; ok {
		delete(states, uid)
		_ = e.saveStatesLocked(states)
	}
	return nil
}

// ExportAccount 读取并返回指定账号的原始 JSON 文件内容。
func (e *Engine) ExportAccount(uid string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	a, err := e.load(uid)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(a.FilePath)
	if err != nil {
		return "", fmt.Errorf("读取账号文件失败: %w", err)
	}
	return string(raw), nil
}

// StartLogin 发起 OAuth 设备授权流程，返回授权 URL 与 state（realm: "cn" 或 "global"）。
func (e *Engine) StartLogin(realm string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if realm != "global" {
		realm = "cn"
	}
	epState, _, _, origin := e.loginEndpoints(realm)
	data, status, err := e.doLoginJSON(http.MethodPost, epState, "", bytes.NewReader([]byte("{}")), origin)
	if err != nil {
		return "", fmt.Errorf("发起登录失败（HTTP %d）: %s", status, short(err.Error()))
	}
	var st struct {
		State   string `json:"state"`
		AuthURL string `json:"authUrl"`
	}
	if err := json.Unmarshal(data, &st); err != nil || st.State == "" || st.AuthURL == "" {
		return "", errors.New("发起登录失败：上游未返回 state 或授权链接")
	}
	now := e.nowTime()
	if e.logins == nil {
		e.logins = map[string]loginSession{}
	}
	for k, sess := range e.logins {
		if now.Sub(sess.created) > loginTTL {
			delete(e.logins, k)
		}
	}
	e.logins[st.State] = loginSession{created: now, realm: realm}
	return marshal(startLoginResult{
		State: st.State,
		URL:   st.AuthURL,
		Realm: realm,
	})
}

// PollLogin 轮询登录授权状态。未完成返回 done=false；完成后落盘账号文件并返回 Account。
func (e *Engine) PollLogin(state string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	state = strings.TrimSpace(state)
	if state == "" {
		return "", errors.New("缺少登录 state")
	}
	now := e.nowTime()
	sess, ok := e.logins[state]
	if !ok || now.Sub(sess.created) > loginTTL {
		delete(e.logins, state)
		return "", errors.New("登录会话已过期，请重新发起登录")
	}
	_, epToken, epAcct, origin := e.loginEndpoints(sess.realm)

	tokRaw, _, err := e.doLoginJSON(http.MethodGet, epToken+state, "", nil, origin)
	if err != nil {
		return marshal(pollLoginResult{Done: false, Message: short(err.Error())})
	}
	var tok struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	if err := json.Unmarshal(tokRaw, &tok); err != nil || tok.AccessToken == "" {
		return marshal(pollLoginResult{Done: false, Message: "等待授权完成"})
	}

	var acct struct {
		UID          string `json:"uid"`
		EnterpriseID string `json:"enterpriseId"`
		Nickname     string `json:"nickname"`
	}
	if acctRaw, _, err := e.doLoginJSON(http.MethodGet, epAcct+state, tok.AccessToken, nil, origin); err == nil {
		_ = json.Unmarshal(acctRaw, &acct)
	}
	if acct.UID == "" {
		return "", errors.New("授权成功但未获取到 uid，请重试")
	}
	if !validUID(acct.UID) {
		return "", errors.New("上游返回的 uid 含非法字符，拒绝保存")
	}
	if err := os.MkdirAll(e.dir, 0o700); err != nil {
		return "", fmt.Errorf("创建账号目录失败: %w", err)
	}

	a := &auth.Auth{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    now.Add(time.Duration(tok.ExpiresIn) * time.Second).Unix(),
		Domain:       tok.Domain,
		UID:          acct.UID,
		EnterpriseID: acct.EnterpriseID,
		Nickname:     acct.Nickname,
		FilePath:     filepath.Join(e.dir, "workbuddy-"+acct.UID+".json"),
	}
	if sess.realm == "global" {
		if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
			return "", fmt.Errorf("写入 realm 失败: %w", err)
		}
	} else {
		_, _ = a.BackfillRealm()
	}
	if err := a.SaveAtomic(); err != nil {
		return "", fmt.Errorf("保存账号文件失败: %w", err)
	}
	delete(e.logins, state)

	states := e.loadStatesLocked()
	acc := accountOf(a, states[a.UID], now.Format("2006-01-02"))
	return marshal(pollLoginResult{Done: true, Account: &acc})
}

func (e *Engine) loginEndpoints(realm string) (state, token, account, origin string) {
	if realm == "global" {
		base := e.loginBaseGlobal
		if base == "" {
			base = loginBaseGlobal
		}
		return base + "/v2/plugin/auth/state?platform=CLI",
			base + "/v2/plugin/auth/token?state=",
			base + "/v2/plugin/login/account?state=",
			loginOriginGlobal
	}
	base := e.loginBaseCN
	if base == "" {
		base = loginBaseCN
	}
	return base + "/v2/plugin/auth/state?platform=CLI",
		base + "/v2/plugin/auth/token?state=",
		base + "/v2/plugin/login/account?state=",
		loginOriginCN
}

type loginAPIEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (e *Engine) doLoginJSON(method, fullURL, bearer string, body io.Reader, origin string) (json.RawMessage, int, error) {
	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", origin+"/")
	req.Header.Set("User-Agent", loginClientUA)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := http.DefaultClient
	if e.up != nil && e.up.HTTP != nil {
		client = e.up.HTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	var env loginAPIEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parse failed: %w", err)
	}
	if env.Code != 0 {
		return nil, resp.StatusCode, fmt.Errorf("code=%d msg=%s", env.Code, env.Msg)
	}
	return env.Data, resp.StatusCode, nil
}

func (e *Engine) nowTime() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

func (e *Engine) today() string {
	return e.nowTime().Format("2006-01-02")
}

func (e *Engine) stateFilePath() string {
	return filepath.Join(e.dir, stateFileName)
}

func (e *Engine) loadStatesLocked() map[string]persistedAccountState {
	raw, err := os.ReadFile(e.stateFilePath())
	if err != nil {
		return map[string]persistedAccountState{}
	}
	var m map[string]persistedAccountState
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return map[string]persistedAccountState{}
	}
	return m
}

func (e *Engine) saveStatesLocked(states map[string]persistedAccountState) error {
	if err := os.MkdirAll(e.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(e.dir, ".checkin_state.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, e.stateFilePath()); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// refreshIfNeeded 在 token 临近过期时刷新并写回；done=true 表示不应继续签到。
func (e *Engine) refreshIfNeeded(a *auth.Auth) (status, message string, done bool) {
	if !a.NeedsRefresh(refreshWindow) {
		return "", "", false
	}
	if a.RefreshTokenValue() == "" {
		return "auth_invalid", "凭证已过期且无 refreshToken，请重新导入该账号文件", true
	}
	if err := e.up.RefreshToken(a); err != nil {
		if isSessionDead(err) {
			return "auth_invalid", "凭证已失效，请重新导入该账号文件", true
		}
		return "failed", "刷新凭证失败：" + short(err.Error()), true
	}
	if err := a.SaveAtomic(); err != nil {
		return "failed", "token 刷新成功但写回失败：" + short(err.Error()), true
	}
	return "", "", false
}

func isSessionDead(err error) bool {
	var ue *upstream.Error
	return errors.As(err, &ue) && ue.Kind == upstream.ErrSessionDead
}

func (e *Engine) load(uid string) (*auth.Auth, error) {
	list, err := auth.LoadDir(e.dir)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.UID == uid {
			return a, nil
		}
	}
	return nil, fmt.Errorf("account not found: %s", uid)
}

// short 把上游错误压成一行，按 rune 截断避免切坏中文；含省略号不超过 160 字符。
func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 160 {
		return string(r[:159]) + "…"
	}
	return s
}

// checkinErrorMessage 把签到错误压成用户可读文案：上游业务/HTTP 错误保留摘要，
// 传输层（DNS/连接/超时）错误统一加「网络错误：」前缀。
func checkinErrorMessage(err error) string {
	var ue *upstream.Error
	if errors.As(err, &ue) {
		return short(err.Error())
	}
	return "网络错误：" + short(err.Error())
}
