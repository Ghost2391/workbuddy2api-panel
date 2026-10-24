package main

import (
	"testing"
	"time"
)

// blockingWriter 模拟 Windows 控制台 QuickEdit 挂起：Write 一直阻塞到 release 关闭。
type blockingWriter struct{ release chan struct{} }

func (b *blockingWriter) Write(p []byte) (int, error) {
	<-b.release
	return len(p), nil
}

// TestAsyncLogWriterNeverBlocks 回归 Windows QuickEdit 事故：控制台 stdout/stderr
// 被挂起时，日志写入必须立即返回。此前用 io.MultiWriter(os.Stderr, ...) 同步写，
// 一条日志就能把持有 pool 锁的请求链路卡死（症状：页面请求偶尔挂住，回终端按
// 回车才恢复）。本用例用「Write 永久阻塞」的 sink 复现该形态。
func TestAsyncLogWriterNeverBlocks(t *testing.T) {
	bw := &blockingWriter{release: make(chan struct{})}
	defer close(bw.release)
	aw := newAsyncLogWriter(bw, 4)

	done := make(chan struct{})
	go func() {
		// 写 200 行，远超队列容量 4：靠「满了就丢」保证不阻塞。
		for i := 0; i < 200; i++ {
			n, err := aw.Write([]byte("a log line\n"))
			if err != nil || n != len("a log line\n") {
				t.Errorf("Write 应恒成功并为满写，got n=%d err=%v", n, err)
				break
			}
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write 被阻塞：控制台挂起时日志不得阻塞业务")
	}

	if got := aw.Dropped(); got == 0 {
		t.Errorf("队列容量 4 写 200 行应有丢弃，Dropped=%d", got)
	}
}

// TestAsyncLogWriterCopiesBuffer 日志包会复用缓冲区，异步消费时必须自己拷一份，
// 否则控制台恢复后打出来的是被后续日志覆盖过的内容。
func TestAsyncLogWriterCopiesBuffer(t *testing.T) {
	got := make(chan string, 1)
	aw := newAsyncLogWriter(writerFunc(func(p []byte) (int, error) {
		got <- string(p)
		return len(p), nil
	}), 4)

	buf := []byte("first\n")
	if _, err := aw.Write(buf); err != nil {
		t.Fatal(err)
	}
	copy(buf, "XXXXX\n") // 模拟调用方复用缓冲区

	select {
	case s := <-got:
		if s != "first\n" {
			t.Errorf("写入内容被后续改写污染：got %q want %q", s, "first\n")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("后台消费未收到数据")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
