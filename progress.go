package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Progress struct {
	total   int64
	current int64
	start   time.Time
	mu      sync.Mutex
}

func NewProgress() *Progress {
	return &Progress{}
}

func (p *Progress) Start(total int64) {
	p.total = total
	atomic.StoreInt64(&p.current, 0)
	p.start = time.Now()
}

func (p *Progress) Add(bytes int64) {
	current := atomic.AddInt64(&p.current, bytes)
	p.print(current)
}

func (p *Progress) Finish() {
	elapsed := time.Since(p.start)
	speed := float64(0)
	if elapsed.Seconds() > 0 {
		speed = float64(p.total) / elapsed.Seconds()
	}
	fmt.Printf("\r下载完成 | 耗时: %s | 平均速度: %s/s\n",
		elapsed.Round(time.Millisecond),
		formatSize(int64(speed)))
}

func (p *Progress) print(current int64) {
	if p.total <= 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	elapsed := time.Since(p.start)
	percent := float64(current) / float64(p.total) * 100
	speed := float64(0)
	if elapsed.Seconds() > 0 {
		speed = float64(current) / elapsed.Seconds()
	}

	fmt.Printf("\r进度: %5.1f%% | %s / %s | %s/s",
		percent,
		formatSize(current),
		formatSize(p.total),
		formatSize(int64(speed)))
}
