package engine

import "errors"

// 哨兵错误：调用方（含 REST API 层）用 errors.Is 区分失败类别，
// 决定提示文案与 HTTP 状态码，而不是解析错误字符串。
var (
	// ErrTaskNotFound 任务记录不存在（已删除或 ID 无效）。
	ErrTaskNotFound = errors.New("任务不存在")
	// ErrInvalidState 当前状态不允许该操作（如恢复未暂停的任务）。
	ErrInvalidState = errors.New("当前状态不允许该操作")
)
