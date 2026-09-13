package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ErrPaused 表示任务因用户暂停/取消而停止，区别于真实下载错误。
var ErrPaused = errors.New("paused")

const (
	maxRetries        = 5
	initialBackoff    = 1 * time.Second
	maxBackoff        = 16 * time.Second
	sidecarFlushEvery = 2 * time.Second
)

// offsetWriter 从固定偏移写入文件，供 io.Copy 流式落盘使用。
type offsetWriter struct {
	f   *os.File
	off int64
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	return n, err
}

// taskRunner 负责一个任务单次运行的完整下载逻辑：
// 探测 → 续传校验 → 分段下载（含重试）→ 落盘 → 改名。
type taskRunner struct {
	m         *Manager
	h         *taskHandle
	task      Task // 本地副本，关键字段经 updateTask 写回
	cancel    context.CancelFunc
	sidecar   *Sidecar
	scMu      sync.Mutex
	dirty     bool
	partPath  string
	statePath string
	partFile  *os.File
	resuming  bool
}

func (r *taskRunner) run(ctx context.Context) error {
	probe, err := probeURL(ctx, r.m.client, r.task.URL)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return ErrPaused
		}
		return fmt.Errorf("探测失败: %w", err)
	}

	r.statePath = r.m.store.StatePath(r.task.ID)
	sc := r.tryLoadSidecar(probe)
	fresh := sc == nil
	if !fresh {
		r.sidecar = sc
		r.partPath = filepath.Join(r.task.SaveDir, r.task.FileName+".part")
		if _, err := os.Stat(r.partPath); err != nil {
			fresh = true // .part 丢失，只能从头开始
		}
	}
	if fresh {
		name := sanitizeFileName(probe.FileName)
		if name == "" {
			name = "download"
		}
		name = uniqueName(r.task.SaveDir, name)
		r.task.FileName = name
		r.partPath = filepath.Join(r.task.SaveDir, name+".part")
		r.sidecar = newSidecar(r.task.URL, probe, r.task.Connections)
		r.resuming = false
	} else {
		r.resuming = true
	}

	total := probe.TotalSize
	if total <= 0 {
		total = r.sidecar.TotalSize
	}
	r.task.TotalSize = total
	if r.sidecar.TotalSize <= 0 && total > 0 {
		r.sidecar.TotalSize = total
	}

	if err := os.MkdirAll(r.task.SaveDir, 0o755); err != nil {
		return err
	}
	r.m.updateTask(r.h, func(t *Task) {
		t.FileName = r.task.FileName
		t.TotalSize = r.task.TotalSize
	})

	flags := os.O_CREATE | os.O_WRONLY
	if !r.resuming {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(r.partPath, flags, 0o644)
	if err != nil {
		return err
	}
	r.partFile = f
	defer func() {
		if r.partFile != nil {
			_ = f.Close()
			r.partFile = nil
		}
	}()

	r.saveSidecar()

	if r.sidecar.Single {
		if st, err := f.Stat(); err == nil {
			r.m.setBase(r.h, st.Size())
		}
	} else {
		r.m.setBase(r.h, r.sidecar.DoneBytes())
	}

	// 定期把续传状态落盘，崩溃/断电后最多丢 sidecarFlushEvery 的记录
	flushStop := make(chan struct{})
	defer close(flushStop)
	go func() {
		ticker := time.NewTicker(sidecarFlushEvery)
		defer ticker.Stop()
		for {
			select {
			case <-flushStop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.flushSidecar()
			}
		}
	}()

	if r.sidecar.Single {
		err = r.runSingle(ctx, &r.h.live)
	} else {
		err = r.runMulti(ctx, &r.h.live)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			r.flushSidecar()
			return ErrPaused
		}
		return err
	}
	r.flushSidecar()
	return r.finishFile()
}

// newSidecar 根据探测结果构造初始续传状态。
func newSidecar(url string, p *ProbeResult, connections int) *Sidecar {
	sc := &Sidecar{
		URL:          url,
		TotalSize:    p.TotalSize,
		ETag:         p.ETag,
		LastModified: p.LastModified,
	}
	single := !p.AcceptsRange || p.TotalSize <= 0 || connections <= 1
	if single {
		sc.Single = true
		end := p.TotalSize - 1
		if p.TotalSize <= 0 {
			end = -1
		}
		sc.Chunks = []SidecarChunk{{Start: 0, End: end}}
	} else {
		for _, c := range CalculateChunks(p.TotalSize, connections) {
			sc.Chunks = append(sc.Chunks, SidecarChunk{Start: c.Start, End: c.End})
		}
	}
	return sc
}

// tryLoadSidecar 加载已存在的续传状态，并校验服务器内容未变化。
func (r *taskRunner) tryLoadSidecar(p *ProbeResult) *Sidecar {
	sc, err := loadSidecar(r.statePath)
	if err != nil || sc.URL != r.task.URL {
		return nil
	}
	if p.TotalSize > 0 && sc.TotalSize > 0 && p.TotalSize != sc.TotalSize {
		return nil
	}
	if p.ETag != "" && sc.ETag != "" && p.ETag != sc.ETag {
		return nil
	}
	if p.LastModified != "" && sc.LastModified != "" && p.LastModified != sc.LastModified {
		return nil
	}
	return sc
}

// runMulti 多连接分段下载：预分配文件，分段投喂给 worker 池。
func (r *taskRunner) runMulti(ctx context.Context, live *atomic.Int64) error {
	if r.sidecar.TotalSize <= 0 {
		return errors.New("分段下载缺少文件大小")
	}
	if err := r.partFile.Truncate(r.sidecar.TotalSize); err != nil {
		return err
	}
	pending := make(chan SidecarChunk, len(r.sidecar.Chunks))
	np := 0
	for _, c := range r.sidecar.Chunks {
		if !c.Done {
			pending <- c
			np++
		}
	}
	close(pending)
	if np == 0 {
		return nil
	}
	workers := r.task.Connections
	if workers > np {
		workers = np
	}

	var (
		wg        sync.WaitGroup
		once      sync.Once
		firstErr  error
		completed atomic.Int64
	)
	fail := func(err error) {
		once.Do(func() {
			firstErr = err
			if r.cancel != nil {
				r.cancel() // 快速失败：让其他 worker 尽快退出
			}
		})
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range pending {
				if err := r.downloadChunk(ctx, c, live); err != nil {
					if !errors.Is(err, context.Canceled) {
						fail(err)
					}
					return
				}
				r.markChunkDone(c)
				completed.Add(1)
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	if int(completed.Load()) != np {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("下载未完成")
	}
	return nil
}

// downloadChunk 带指数退避重试地下载单个分段。
func (r *taskRunner) downloadChunk(ctx context.Context, c SidecarChunk, live *atomic.Int64) error {
	backoff := initialBackoff
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
		err := r.fetchRange(ctx, c, live)
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (r *taskRunner) fetchRange(ctx context.Context, c SidecarChunk, live *atomic.Int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.task.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", c.Start, c.End))
	resp, err := r.m.client.Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("服务器返回状态码 %d（需要 206）", resp.StatusCode)
	}
	w := &offsetWriter{f: r.partFile, off: c.Start}
	n, err := io.Copy(w, resp.Body)
	live.Add(n)
	if err != nil {
		return err
	}
	if want := c.End - c.Start + 1; n != want {
		return fmt.Errorf("分段 [%d,%d] 只收到 %d/%d 字节", c.Start, c.End, n, want)
	}
	return nil
}

// runSingle 单连接流式下载。重试时从 .part 当前大小继续（流式续传）。
func (r *taskRunner) runSingle(ctx context.Context, live *atomic.Int64) error {
	offset := int64(0)
	if st, err := r.partFile.Stat(); err == nil {
		offset = st.Size()
	}
	if r.task.TotalSize > 0 && offset >= r.task.TotalSize {
		return nil
	}
	backoff := initialBackoff
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
		if st, err := r.partFile.Stat(); err == nil {
			offset = st.Size()
		}
		err := r.fetchStream(ctx, offset, live)
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (r *taskRunner) fetchStream(ctx context.Context, offset int64, live *atomic.Int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.task.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := r.m.client.Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp)
	switch {
	case resp.StatusCode == http.StatusPartialContent:
	case resp.StatusCode == http.StatusOK && offset == 0:
	default:
		return fmt.Errorf("服务器返回状态码 %d，无法续传", resp.StatusCode)
	}
	w := &offsetWriter{f: r.partFile, off: offset}
	n, err := io.Copy(w, resp.Body)
	live.Add(n)
	if err != nil {
		return err
	}
	if r.task.TotalSize > 0 && offset+n != r.task.TotalSize {
		return fmt.Errorf("下载不完整：%d/%d 字节", offset+n, r.task.TotalSize)
	}
	return nil
}

func (r *taskRunner) markChunkDone(c SidecarChunk) {
	r.scMu.Lock()
	defer r.scMu.Unlock()
	for i := range r.sidecar.Chunks {
		if r.sidecar.Chunks[i].Start == c.Start && r.sidecar.Chunks[i].End == c.End {
			r.sidecar.Chunks[i].Done = true
			break
		}
	}
	r.dirty = true
}

func (r *taskRunner) flushSidecar() {
	r.scMu.Lock()
	if !r.dirty {
		r.scMu.Unlock()
		return
	}
	r.dirty = false
	r.scMu.Unlock()
	_ = saveSidecar(r.statePath, r.sidecar)
}

func (r *taskRunner) saveSidecar() {
	r.scMu.Lock()
	r.dirty = false
	r.scMu.Unlock()
	_ = saveSidecar(r.statePath, r.sidecar)
}

// finishFile 关闭 .part 并改名为正式文件。
func (r *taskRunner) finishFile() error {
	if r.partFile != nil {
		_ = r.partFile.Sync()
		_ = r.partFile.Close()
		r.partFile = nil
	}
	final := filepath.Join(r.task.SaveDir, uniqueName(r.task.SaveDir, r.task.FileName))
	return os.Rename(r.partPath, final)
}

// uniqueName 目录下同名文件存在时追加 " (n)" 后缀。
func uniqueName(dir, name string) string {
	if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
		return name
	}
	ext := filepath.Ext(name)
	stem := name[:len(name)-len(ext)]
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Stat(filepath.Join(dir, cand)); os.IsNotExist(err) {
			return cand
		}
	}
}
