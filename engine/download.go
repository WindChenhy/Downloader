package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
// onWrite 在每次成功写入后被调用（用于把段内进度记入续传状态）。
type offsetWriter struct {
	f       *os.File
	off     int64
	onWrite func(n int64)
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	if n > 0 && w.onWrite != nil {
		w.onWrite(int64(n))
	}
	return n, err
}

// taskRunner 负责一个任务单次运行的完整下载逻辑：
// 探测 → 续传校验 → 分段下载（含重试）→ 落盘 → 改名。
type taskRunner struct {
	m            *Manager
	h            *taskHandle
	task         Task // 本地副本，关键字段经 updateTask 写回
	cancel       context.CancelFunc
	settings     Settings // 运行开始时的设置快照
	fetchURL     string   // 实际请求 URL（可能经过镜像改写）
	userAgent    string   // 本轮使用的 UA
	extraHeaders map[string]string
	sidecar      *Sidecar
	scMu         sync.Mutex
	dirty        bool
	partPath     string
	statePath    string
	partFile     *os.File
	resuming     bool
	taskLimiter  *rateLimiter // 每任务限速；nil 表示不限
}

func (r *taskRunner) run(ctx context.Context) error {
	// 运行开始时锁定设置快照：UA/请求头/镜像/代理在本轮内保持一致
	r.settings = r.m.GetSettings()
	r.userAgent = strings.TrimSpace(r.settings.UserAgent)
	if r.userAgent == "" {
		r.userAgent = defaultUA
	}
	r.extraHeaders = parseExtraHeaders(r.settings.ExtraHeaders)
	r.fetchURL = resolveFetchURL(r.settings, r.task.URL)
	if r.task.SpeedLimit > 0 {
		r.taskLimiter = newRateLimiter(r.task.SpeedLimit)
	}

	probe, err := probeURL(ctx, r.m.httpClient(), r.fetchURL, r.userAgent, r.extraHeaders)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return ErrPaused
		}
		return fmt.Errorf("探测失败: %w", err)
	}

	r.statePath = r.m.store.StatePath(r.task.ID)
	// 暂存文件按任务 ID 命名、放在保存目录的 .downloader 子目录内：
	// 与最终文件名解耦（改名/重名检测都不影响），且同盘完成时原地改名
	r.partPath = r.m.stagingPath(r.task.SaveDir, r.task.ID)
	sc := r.tryLoadSidecar(probe)
	fresh := sc == nil
	if !fresh {
		r.sidecar = sc
		if _, err := os.Stat(r.partPath); err != nil {
			fresh = true // 暂存数据丢失，只能从头开始
		}
	}
	if fresh {
		// 命名优先级：用户自定义 > Content-Disposition > URL 路径
		name := sanitizeFileName(r.task.CustomName)
		if name == "" {
			name = sanitizeFileName(probe.FileName)
		}
		if name == "" {
			name = "download"
		}
		name = uniqueName(r.task.SaveDir, name)
		r.task.FileName = name
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

	if err := os.MkdirAll(filepath.Dir(r.partPath), 0o755); err != nil {
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
		// 基准含未完成分段内的部分进度：恢复后进度条直接回到真实位置
		r.m.setBase(r.h, r.sidecar.ProgressBytes())
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
	if err := r.finishFile(); err != nil {
		return err
	}
	final := filepath.Join(r.task.SaveDir, r.task.FileName)
	// 可选校验和：完成后计算摘要；提供了期望值则比对
	if r.task.ChecksumAlgo != "" || r.task.ChecksumExpected != "" {
		actual, status := verifyChecksum(final, r.task.ChecksumAlgo, r.task.ChecksumExpected)
		r.task.ChecksumActual = actual
		r.task.ChecksumStatus = status
		algo := normalizeChecksumAlgo(r.task.ChecksumAlgo, r.task.ChecksumExpected)
		r.m.updateTask(r.h, func(t *Task) {
			t.ChecksumActual = actual
			t.ChecksumStatus = status
			t.ChecksumAlgo = algo
		})
		if status == ChecksumMismatch {
			return fmt.Errorf("校验和不匹配：期望 %s，实际 %s", r.task.ChecksumExpected, actual)
		}
		if status == ChecksumError {
			return fmt.Errorf("校验和计算失败")
		}
	}
	// 下载完成后按设置自动解压压缩包（失败不改变任务完成状态）
	if r.settings.AutoExtract && r.task.FileName != "" {
		if isArchivePath(final) {
			_ = extractArchive(final)
		}
	}
	return nil
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

// runMulti 多连接分段下载：预分配文件，未完成分段（含段内部分进度）投喂给 worker 池。
func (r *taskRunner) runMulti(ctx context.Context, live *atomic.Int64) error {
	if r.sidecar.TotalSize <= 0 {
		return errors.New("分段下载缺少文件大小")
	}
	if err := r.partFile.Truncate(r.sidecar.TotalSize); err != nil {
		return err
	}
	r.scMu.Lock()
	pending := make(chan int, len(r.sidecar.Chunks))
	np := 0
	for i := range r.sidecar.Chunks {
		c := r.sidecar.Chunks[i]
		if !c.Done && c.Received < c.Size() {
			pending <- i
			np++
		}
	}
	r.scMu.Unlock()
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
			for idx := range pending {
				if err := r.downloadChunk(ctx, idx, live); err != nil {
					if !errors.Is(err, context.Canceled) {
						fail(err)
					}
					return
				}
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

// downloadChunk 带指数退避重试地下载单个分段；每次重试都从段内已收偏移继续。
func (r *taskRunner) downloadChunk(ctx context.Context, idx int, live *atomic.Int64) error {
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
		err := r.fetchRange(ctx, idx, live)
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

// chunkAt 读取分段当前状态（含段内已收字节数）的快照。
func (r *taskRunner) chunkAt(idx int) SidecarChunk {
	r.scMu.Lock()
	defer r.scMu.Unlock()
	return r.sidecar.Chunks[idx]
}

// addChunkProgress 累计分段内已落盘字节数（由 offsetWriter 回调触发）。
func (r *taskRunner) addChunkProgress(idx int, n int64) {
	r.scMu.Lock()
	r.sidecar.Chunks[idx].Received += n
	r.dirty = true
	r.scMu.Unlock()
}

func (r *taskRunner) markChunkDone(idx int) {
	r.scMu.Lock()
	r.sidecar.Chunks[idx].Done = true
	r.sidecar.Chunks[idx].Received = r.sidecar.Chunks[idx].Size()
	r.dirty = true
	r.scMu.Unlock()
}

// fetchRange 下载一个分段：从段内已收偏移（字节级断点）续传到段尾。
func (r *taskRunner) fetchRange(ctx context.Context, idx int, live *atomic.Int64) error {
	c := r.chunkAt(idx)
	if c.Done {
		return nil
	}
	start := c.Start + c.Received
	if start > c.End {
		r.markChunkDone(idx)
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.fetchURL, nil)
	if err != nil {
		return err
	}
	applyExtraHeaders(req, r.extraHeaders)
	req.Header.Set("User-Agent", r.userAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, c.End))
	resp, err := r.m.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("服务器返回状态码 %d（需要 206）", resp.StatusCode)
	}
	w := &offsetWriter{f: r.partFile, off: start, onWrite: func(n int64) {
		r.addChunkProgress(idx, n)
		live.Add(n) // 每个写入块实时计入进度，而不是等整段完成
	}}
	n, err := io.Copy(w, &limitedReader{r: resp.Body, ctx: ctx, l: r.m.limiter, extra: r.taskLimiter})
	if err != nil {
		return err
	}
	if start+n != c.End+1 {
		return fmt.Errorf("分段 [%d,%d] 只收到 %d/%d 字节", c.Start, c.End, start+n-c.Start, c.Size())
	}
	r.markChunkDone(idx)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.fetchURL, nil)
	if err != nil {
		return err
	}
	applyExtraHeaders(req, r.extraHeaders)
	req.Header.Set("User-Agent", r.userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := r.m.httpClient().Do(req)
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
	w := &offsetWriter{f: r.partFile, off: offset, onWrite: func(n int64) { live.Add(n) }}
	n, err := io.Copy(w, &limitedReader{r: resp.Body, ctx: ctx, l: r.m.limiter, extra: r.taskLimiter})
	if err != nil {
		return err
	}
	if r.task.TotalSize > 0 && offset+n != r.task.TotalSize {
		return fmt.Errorf("下载不完整：%d/%d 字节", offset+n, r.task.TotalSize)
	}
	return nil
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

// finishFile 关闭暂存数据并改名为正式文件（同盘原地改名，瞬时完成）。
func (r *taskRunner) finishFile() error {
	if r.partFile != nil {
		_ = r.partFile.Sync()
		_ = r.partFile.Close()
		r.partFile = nil
	}
	final := filepath.Join(r.task.SaveDir, uniqueName(r.task.SaveDir, r.task.FileName))
	if err := os.Rename(r.partPath, final); err != nil {
		return err
	}
	r.task.FileName = filepath.Base(final)
	r.m.updateTask(r.h, func(t *Task) {
		t.FileName = r.task.FileName
	})
	// 暂存目录已空则移除（其他任务共用时删除失败，忽略）
	_ = os.Remove(filepath.Dir(r.partPath))
	return nil
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
