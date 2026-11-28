package mobile

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// testAuthDoc 生成与插件 OAuth 输出同形的账号文件内容。
func testAuthDoc(t *testing.T, uid, nick, realm, accessToken, refreshToken string, expiresAt int64, enterpriseID string) string {
	t.Helper()
	doc := map[string]any{
		"auth": map[string]any{
			"accessToken":  accessToken,
			"refreshToken": refreshToken,
			"expiresAt":    expiresAt,
			"domain":       "codebuddy.cn",
			"realm":        realm,
		},
		"account": map[string]any{
			"uid":          uid,
			"nickname":     nick,
			"enterpriseId": enterpriseID,
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal auth doc: %v", err)
	}
	return string(raw)
}

func TestImportAndList(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir)

	out, err := e.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "at-1", "rt-1", 9999999999, ""))
	if err != nil {
		t.Fatalf("import nested: %v", err)
	}
	var acc accountJSON
	if err := json.Unmarshal([]byte(out), &acc); err != nil {
		t.Fatalf("decode account json: %v", err)
	}
	if acc.UID != "u1" || acc.Nickname != "一号" || acc.Realm != "cn" || acc.Enterprise || acc.File != "workbuddy-u1.json" {
		t.Fatalf("unexpected account: %+v", acc)
	}

	flat := `{"accessToken":"at-2","refreshToken":"rt-2","expiresAt":9999999999,"uid":"u2","nickname":"二号"}`
	if _, err := e.ImportAccount(flat); err != nil {
		t.Fatalf("import flat: %v", err)
	}

	raw, err := e.ListAccounts()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list []accountJSON
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 || list[0].UID != "u1" || list[1].UID != "u2" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestImportRejectsInvalid(t *testing.T) {
	e := NewEngine(t.TempDir())
	cases := []string{
		`not json`,
		`{"auth":{"accessToken":""},"account":{"uid":"u1"}}`,
		`{"accessToken":"at","refreshToken":"rt"}`,
		`{"accessToken":"at","refreshToken":"rt","uid":"../x"}`,
		`{"accessToken":"at","refreshToken":"rt","uid":"` + strings.Repeat("a", 65) + `"}`,
	}
	for _, c := range cases {
		if _, err := e.ImportAccount(c); err == nil {
			t.Fatalf("want error for %s", c)
		}
	}

	if _, err := e.ImportAccount(`not json`); err == nil {
		t.Fatalf("want parse error for malformed json")
	} else if !strings.Contains(err.Error(), "账号文件解析失败") {
		t.Fatalf("parse error missing Chinese context: %v", err)
	}
}

func TestImportOverwritesSameUID(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir)
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "旧", "cn", "at-old", "rt-old", 9999999999, "")); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "新", "cn", "at-new", "rt-new", 9999999999, "")); err != nil {
		t.Fatalf("second import: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "workbuddy-u1.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var doc struct {
		Auth struct {
			AccessToken string `json:"accessToken"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode file: %v", err)
	}
	if doc.Auth.AccessToken != "at-new" {
		t.Fatalf("accessToken=%q want at-new", doc.Auth.AccessToken)
	}
}

type fakeUpstream struct {
	srv         *httptest.Server
	checkinHits atomic.Int32
	refreshHits atomic.Int32
	chatHits    atomic.Int32
	reportHits  atomic.Int32
	checkinBody string
	refreshCode int
	refreshBody string
	chatModel   string

	authMu   sync.Mutex
	lastAuth string
	lastUID  string
}

func (f *fakeUpstream) setLastAuth(v string) {
	f.authMu.Lock()
	f.lastAuth = v
	f.authMu.Unlock()
}

func (f *fakeUpstream) lastAuthValue() string {
	f.authMu.Lock()
	defer f.authMu.Unlock()
	return f.lastAuth
}

func (f *fakeUpstream) setLastUID(v string) {
	f.authMu.Lock()
	f.lastUID = v
	f.authMu.Unlock()
}

func (f *fakeUpstream) lastUIDValue() string {
	f.authMu.Lock()
	defer f.authMu.Unlock()
	return f.lastUID
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{
		checkinBody: `{"code":0,"msg":"ok","data":{}}`,
		refreshCode: http.StatusOK,
		chatModel:   "hy3",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/billing/meter/daily-checkin", func(w http.ResponseWriter, r *http.Request) {
		f.checkinHits.Add(1)
		f.setLastAuth(r.Header.Get("Authorization"))
		f.setLastUID(r.Header.Get("X-User-Id"))
		if got := r.Header.Get("X-CodeBuddy-Request"); got != "1" {
			t.Errorf("X-CodeBuddy-Request=%q want 1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, f.checkinBody)
	})
	mux.HandleFunc("/v2/plugin/auth/token/refresh", func(w http.ResponseWriter, r *http.Request) {
		f.refreshHits.Add(1)
		if got := r.Header.Get("X-Refresh-Token"); got == "" {
			t.Errorf("missing X-Refresh-Token")
		}
		w.Header().Set("Content-Type", "application/json")
		if f.refreshCode != http.StatusOK {
			w.WriteHeader(f.refreshCode)
		}
		_, _ = io.WriteString(w, f.refreshBody)
	})
	mux.HandleFunc("/v2/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		f.chatHits.Add(1)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != f.chatModel {
			t.Errorf("chat model=%q want %q", body.Model, f.chatModel)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\ndata: [DONE]\n\n")
	})
	mux.HandleFunc("/v2/report", func(w http.ResponseWriter, r *http.Request) {
		f.reportHits.Add(1)
		_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUpstream) engine(dir string) *Engine {
	up := upstream.New()
	up.HTTP = f.srv.Client()
	up.ChatBaseCN = f.srv.URL
	up.BillingBaseCN = f.srv.URL
	up.ChatBaseGlobal = f.srv.URL
	up.BillingBaseGlobal = f.srv.URL
	up.GlobalEnabled = true
	up.GlobalCheckinModel = upstream.DefaultGlobalCheckinModel
	return newEngineWith(dir, up)
}

func decodeResult(t *testing.T, raw string) checkinResult {
	t.Helper()
	var r checkinResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return r
}

func TestCheckinSuccess(t *testing.T) {
	f := newFakeUpstream(t)
	e := f.engine(t.TempDir())
	future := time.Now().Add(24 * time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "at-1", "rt-1", future, "")); err != nil {
		t.Fatalf("import: %v", err)
	}

	raw, err := e.Checkin("u1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "ok" || r.Message != "签到成功" {
		t.Fatalf("result=%+v", r)
	}
	if f.checkinHits.Load() != 1 || f.refreshHits.Load() != 0 {
		t.Fatalf("hits checkin=%d refresh=%d", f.checkinHits.Load(), f.refreshHits.Load())
	}
	if f.lastAuthValue() != "Bearer at-1" {
		t.Fatalf("authorization=%q", f.lastAuthValue())
	}
	if f.lastUIDValue() != "u1" {
		t.Fatalf("X-User-Id=%q want u1", f.lastUIDValue())
	}
}

func TestCheckinAlready(t *testing.T) {
	f := newFakeUpstream(t)
	f.checkinBody = `{"code":10001,"msg":"今天已签到","data":{}}`
	e := f.engine(t.TempDir())
	future := time.Now().Add(24 * time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "at-1", "rt-1", future, "")); err != nil {
		t.Fatalf("import: %v", err)
	}

	raw, err := e.Checkin("u1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "already" || r.Message != "今天已签到" {
		t.Fatalf("result=%+v", r)
	}
	if f.checkinHits.Load() != 1 {
		t.Fatalf("checkin hits=%d want 1", f.checkinHits.Load())
	}
}

func TestCheckinBusinessFailure(t *testing.T) {
	f := newFakeUpstream(t)
	f.checkinBody = `{"code":1,"msg":"服务开小差了","data":{}}`
	e := f.engine(t.TempDir())
	future := time.Now().Add(24 * time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "at-1", "rt-1", future, "")); err != nil {
		t.Fatalf("import: %v", err)
	}

	raw, err := e.Checkin("u1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "failed" || !strings.Contains(r.Message, "服务开小差了") {
		t.Fatalf("result=%+v", r)
	}
	if f.checkinHits.Load() != 1 {
		t.Fatalf("checkin hits=%d want 1", f.checkinHits.Load())
	}
}

func TestCheckinErrorMessage(t *testing.T) {
	if got := checkinErrorMessage(fmt.Errorf("dial tcp: i/o timeout")); !strings.HasPrefix(got, "网络错误：") {
		t.Fatalf("transport error=%q want 网络错误： prefix", got)
	}
	upErr := &upstream.Error{Kind: upstream.ErrServer, Status: 500, Msg: "boom"}
	got := checkinErrorMessage(upErr)
	if strings.HasPrefix(got, "网络错误：") {
		t.Fatalf("upstream error=%q should not carry network prefix", got)
	}
	if !strings.Contains(got, "boom") {
		t.Fatalf("upstream error=%q missing msg", got)
	}
}

func TestShortLimit(t *testing.T) {
	long := strings.Repeat("签", 200)
	got := short(long)
	if len([]rune(got)) != 160 {
		t.Fatalf("short rune len=%d want 160", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("short=%q missing ellipsis", got)
	}
}

func TestCheckinSkipsEnterpriseAndGlobal(t *testing.T) {
	f := newFakeUpstream(t)
	e := f.engine(t.TempDir())
	future := time.Now().Add(24 * time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "ent", "企业", "cn", "at-ent", "rt-ent", future, "ent-1")); err != nil {
		t.Fatalf("import enterprise: %v", err)
	}
	if _, err := e.ImportAccount(testAuthDoc(t, "glb", "国际", "global", "at-glb", "rt-glb", future, "")); err != nil {
		t.Fatalf("import global: %v", err)
	}

	raw, err := e.Checkin("ent")
	if err != nil {
		t.Fatalf("checkin ent: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "skipped" || r.Message != "企业账号无签到体系（已跳过）" {
		t.Fatalf("enterprise result=%+v", r)
	}
	if f.checkinHits.Load() != 0 || f.chatHits.Load() != 0 || f.refreshHits.Load() != 0 {
		t.Fatalf("enterprise should not hit upstream: checkin=%d chat=%d refresh=%d",
			f.checkinHits.Load(), f.chatHits.Load(), f.refreshHits.Load())
	}
}

func TestCheckinGlobalRealChatAndIdempotency(t *testing.T) {
	f := newFakeUpstream(t)
	dir := t.TempDir()
	e := f.engine(dir)
	future := time.Now().Add(24 * time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "glb", "国际", "global", "at-glb", "rt-glb", future, "")); err != nil {
		t.Fatalf("import global: %v", err)
	}
	if err := e.SetCheckinModel("global:hy4-preview-f"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	f.chatModel = "hy4-preview-f"

	raw, err := e.Checkin("glb")
	if err != nil {
		t.Fatalf("checkin global: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "ok" || !strings.Contains(r.Message, "国际版签到完成") {
		t.Fatalf("global result=%+v", r)
	}
	if f.chatHits.Load() != 1 || f.reportHits.Load() != 1 || f.checkinHits.Load() != 0 {
		t.Fatalf("global hits chat=%d report=%d cn-checkin=%d", f.chatHits.Load(), f.reportHits.Load(), f.checkinHits.Load())
	}
	// 第二次：同日幂等，不再发真实对话。
	raw2, err := e.Checkin("glb")
	if err != nil {
		t.Fatalf("second checkin: %v", err)
	}
	r2 := decodeResult(t, raw2)
	if r2.Status != "already" || f.chatHits.Load() != 1 {
		t.Fatalf("second result=%+v chatHits=%d", r2, f.chatHits.Load())
	}
	// 模型设置跨 Engine 实例持久化。
	if got := NewEngine(dir).GetCheckinModel(); got != "global:hy4-preview-f" {
		t.Fatalf("persisted model=%q", got)
	}
}

func TestGlobalCheckinFailureNotMarkedDone(t *testing.T) {
	f := newFakeUpstream(t)
	e := f.engine(t.TempDir())
	future := time.Now().Add(24 * time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "glb", "国际", "global", "at-glb", "rt-glb", future, "")); err != nil {
		t.Fatalf("import global: %v", err)
	}
	// fake chat 返回非 2xx：通过临时把 server 关掉模拟网络失败。
	f.srv.Close()
	raw, err := e.Checkin("glb")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "failed" {
		t.Fatalf("result=%+v want failed", r)
	}
	if e.isCheckinDoneToday("glb") {
		t.Fatal("failed checkin must not mark done")
	}
}

func TestCheckinUnknownUID(t *testing.T) {
	f := newFakeUpstream(t)
	e := f.engine(t.TempDir())
	if _, err := e.Checkin("missing"); err == nil {
		t.Fatalf("want error for missing uid")
	}
}

func TestCheckinRefreshesExpiringToken(t *testing.T) {
	f := newFakeUpstream(t)
	f.refreshBody = `{"code":0,"msg":"ok","data":{"accessToken":"new-at","refreshToken":"new-rt","expiresIn":5184000,"domain":"codebuddy.cn"}}`
	dir := t.TempDir()
	e := f.engine(dir)
	soon := time.Now().Add(time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "old-at", "old-rt", soon, "")); err != nil {
		t.Fatalf("import: %v", err)
	}

	raw, err := e.Checkin("u1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "ok" {
		t.Fatalf("result=%+v", r)
	}
	if f.refreshHits.Load() != 1 {
		t.Fatalf("refresh hits=%d want 1", f.refreshHits.Load())
	}
	if f.lastAuthValue() != "Bearer new-at" {
		t.Fatalf("checkin authorization=%q want Bearer new-at", f.lastAuthValue())
	}

	saved, err := os.ReadFile(filepath.Join(dir, "workbuddy-u1.json"))
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	a, err := auth.Parse(saved)
	if err != nil {
		t.Fatalf("parse saved: %v", err)
	}
	if a.AccessTokenValue() != "new-at" {
		t.Fatalf("saved accessToken=%q", a.AccessTokenValue())
	}
	if a.RefreshTokenValue() != "new-rt" {
		t.Fatalf("saved refreshToken=%q", a.RefreshTokenValue())
	}
}

func TestCheckinAuthInvalidOnDeadRefresh(t *testing.T) {
	f := newFakeUpstream(t)
	f.refreshCode = http.StatusUnauthorized
	f.refreshBody = `{"code":12153,"msg":"Offline user session not found"}`
	e := f.engine(t.TempDir())
	soon := time.Now().Add(time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "old-at", "old-rt", soon, "")); err != nil {
		t.Fatalf("import: %v", err)
	}

	raw, err := e.Checkin("u1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "auth_invalid" {
		t.Fatalf("result=%+v", r)
	}
	if f.checkinHits.Load() != 0 {
		t.Fatalf("checkin hits=%d want 0", f.checkinHits.Load())
	}
}

func TestCheckinAuthInvalidWithoutRefreshToken(t *testing.T) {
	f := newFakeUpstream(t)
	e := f.engine(t.TempDir())
	soon := time.Now().Add(time.Hour).Unix()
	doc := `{"auth":{"accessToken":"old-at","expiresAt":` + strconv.FormatInt(soon, 10) + `,"domain":"codebuddy.cn"},"account":{"uid":"u1","nickname":"一号"}}`
	if _, err := e.ImportAccount(doc); err != nil {
		t.Fatalf("import: %v", err)
	}

	raw, err := e.Checkin("u1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	r := decodeResult(t, raw)
	if r.Status != "auth_invalid" {
		t.Fatalf("result=%+v", r)
	}
	if f.refreshHits.Load() != 0 || f.checkinHits.Load() != 0 {
		t.Fatalf("unexpected hits refresh=%d checkin=%d", f.refreshHits.Load(), f.checkinHits.Load())
	}
}

func TestConcurrentCheckinSerialized(t *testing.T) {
	f := newFakeUpstream(t)
	e := f.engine(t.TempDir())
	future := time.Now().Add(24 * time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "at-1", "rt-1", future, "")); err != nil {
		t.Fatalf("import: %v", err)
	}

	// 钩子在持锁期间执行：若 Checkin 未串行化，两个 goroutine 会同时进入钩子，
	// maxActive 升至 2；sleep 扩大重叠窗口让竞争稳定复现。
	var (
		hookMu    sync.Mutex
		active    int
		maxActive int
	)
	e.testHook = func() {
		hookMu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		hookMu.Unlock()

		time.Sleep(50 * time.Millisecond)

		hookMu.Lock()
		active--
		hookMu.Unlock()
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			<-start
			if _, err := e.Checkin("u1"); err != nil {
				t.Errorf("checkin: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if maxActive != 1 {
		t.Fatalf("maxActive=%d want 1 (Checkin 未串行化)", maxActive)
	}
	if f.checkinHits.Load() != 2 {
		t.Fatalf("checkin hits=%d want 2", f.checkinHits.Load())
	}
}

func newFakeResourceAndLoginServer(t *testing.T, loginPollReady *atomic.Bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/billing/meter/daily-checkin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{}}`)
	})
	mux.HandleFunc("/v2/billing/meter/get-user-resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		expEnd := time.Now().Add(48 * time.Hour).Format("2006-01-02 15:04:05")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"code":0,"msg":"ok","data":{"Response":{"Data":{"Accounts":[{"PackageName":"free","CycleCapacitySize":500,"CycleCapacityRemain":320,"CycleCapacityUsed":180,"CycleEndTime":%q}]}}}}`, expEnd))
	})
	mux.HandleFunc("/v2/plugin/auth/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"state":"st-123","authUrl":"https://example.test/auth?state=st-123"}}`)
	})
	mux.HandleFunc("/v2/plugin/auth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if loginPollReady != nil && !loginPollReady.Load() {
			_, _ = io.WriteString(w, `{"code":10001,"msg":"login ing","data":null}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"accessToken":"at-login","refreshToken":"rt-login","expiresIn":7200,"domain":"www.codebuddy.cn"}}`)
	})
	mux.HandleFunc("/v2/plugin/login/account", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"uid":"login-u1","nickname":"扫码新号","enterpriseId":""}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestUpstream(srv *httptest.Server) *upstream.Client {
	up := upstream.New()
	up.HTTP = srv.Client()
	up.ChatBaseCN = srv.URL
	up.BillingBaseCN = srv.URL
	return up
}

func TestCheckinPersistsAcrossRestartAndExpiresNextDay(t *testing.T) {
	srv := newFakeResourceAndLoginServer(t, nil)
	dir := t.TempDir()
	day1 := time.Date(2026, 11, 27, 9, 0, 0, 0, time.Local)

	up := newTestUpstream(srv)
	e1 := newEngineWith(dir, up)
	e1.now = func() time.Time { return day1 }

	// 过期时间锚定真实时钟：NeedsRefresh 用 time.Now 判断，硬编码日期会随实际日期推移而失效。
	future := time.Now().Add(48 * time.Hour).Unix()
	if _, err := e1.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "at-1", "rt-1", future, "")); err != nil {
		t.Fatalf("import: %v", err)
	}

	raw, err := e1.Checkin("u1")
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	res := decodeResult(t, raw)
	if res.Status != "ok" || !res.HasCredits || res.CreditsRemain != 320 || res.CreditsTotal != 500 || res.CreditsExpiring != 320 {
		t.Fatalf("unexpected checkin result: %+v", res)
	}

	// 模拟 App 重启：新建 Engine 实例，仍在同一天，ListAccounts 必须带上当日已签状态与积分。
	e2 := newEngineWith(dir, up)
	e2.now = func() time.Time { return day1.Add(3 * time.Hour) }
	listRaw, err := e2.ListAccounts()
	if err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	var list []accountJSON
	if err := json.Unmarshal([]byte(listRaw), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].LastCheckin == nil || list[0].LastCheckin.Status != "ok" {
		t.Fatalf("restart lost today's checkin status: %+v", list)
	}
	if !list[0].HasCredits || list[0].CreditsRemain != 320 {
		t.Fatalf("restart lost credits: %+v", list[0])
	}

	// 跨零点到次日：LastCheckin 自然失效为 nil（待签到），积分快照保留。
	e3 := newEngineWith(dir, up)
	e3.now = func() time.Time { return day1.Add(24 * time.Hour) }
	listRaw, err = e3.ListAccounts()
	if err != nil {
		t.Fatalf("list next day: %v", err)
	}
	list = nil
	if err := json.Unmarshal([]byte(listRaw), &list); err != nil {
		t.Fatalf("decode next day list: %v", err)
	}
	if len(list) != 1 || list[0].LastCheckin != nil {
		t.Fatalf("next day should clear LastCheckin, got: %+v", list)
	}
	if !list[0].HasCredits || list[0].CreditsRemain != 320 {
		t.Fatalf("next day should keep cached credits: %+v", list[0])
	}
}

func TestRefreshCreditsPreservesCheckinStatus(t *testing.T) {
	srv := newFakeResourceAndLoginServer(t, nil)
	dir := t.TempDir()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.Local)
	up := newTestUpstream(srv)
	e := newEngineWith(dir, up)
	e.now = func() time.Time { return now }

	future := time.Now().Add(48 * time.Hour).Unix()
	if _, err := e.ImportAccount(testAuthDoc(t, "u1", "一号", "cn", "at-1", "rt-1", future, "")); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := e.Checkin("u1"); err != nil {
		t.Fatalf("checkin: %v", err)
	}
	accRaw, err := e.RefreshCredits("u1")
	if err != nil {
		t.Fatalf("RefreshCredits: %v", err)
	}
	var acc accountJSON
	if err := json.Unmarshal([]byte(accRaw), &acc); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if !acc.HasCredits || acc.CreditsRemain != 320 || acc.LastCheckin == nil || acc.LastCheckin.Status != "ok" {
		t.Fatalf("unexpected account after RefreshCredits: %+v", acc)
	}
}

func TestDeleteAndExportAccount(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir)
	doc := testAuthDoc(t, "u1", "一号", "cn", "at-1", "rt-1", 9999999999, "")
	if _, err := e.ImportAccount(doc); err != nil {
		t.Fatalf("import: %v", err)
	}
	exported, err := e.ExportAccount("u1")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	parsed, err := auth.Parse([]byte(exported))
	if err != nil || parsed.UID != "u1" || parsed.AccessToken != "at-1" {
		t.Fatalf("exported json invalid: %v %+v", err, parsed)
	}

	if err := e.DeleteAccount("u1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := e.ExportAccount("u1"); err == nil {
		t.Fatal("expected export after delete to fail")
	}
	listRaw, err := e.ListAccounts()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list []accountJSON
	if err := json.Unmarshal([]byte(listRaw), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list after delete, got %+v", list)
	}
}

func TestStartAndPollLogin(t *testing.T) {
	var ready atomic.Bool
	srv := newFakeResourceAndLoginServer(t, &ready)
	dir := t.TempDir()
	up := newTestUpstream(srv)
	e := newEngineWith(dir, up)
	e.loginBaseCN = srv.URL
	e.loginBaseGlobal = srv.URL

	startRaw, err := e.StartLogin("cn")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	var startRes startLoginResult
	if err := json.Unmarshal([]byte(startRaw), &startRes); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if startRes.State != "st-123" || startRes.Realm != "cn" || !strings.Contains(startRes.URL, "st-123") {
		t.Fatalf("unexpected start result: %+v", startRes)
	}

	pollRaw, err := e.PollLogin("st-123")
	if err != nil {
		t.Fatalf("PollLogin pending: %v", err)
	}
	var poll1 pollLoginResult
	if err := json.Unmarshal([]byte(pollRaw), &poll1); err != nil || poll1.Done {
		t.Fatalf("expected pending poll, got err=%v res=%+v", err, poll1)
	}

	ready.Store(true)
	pollRaw2, err := e.PollLogin("st-123")
	if err != nil {
		t.Fatalf("PollLogin done: %v", err)
	}
	var poll2 pollLoginResult
	if err := json.Unmarshal([]byte(pollRaw2), &poll2); err != nil || !poll2.Done || poll2.Account == nil {
		t.Fatalf("expected completed poll, got err=%v res=%+v", err, poll2)
	}
	if poll2.Account.UID != "login-u1" || poll2.Account.Nickname != "扫码新号" || poll2.Account.Realm != "cn" {
		t.Fatalf("unexpected logged-in account: %+v", poll2.Account)
	}

	// 完成后 state 应被消费，再次 poll 报错
	if _, err := e.PollLogin("st-123"); err == nil {
		t.Fatal("expected reused state to fail")
	}
}
