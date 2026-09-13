package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
)

type Downloader struct {
	config   *Config
	client   *HTTPClient
	progress *Progress
}

func NewDownloader(cfg *Config) *Downloader {
	return &Downloader{
		config:   cfg,
		client:   NewHTTPClient(cfg.MaxRetries),
		progress: NewProgress(),
	}
}

func (d *Downloader) Download() error {
	fileSize, err := d.client.GetFileSize(d.config.URL)
	if err != nil {
		return fmt.Errorf("获取文件大小失败: %w", err)
	}

	filename := extractFilename(d.config.URL)
	filePath := d.config.OutputPath(filename)

	if err := os.MkdirAll(d.config.DownloadDir, 0755); err != nil {
		return fmt.Errorf("创建下载目录失败: %w", err)
	}

	supportsRange := d.client.SupportsRange(d.config.URL)
	chunks := CalculateChunks(fileSize, d.config.ChunkSize, d.config.Threads)

	fmt.Printf("文件: %s (%s)\n", filename, formatSize(fileSize))
	fmt.Printf("线程数: %d | 分片数: %d\n", d.config.Threads, len(chunks))

	if !supportsRange || len(chunks) <= 1 {
		fmt.Println("服务器不支持分片下载，使用单线程模式")
		return d.downloadSingle(filePath)
	}

	return d.downloadMulti(filePath, fileSize, chunks)
}

func (d *Downloader) downloadSingle(filePath string) error {
	data, err := d.client.Get(d.config.URL)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func (d *Downloader) downloadMulti(filePath string, fileSize int64, chunks []Chunk) error {
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	if err := file.Truncate(fileSize); err != nil {
		return fmt.Errorf("预分配文件空间失败: %w", err)
	}

	d.progress.Start(fileSize)

	var (
		wg      sync.WaitGroup
		errOnce sync.Once
		dlErr   error
	)

	chunkCh := make(chan Chunk, len(chunks))
	for _, c := range chunks {
		chunkCh <- c
	}
	close(chunkCh)

	workers := d.config.Threads
	if workers > len(chunks) {
		workers = len(chunks)
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range chunkCh {
				if err := d.downloadChunk(file, chunk); err != nil {
					errOnce.Do(func() {
						dlErr = err
					})
					return
				}
			}
		}()
	}

	wg.Wait()
	d.progress.Finish()

	if dlErr != nil {
		os.Remove(filePath)
		return dlErr
	}

	return file.Sync()
}

func (d *Downloader) downloadChunk(file *os.File, chunk Chunk) error {
	data, err := d.client.GetRange(d.config.URL, chunk.Start, chunk.End)
	if err != nil {
		return fmt.Errorf("分片 %d 下载失败: %w", chunk.Index, err)
	}

	if _, err := file.WriteAt(data, chunk.Start); err != nil {
		return fmt.Errorf("分片 %d 写入失败: %w", chunk.Index, err)
	}

	d.progress.Add(int64(len(data)))
	return nil
}

func extractFilename(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "download"
	}

	path := parsed.Path
	if path == "" || path == "/" {
		return "download"
	}

	parts := strings.Split(path, "/")
	filename := parts[len(parts)-1]

	if filename == "" {
		return "download"
	}
	return filename
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
