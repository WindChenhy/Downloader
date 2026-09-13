package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var (
		url       string
		threads   int
		retries   int
		dir       string
		chunkSize int64
	)

	flag.StringVar(&url, "url", "", "下载地址 (必填)")
	flag.StringVar(&url, "u", "", "下载地址 (必填) [短参数]")
	flag.IntVar(&threads, "threads", DefaultThreads, "下载线程数")
	flag.IntVar(&threads, "t", DefaultThreads, "下载线程数 [短参数]")
	flag.IntVar(&retries, "retries", DefaultRetries, "重试次数 (1-5)")
	flag.IntVar(&retries, "r", DefaultRetries, "重试次数 (1-5) [短参数]")
	flag.StringVar(&dir, "dir", DefaultDownloadDir, "下载目录")
	flag.StringVar(&dir, "d", DefaultDownloadDir, "下载目录 [短参数]")
	flag.Int64Var(&chunkSize, "chunk-size", 0, "分片大小(字节)，0为自动计算")
	flag.Int64Var(&chunkSize, "c", 0, "分片大小(字节)，0为自动计算 [短参数]")
	flag.Parse()

	if url == "" {
		fmt.Fprintln(os.Stderr, "错误: 请指定下载地址")
		fmt.Fprintln(os.Stderr)
		flag.Usage()
		os.Exit(1)
	}

	cfg := &Config{
		URL:         url,
		Threads:     threads,
		MaxRetries:  retries,
		DownloadDir: dir,
		ChunkSize:   chunkSize,
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(1)
	}

	d := NewDownloader(cfg)
	if err := d.Download(); err != nil {
		fmt.Fprintf(os.Stderr, "\n下载失败: %v\n", err)
		os.Exit(1)
	}
}
