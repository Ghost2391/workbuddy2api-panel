package upstream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// TestClaimRewardWebEndpoint 领奖走 Web 域（workbuddy.cn）、任务码在路径里、无 body。
// 这是与 CLI 域（copilot.tencent.com/v2/.../reward/claim，task_code 在 body）的关键区别——
// 后者路径不存在，曾导致长期 400 "task not completed" 误判为"上游不支持领取"。
func TestClaimRewardWebEndpoint(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	var gotPlatform, gotReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotPlatform = r.Header.Get("x-client-platform")
		gotReferer = r.Header.Get("Referer")
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Write([]byte(`{"code":0,"msg":"OK","data":{"already_claimed":false,"credit":100,"energy":5}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL, WebBaseCN: srv.URL}
	a := &auth.Auth{AccessToken: "at", UID: "u1"}

	credit, energy, err := c.ClaimReward(a, "Model_chat_GLM5.2")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if credit != 100 || energy != 5 {
		t.Errorf("credit/energy = %d/%d, want 100/5", credit, energy)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%s want POST", gotMethod)
	}
	if want := "/activity/growth/tasks/Model_chat_GLM5.2/claim"; gotPath != want {
		t.Errorf("path=%q want %q（任务码必须在路径里）", gotPath, want)
	}
	if gotBody != "" {
		t.Errorf("claim 不应携带 body，got %q", gotBody)
	}
	if gotPlatform != "web" {
		t.Errorf("x-client-platform=%q want web", gotPlatform)
	}
	if want := srv.URL + "/profile/growth-center"; gotReferer != want {
		t.Errorf("Referer=%q want %q（应与 base 同域）", gotReferer, want)
	}
}

// TestClaimRewardAlreadyClaimed 重复领取：上游返回 already_claimed=true，
// 本地应视为"无新增奖励但不报错"（幂等语义）。
func TestClaimRewardAlreadyClaimed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"msg":"OK","data":{"already_claimed":true}}`))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), WebBaseCN: srv.URL}
	credit, energy, err := c.ClaimReward(&auth.Auth{AccessToken: "at", UID: "u1"}, "chat_5")
	if err != nil {
		t.Fatalf("already_claimed should not error: %v", err)
	}
	if credit != 0 || energy != 0 {
		t.Errorf("already claimed should yield 0/0, got %d/%d", credit, energy)
	}
}

// TestClaimRewardNotCompleted 未达标：上游 400 + task not completed 应作为错误透出。
func TestClaimRewardNotCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"code": 400, "msg": "task not completed"})
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), WebBaseCN: srv.URL}
	if _, _, err := c.ClaimReward(&auth.Auth{AccessToken: "at", UID: "u1"}, "chat_5"); err == nil {
		t.Fatal("want error for not-completed task")
	}
}

// TestClaimRewardGlobalRealm 国际版领奖：base 切 workbuddy.ai，Origin/Referer 同域
// （此前写死 workbuddy.cn，国际版请求会带与 base 不一致的来源域）。
func TestClaimRewardGlobalRealm(t *testing.T) {
	var gotPath, gotOrigin, gotReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotOrigin = r.Header.Get("Origin")
		gotReferer = r.Header.Get("Referer")
		w.Write([]byte(`{"code":0,"msg":"OK","data":{"already_claimed":false,"credit":50,"energy":0}}`))
	}))
	defer srv.Close()

	a := &auth.Auth{AccessToken: "at", UID: "u1"}
	if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
		t.Fatalf("BackfillRealmFor: %v", err)
	}
	c := &Client{HTTP: srv.Client(), GlobalEnabled: true, WebBaseGlobal: srv.URL}
	credit, _, err := c.ClaimReward(a, "first_chat")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if credit != 50 {
		t.Errorf("credit=%d want 50", credit)
	}
	if want := "/activity/growth/tasks/first_chat/claim"; gotPath != want {
		t.Errorf("path=%q want %q", gotPath, want)
	}
	if gotOrigin != srv.URL {
		t.Errorf("Origin=%q want %q（应与 base 同域）", gotOrigin, srv.URL)
	}
	if want := srv.URL + "/profile/growth-center"; gotReferer != want {
		t.Errorf("Referer=%q want %q", gotReferer, want)
	}
}

// TestParseGrowthTasksGlobalCodeField 国际版任务码字段名是 code（CN 是 task_code）。
// 只读 task_code 会让 global 任务全部变成空码——面板扫描看不到、自动动作匹配不上。
// 本用例同时钉住 CN 形态不受影响（task_code 优先）。
func TestParseGrowthTasksGlobalCodeField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"msg":"OK","data":{"tasks":[
			{"task_id":1,"code":"first_chat","title":"完成一次对话","status":"available"},
			{"task_code":"chat_5","title":"累计5次对话","status":"available"}
		]}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	tasks, err := c.ListTasks(&auth.Auth{AccessToken: "at", UID: "u1"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks=%d want 2", len(tasks))
	}
	if tasks[0].TaskCode != "first_chat" {
		t.Errorf("global code=%q want first_chat（code 字段必须回落）", tasks[0].TaskCode)
	}
	if tasks[1].TaskCode != "chat_5" {
		t.Errorf("cn task_code=%q want chat_5（task_code 优先）", tasks[1].TaskCode)
	}
}
