package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

type HTTPClient struct {
	client     *http.Client
	maxRetries int
}

func NewHTTPClient(maxRetries int) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		maxRetries: maxRetries,
	}
}

func (h *HTTPClient) GetFileSize(url string) (int64, error) {
	resp, err := h.head(url)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()

	if resp.ContentLength <= 0 {
		return 0, fmt.Errorf("unable to determine file size")
	}
	return resp.ContentLength, nil
}

func (h *HTTPClient) SupportsRange(url string) bool {
	resp, err := h.head(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.Header.Get("Accept-Ranges") == "bytes"
}

func (h *HTTPClient) GetRange(url string, start, end int64) ([]byte, error) {
	var data []byte
	var err error
	for i := 0; i <= h.maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(1<<uint(i-1)) * time.Second)
		}
		data, err = h.doRangeRequest(url, start, end)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("range [%d-%d] failed after %d retries: %w", start, end, h.maxRetries, err)
}

func (h *HTTPClient) Get(url string) ([]byte, error) {
	var data []byte
	var err error
	for i := 0; i <= h.maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(1<<uint(i-1)) * time.Second)
		}
		data, err = h.doGetRequest(url)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("download failed after %d retries: %w", h.maxRetries, err)
}

func (h *HTTPClient) head(url string) (*http.Response, error) {
	var resp *http.Response
	var err error
	for i := 0; i <= h.maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(1<<uint(i-1)) * time.Second)
		}
		req, reqErr := http.NewRequest(http.MethodHead, url, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err = h.client.Do(req)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusMovedPermanently) {
			return resp, nil
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	return nil, fmt.Errorf("HEAD failed after %d retries: %w", h.maxRetries, err)
}

func (h *HTTPClient) doRangeRequest(url string, start, end int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (h *HTTPClient) doGetRequest(url string) ([]byte, error) {
	resp, err := h.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
