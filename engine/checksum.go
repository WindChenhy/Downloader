package engine

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

// normalizeChecksumAlgo 规范算法名；期望摘要长度可推断算法。
func normalizeChecksumAlgo(algo, expected string) string {
	a := strings.ToLower(strings.TrimSpace(algo))
	expected = strings.ToLower(strings.TrimSpace(expected))
	switch a {
	case "md5", "sha1", "sha256":
		return a
	}
	if expected == "" {
		return ""
	}
	switch len(expected) {
	case 32:
		return "md5"
	case 40:
		return "sha1"
	case 64:
		return "sha256"
	default:
		return ""
	}
}

func newHash(algo string) (hash.Hash, error) {
	switch algo {
	case "md5":
		return md5.New(), nil
	case "sha1":
		return sha1.New(), nil
	case "sha256":
		return sha256.New(), nil
	default:
		return nil, fmt.Errorf("不支持的校验算法: %s", algo)
	}
}

// fileChecksum 计算文件摘要（十六进制小写）。
func fileChecksum(path, algo string) (string, error) {
	h, err := newHash(algo)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyChecksum 完成后校验：返回 actual、status。algo/expected 均空时跳过。
func verifyChecksum(path, algo, expected string) (actual, status string) {
	algo = normalizeChecksumAlgo(algo, expected)
	expected = strings.ToLower(strings.TrimSpace(expected))
	if algo == "" {
		return "", ChecksumSkipped
	}
	actual, err := fileChecksum(path, algo)
	if err != nil {
		return "", ChecksumError
	}
	if expected == "" {
		return actual, ChecksumSkipped // 仅计算摘要，未提供期望值
	}
	if actual == expected {
		return actual, ChecksumOK
	}
	return actual, ChecksumMismatch
}
