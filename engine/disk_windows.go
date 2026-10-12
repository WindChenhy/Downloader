//go:build windows

package engine

import (
	"syscall"
	"unsafe"
)

// getDiskFreeSpaceExW kernel32 的卷剩余空间查询；进程内复用同一个句柄。
var getDiskFreeSpaceExW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// freeSpaceOnPath 返回 path 所在卷的剩余字节（仅标准库，不引 x/sys）。
// 取「调用方可写」的 FreeBytesAvailableToCaller 而不是卷的总剩余：磁盘配额、
// 压缩卷与只读重解析点上两者不同，能真正写进去的数才是下载文件的下限。
func freeSpaceOnPath(path string) (int64, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeToCaller, totalBytes, totalFree uint64
	r1, _, e1 := getDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return 0, e1
	}
	return int64(freeToCaller), nil
}
