package engine

import (
	"context"
	"io"
	"sync"
	"time"
)

const readChunkSize = 32 << 10 // 限速粒度：单次读取的配额块大小

// rateLimiter 令牌式限速器：配额随时间匀速累积。
// 用于全局限速；每任务限速也复用同一实现（limitedReader.extra）。
// limit <= 0 表示不限速，Wait 立即返回。
type rateLimiter struct {
	mu     sync.Mutex
	limit  int64 // 字节/秒
	credit float64
	last   time.Time
}

func newRateLimiter(limit int64) *rateLimiter {
	return &rateLimiter{limit: limit, last: time.Now()}
}

// SetLimit 立即调整限速并清空已累积配额。
func (l *rateLimiter) SetLimit(limit int64) {
	l.mu.Lock()
	l.limit = limit
	l.credit = 0
	l.last = time.Now()
	l.mu.Unlock()
}

// Wait 阻塞直到获得 n 字节的下载配额。
func (l *rateLimiter) Wait(n int64) {
	if n <= 0 {
		return
	}
	for {
		l.mu.Lock()
		if l.limit <= 0 {
			l.mu.Unlock()
			return
		}
		now := time.Now()
		l.credit += now.Sub(l.last).Seconds() * float64(l.limit)
		// 突发上限默认 1 秒配额；当 n 本身超过 limit（低速限 + 大读块）时
		// 必须允许累积到 n，否则 credit 永远够不着 n 而空转。
		maxCredit := float64(l.limit)
		if maxCredit < float64(n) {
			maxCredit = float64(n)
		}
		if l.credit > maxCredit {
			l.credit = maxCredit
		}
		l.last = now
		if l.credit >= float64(n) {
			l.credit -= float64(n)
			l.mu.Unlock()
			return
		}
		need := time.Duration((float64(n) - l.credit) / float64(l.limit) * float64(time.Second))
		l.mu.Unlock()
		if need > 200*time.Millisecond {
			need = 200 * time.Millisecond // 分片等待，保证限速调整能及时生效
		}
		time.Sleep(need)
	}
}

// limitedReader 逐块消耗配额后放行读取；配合 io.Copy 使用。
// 额外限速器 extra 不为 nil 时再叠加一层每任务限速（取更严的一档）。
type limitedReader struct {
	r     io.Reader
	ctx   context.Context
	l     *rateLimiter
	extra *rateLimiter
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if err := lr.ctx.Err(); err != nil {
		return 0, err
	}
	n := len(p)
	if n > readChunkSize {
		n = readChunkSize
	}
	lr.l.Wait(int64(n))
	if lr.extra != nil {
		lr.extra.Wait(int64(n))
	}
	return lr.r.Read(p)
}
