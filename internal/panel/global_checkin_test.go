package panel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func TestAccountCheckinGlobalUsesRealChatAndIsIdempotent(t *testing.T) {
	var chatHits, reportHits, dailyHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/chat/completions":
			chatHits.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\ndata: [DONE]\n\n")
		case "/v2/report":
			reportHits.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		case "/billing/meter/get-user-resource", "/v2/billing/meter/get-user-resource":
			_, _ = io.WriteString(w, `{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CycleCapacitySize":100,"CycleCapacityRemain":90,"CycleCapacityUsed":10}]}}}}`)
		case "/billing/meter/daily-checkin", "/v2/billing/meter/daily-checkin":
			dailyHits.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := &auth.Auth{AccessToken: "at", RefreshToken: "rt", UID: "g1"}
	if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
		t.Fatalf("set realm: %v", err)
	}
	p := pool.New("")
	p.Add(a)
	up := &upstream.Client{
		HTTP:               srv.Client(),
		GlobalEnabled:      true,
		ChatBaseCN:         srv.URL,
		BillingBaseCN:      srv.URL,
		ChatBaseGlobal:     srv.URL,
		BillingBaseGlobal:  srv.URL,
		GlobalCheckinModel: "global:hy3",
	}
	panel := &Panel{cfg: Config{Pool: p, Upstream: up}}

	req := httptest.NewRequest(http.MethodPost, "/panel/api/accounts/g1/checkin", nil)
	req.SetPathValue("uid", "g1")
	rec := httptest.NewRecorder()
	panel.accountCheckin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"checkin_done":true`) {
		t.Fatalf("body=%s", body)
	}
	if !strings.Contains(body, "国际版签到完成") {
		t.Fatalf("body=%s", body)
	}
	if chatHits.Load() != 1 || reportHits.Load() != 1 {
		t.Fatalf("chat=%d report=%d want 1/1", chatHits.Load(), reportHits.Load())
	}
	if dailyHits.Load() != 0 {
		t.Fatalf("国际版签到不应调用 CN daily-checkin（hits=%d）", dailyHits.Load())
	}
	if st, ok := p.Status("g1"); !ok || !st.CheckinDone {
		t.Fatalf("CheckinDone=%v ok=%v", st.CheckinDone, ok)
	}

	// 同日再次点击：幂等，不应再发真实对话。
	rec2 := httptest.NewRecorder()
	panel.accountCheckin(rec2, req)
	if chatHits.Load() != 1 {
		t.Fatalf("第二次签到又发了对话（chat=%d）", chatHits.Load())
	}
	if !strings.Contains(rec2.Body.String(), `"checkin_done":true`) {
		t.Fatalf("second body=%s", rec2.Body.String())
	}
}

func TestAccountCheckinGlobalReportFailureDoesNotMarkDone(t *testing.T) {
	var chatHits, reportHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/chat/completions":
			chatHits.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\ndata: [DONE]\n\n")
		case "/v2/report":
			reportHits.Add(1)
			http.Error(w, `{"code":500,"msg":"report down"}`, http.StatusInternalServerError)
		case "/billing/meter/get-user-resource", "/v2/billing/meter/get-user-resource":
			_, _ = io.WriteString(w, `{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CycleCapacitySize":100,"CycleCapacityRemain":90,"CycleCapacityUsed":10}]}}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := &auth.Auth{AccessToken: "at", RefreshToken: "rt", UID: "g2"}
	if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
		t.Fatalf("set realm: %v", err)
	}
	p := pool.New("")
	p.Add(a)
	up := &upstream.Client{
		HTTP:               srv.Client(),
		GlobalEnabled:      true,
		ChatBaseGlobal:     srv.URL,
		BillingBaseGlobal:  srv.URL,
		GlobalCheckinModel: "global:hy3",
	}
	panel := &Panel{cfg: Config{Pool: p, Upstream: up}}

	req := httptest.NewRequest(http.MethodPost, "/panel/api/accounts/g2/checkin", nil)
	req.SetPathValue("uid", "g2")
	rec := httptest.NewRecorder()
	panel.accountCheckin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `"checkin_done":true`) {
		t.Fatalf("report failure must not mark checkin done: %s", body)
	}
	if !strings.Contains(body, "国际版签到失败") {
		t.Fatalf("body=%s", body)
	}
	if st, ok := p.Status("g2"); !ok || st.CheckinDone {
		t.Fatalf("CheckinDone=%v ok=%v want false", st.CheckinDone, ok)
	}
	if chatHits.Load() != 1 || reportHits.Load() != 1 {
		t.Fatalf("chat=%d report=%d want 1/1", chatHits.Load(), reportHits.Load())
	}
}
