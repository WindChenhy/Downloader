package engine

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// isArchivePath 按扩展名判断是否为可自动解压的压缩包。
func isArchivePath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"),
		strings.HasSuffix(name, ".tar.bz2"), strings.HasSuffix(name, ".tbz2"):
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip", ".tar", ".gz", ".bz2":
		return true
	}
	return false
}

// extractArchive 按扩展名解压到 <同目录>/<去掉压缩后缀的文件名>/。
// 失败时返回错误，调用方可忽略（不影响任务完成状态）。
// 目前支持 zip / tar / tar.gz / tgz / tar.bz2 / gz(单文件)。
func extractArchive(archivePath string) error {
	st, err := os.Stat(archivePath)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("不是普通文件: %s", archivePath)
	}

	base := stripArchiveExt(filepath.Base(archivePath))
	if base == "" {
		base = filepath.Base(archivePath)
	}
	dest := filepath.Join(filepath.Dir(archivePath), base)
	if dest == filepath.Dir(archivePath) {
		dest = archivePath + "_extracted"
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, dest)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, dest)
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		return extractTarBz2(archivePath, dest)
	case strings.HasSuffix(lower, ".tar"):
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()
		return extractTar(f, dest)
	case strings.HasSuffix(lower, ".gz"):
		return extractGz(archivePath, dest, base)
	case strings.HasSuffix(lower, ".bz2"):
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()
		outName := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
		outPath := filepath.Join(dest, outName)
		out, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, bzip2.NewReader(f))
		return err
	}
	return fmt.Errorf("暂不支持的压缩格式: %s", filepath.Ext(archivePath))
}

// stripArchiveExt 去掉常见压缩后缀，返回解压目录名。
func stripArchiveExt(name string) string {
	lower := strings.ToLower(name)
	for _, suf := range []string{".tar.gz", ".tar.bz2", ".tgz", ".tbz2", ".zip", ".tar", ".gz", ".bz2"} {
		if strings.HasSuffix(lower, suf) {
			return name[:len(name)-len(suf)]
		}
	}
	return ""
}

func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := filepath.Join(dest, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(name, filepath.Clean(dest)+string(os.PathSeparator)) &&
			name != filepath.Clean(dest) {
			return fmt.Errorf("非法路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Join(dest, filepath.FromSlash(hdr.Name))
		if !strings.HasPrefix(name, filepath.Clean(dest)+string(os.PathSeparator)) &&
			name != filepath.Clean(dest) {
			return fmt.Errorf("非法路径: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink, tar.TypeLink:
			// 跳过链接，避免任意路径写入
			continue
		}
	}
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTar(gz, dest)
}

func extractTarBz2(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTar(bzip2.NewReader(f), dest)
}

// extractGz 解压非 tar 的单文件 .gz，输出去掉 .gz 的文件名。
func extractGz(src, dest, base string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	outName := base
	if outName == "" {
		outName = strings.TrimSuffix(filepath.Base(src), ".gz")
		outName = strings.TrimSuffix(outName, ".GZ")
	}
	outPath := filepath.Join(dest, outName)
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, gz)
	return err
}
