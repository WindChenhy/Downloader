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

// Refund 归还本次未用掉的配额（短读时把差额退回）。
// Wait 必须预扣整块配额——不先知道要等多久就没法阻塞等待；但底层
// http body 的 Read 经常只返回几百字节，整块计费会把实际速率压成设定值的
// 几分之一（16KB 短读只有一半）。这里按实际读到的字节数结算。
func (l *rateLimiter) Refund(n int64) {
	if n <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limit <= 0 {
		return // 不限速时 Wait 未扣费，无从退还
	}
	now := time.Now()
	l.credit += float64(n) + now.Sub(l.last).Seconds()*float64(l.limit)
	l.last = now
	// 退还后仍受与 Wait 相同的突发上限约束，避免短读反复退款攒出无界配额
	maxCredit := float64(l.limit)
	if maxCredit < float64(n) {
		maxCredit = float64(n)
	}
	if l.credit > maxCredit {
		l.credit = maxCredit
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
	// 必须按配额裁剪后的长度读取：上游缓冲区一旦大于 readChunkSize，
	// 用整个 p 读就会拿到比已申请配额更多的字节，限速被绕过
	nr, err := lr.r.Read(p[:n])
	if nr < n {
		lr.l.Refund(int64(n - nr)) // 短读：按实际字节结算，不白扣整块配额
		if lr.extra != nil {
			lr.extra.Refund(int64(n - nr))
		}
	}
	return nr, err
}
