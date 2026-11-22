// global_checkin.go 国际版手动签到：用可配置的免费 hy 模型完成一次真实对话，
// 再上报同一会话的 chat_request_send 事件。
//
// 背景：国际版没有 CN 的 POST /billing/meter/daily-checkin 签到接口，奖励口径是
// 「网页完成一次对话 → 次日到账 30 积分」。因此国际版签到不走 CN 签到端点，而是
// 直接调用 /v2/chat/completions 做一次极短的真实对话，并按客户端事件形状上报
// chat_request_send（与 CN 的 runModelChat 同款「真实对话 + 对齐模型上报」）。
//
// 该动作只由面板「签到」/「全量签到」与 cmd/signin 手动触发，**不进定时任务**
// （scheduler.RunCheckinNow 仍跳过 global，见 scheduler.go 的 D4 门控）。
package upstream

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// DefaultGlobalCheckinModel 国际版签到默认模型（global 侧免费 hy3）。
// 允许用户写成 global:hy3 / cn:xxx 的网关路由形态；出站前会剥掉路由前缀。
const DefaultGlobalCheckinModel = "global:hy3"

// globalCheckinReportGap 真实对话与事件上报之间的间隔（对齐客户端事件节奏，测试可置 0）。
var globalCheckinReportGap = 1050 * time.Millisecond

// GlobalCheckinModelName 返回生效的国际版签到模型：配置值优先，空则回落默认。
func (c *Client) GlobalCheckinModelName() string {
	if c != nil {
		if m := strings.TrimSpace(c.GlobalCheckinModel); m != "" {
			return m
		}
	}
	return DefaultGlobalCheckinModel
}

// globalCheckinUpstreamModel 把网关路由形态（global:hy3 / cn:xxx）归一化为上游模型 ID。
func globalCheckinUpstreamModel(model string) string {
	m := strings.TrimSpace(model)
	m = strings.TrimPrefix(m, "global:")
	m = strings.TrimPrefix(m, "cn:")
	return strings.TrimSpace(m)
}

// GlobalCheckinChat 国际版签到：用配置的 hy 模型完成一次真实对话，再上报
// 同一 conversationId/模型的 chat_request_send。仅 global 账号可调；返回会话 id。
//
// 失败语义：对话或上报任一失败都返回错误，调用方不应标记「今日已签到」。
// 对话与上报之间保留 ~1s 间隔，避免上游把两条事件判为连发。
func (c *Client) GlobalCheckinChat(a *auth.Auth) (string, error) {
	if a == nil || a.Realm() != "global" {
		return "", fmt.Errorf("global checkin: only global accounts")
	}
	model := c.GlobalCheckinModelName()
	upstreamModel := globalCheckinUpstreamModel(model)
	if upstreamModel == "" {
		return "", fmt.Errorf("global checkin: empty model")
	}
	cid := fmt.Sprintf("wb2api-global-checkin-%d", time.Now().UnixMilli())
	body, err := json.Marshal(map[string]any{
		"model":    upstreamModel,
		"messages": []map[string]any{{"role": "user", "content": "你好，请只回复：好"}},
		"stream":   true,
	})
	if err != nil {
		return cid, err
	}
	rc, status, respBody, err := c.ChatStream(a, body, "", ChatMeta{
		ConversationID:        cid,
		ConversationRequestID: cid,
		TraceID:               cid,
	})
	if err != nil || status >= 400 {
		if rc != nil {
			rc.Close()
		}
		return cid, fmt.Errorf("global checkin chat: http=%d err=%v body=%.160s", status, err, respBody)
	}
	// 读干 SSE：网关对上游强制 flow，不读完会残留连接。
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, 1<<20))
	rc.Close()

	if globalCheckinReportGap > 0 {
		time.Sleep(globalCheckinReportGap)
	}
	if err := c.ReportChatActivityModel(a, cid, cid, upstreamModel, upstreamModel); err != nil {
		return cid, fmt.Errorf("global checkin report: %w", err)
	}
	return cid, nil
}
