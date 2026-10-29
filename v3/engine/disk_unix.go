//go:build !windows

package engine

import "syscall"

// freeSpaceOnPath 返回 path 所在文件系统的剩余字节（非 Windows 仅供开发/CI 使用）。
// 用 Bavail（普通用户可用块）而不是 Bfree：root 保留块对下载这类普通进程不可用。
func freeSpaceOnPath(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
