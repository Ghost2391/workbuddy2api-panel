package upstream

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

func TestGlobalCheckinChatUsesNewConversationAndModel(t *testing.T) {
	oldGap := globalCheckinReportGap
	globalCheckinReportGap = 0
	defer func() { globalCheckinReportGap = oldGap }()

	var chatHits, reportHits atomic.Int32
	var chatBody, reportBody []byte
	var chatCID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/chat/completions":
			chatHits.Add(1)
			chatCID = r.Header.Get("X-Conversation-ID")
			chatBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\ndata: [DONE]\n\n")
		case "/v2/report":
			reportHits.Add(1)
			reportBody, _ = io.ReadAll(r.Body)
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := &auth.Auth{AccessToken: "at", RefreshToken: "rt", UID: "u1"}
	if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
		t.Fatalf("set realm: %v", err)
	}
	c := &Client{
		HTTP:               srv.Client(),
		GlobalEnabled:      true,
		ChatBaseCN:         srv.URL,
		BillingBaseCN:      srv.URL,
		ChatBaseGlobal:     srv.URL,
		BillingBaseGlobal:  srv.URL,
		GlobalCheckinModel: "global:hy3",
	}

	cid, err := c.GlobalCheckinChat(a)
	if err != nil {
		t.Fatalf("GlobalCheckinChat: %v", err)
	}
	if !strings.HasPrefix(cid, "wb2api-global-checkin-") {
		t.Fatalf("cid=%q", cid)
	}
	if chatHits.Load() != 1 || reportHits.Load() != 1 {
		t.Fatalf("chat hits=%d report hits=%d want 1/1", chatHits.Load(), reportHits.Load())
	}
	if chatCID != cid {
		t.Fatalf("X-Conversation-ID=%q want %q", chatCID, cid)
	}

	var chat map[string]any
	if err := json.Unmarshal(chatBody, &chat); err != nil {
		t.Fatalf("chat body json: %v", err)
	}
	if got := chat["model"]; got != "hy3" {
		t.Fatalf("chat model=%v want hy3（global: 前缀应剥离）", got)
	}
	report := string(reportBody)
	for _, want := range []string{
		`"eventCode":"chat_request_send"`,
		`"conversationId":"` + cid + `"`,
		`"requestId":"` + cid + `"`,
		`"requestModelId":"hy3"`,
		`"userId":"u1"`,
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report body missing %s:\n%s", want, report)
		}
	}
}

func TestGlobalCheckinRejectsCN(t *testing.T) {
	c := &Client{}
	a := &auth.Auth{AccessToken: "at", UID: "u2"}
	if _, err := c.GlobalCheckinChat(a); err == nil {
		t.Fatal("CN account must be rejected")
	}
}

func TestGlobalCheckinModelNameAndPrefix(t *testing.T) {
	c := &Client{}
	if got := c.GlobalCheckinModelName(); got != DefaultGlobalCheckinModel {
		t.Fatalf("default=%q want %q", got, DefaultGlobalCheckinModel)
	}
	c.GlobalCheckinModel = " global:hy4-preview-f "
	if got := c.GlobalCheckinModelName(); got != "global:hy4-preview-f" {
		t.Fatalf("configured=%q", got)
	}
	if got := globalCheckinUpstreamModel("global:hy4-preview-f"); got != "hy4-preview-f" {
		t.Fatalf("upstream model=%q want hy4-preview-f", got)
	}
	if got := globalCheckinUpstreamModel("cn:hy3"); got != "hy3" {
		t.Fatalf("cn prefix strip=%q want hy3", got)
	}
}

func TestGlobalCheckinChatFailureDoesNotReport(t *testing.T) {
	oldGap := globalCheckinReportGap
	globalCheckinReportGap = 0
	defer func() { globalCheckinReportGap = oldGap }()

	var reportHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/chat/completions":
			http.Error(w, `{"code":500,"msg":"chat down"}`, http.StatusInternalServerError)
		case "/v2/report":
			reportHits.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := &auth.Auth{AccessToken: "at", RefreshToken: "rt", UID: "u1"}
	if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
		t.Fatalf("set realm: %v", err)
	}
	c := &Client{
		HTTP:               srv.Client(),
		GlobalEnabled:      true,
		ChatBaseGlobal:     srv.URL,
		BillingBaseGlobal:  srv.URL,
		GlobalCheckinModel: "global:hy3",
	}

	if _, err := c.GlobalCheckinChat(a); err == nil {
		t.Fatal("chat failure must return an error")
	}
	if reportHits.Load() != 0 {
		t.Fatalf("chat failure must not send report (hits=%d)", reportHits.Load())
	}
}

func TestGlobalCheckinReportFailureReturnsError(t *testing.T) {
	oldGap := globalCheckinReportGap
	globalCheckinReportGap = 0
	defer func() { globalCheckinReportGap = oldGap }()

	var reportHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\ndata: [DONE]\n\n")
		case "/v2/report":
			reportHits.Add(1)
			http.Error(w, `{"code":500,"msg":"report down"}`, http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := &auth.Auth{AccessToken: "at", RefreshToken: "rt", UID: "u1"}
	if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
		t.Fatalf("set realm: %v", err)
	}
	c := &Client{
		HTTP:               srv.Client(),
		GlobalEnabled:      true,
		ChatBaseGlobal:     srv.URL,
		BillingBaseGlobal:  srv.URL,
		GlobalCheckinModel: "global:hy3",
	}

	if _, err := c.GlobalCheckinChat(a); err == nil {
		t.Fatal("report failure must return an error")
	}
	if reportHits.Load() != 1 {
		t.Fatalf("report hits=%d want 1", reportHits.Load())
	}
}
