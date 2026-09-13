package main

import (
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
)

const (
	DefaultThreads     = 8
	DefaultRetries     = 3
	MaxRetries         = 5
	DefaultDownloadDir = `C:\Users\Mrchen\Downloads`
)

type Config struct {
	URL         string
	Threads     int
	MaxRetries  int
	DownloadDir string
	ChunkSize   int64
}

func (c *Config) Validate() error {
	if c.URL == "" {
		return errors.New("URL is required")
	}
	if _, err := url.ParseRequestURI(c.URL); err != nil {
		return errors.New("invalid URL format")
	}
	if c.Threads <= 0 {
		c.Threads = DefaultThreads
	}
	if runtime.NumCPU() < c.Threads {
		c.Threads = runtime.NumCPU()
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = DefaultRetries
	}
	if c.MaxRetries > MaxRetries {
		return errors.New("retries cannot exceed 5")
	}
	if c.DownloadDir == "" {
		c.DownloadDir = DefaultDownloadDir
	}
	if c.ChunkSize < 0 {
		c.ChunkSize = 0
	}
	return nil
}

func (c *Config) OutputPath(filename string) string {
	return filepath.Join(c.DownloadDir, filename)
}
