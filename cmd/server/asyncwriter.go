// asyncwriter.go 非阻塞的日志写入器（Windows 控制台 QuickEdit 的防线）。
//
// 问题：Windows 控制台开启「快速编辑模式」时，只要用户在窗口里点一下/拖选文本，
// 控制台就进入选择态，**stdout/stderr 的写入会被内核挂起**，直到按回车或 Esc 退出。
// 服务端日志是同步写的（io.MultiWriter(os.Stderr, 面板环缓冲)），一旦控制台挂起：
//   - 写日志的 goroutine 阻塞；
//   - 如果它正持有 pool 锁 / 处在请求链路里，整条请求链路跟着卡住；
//   - 用户感受是「页面请求偶尔卡死，回终端按一下回车就好了」。
//
// 解法：控制台这一路改成异步 + 有界队列 + 满了就丢。日志永远不能阻塞业务，
// 丢几行日志远好于卡住请求。面板自身的环缓冲（pn.Logs()）仍是同步写的，
// 所以面板「运行日志」页不会因为控制台挂起而缺行。
package main

import (
	"io"
	"sync/atomic"
)

// asyncLogWriter 把写入转交给单一后台 goroutine，调用方立即返回。
//
// 语义：
//   - 队列容量满时**丢弃**该行（不阻塞、不排队扩张）——日志是观测手段，
//     不能反过来影响被观测的业务；
//   - Write 恒返回 (len(p), nil)：满足 io.Writer 契约，且 MultiWriter
//     不会因「短写/错误」中断后续 sink（面板环缓冲照常收到）；
//   - 丢弃计数用 atomic 记录，运行期零开销读取，供需要时排查日志缺失。
type asyncLogWriter struct {
	ch   chan []byte
	drop atomic.Uint64
}

// newAsyncLogWriter 创建异步写入器并启动后台消费 goroutine。
// size 为待写队列容量（行数）：512 行足够吸收一次突发（如整池余额刷新），
// 又不至于在控制台长时间挂起时把内存堆起来。
func newAsyncLogWriter(w io.Writer, size int) *asyncLogWriter {
	if size <= 0 {
		size = 512
	}
	a := &asyncLogWriter{ch: make(chan []byte, size)}
	go func() {
		for b := range a.ch {
			_, _ = w.Write(b)
		}
	}()
	return a
}

// Write 复制一份数据入队后立即返回；队列满则丢弃并计数。
// 必须复制：调用方（log 包）会复用缓冲区，异步消费时原切片可能已被改写。
func (a *asyncLogWriter) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case a.ch <- buf:
	default:
		a.drop.Add(1)
	}
	return len(p), nil
}

// Dropped 返回因队列满被丢弃的行数（排查「日志少了几行」时用）。
func (a *asyncLogWriter) Dropped() uint64 { return a.drop.Load() }
