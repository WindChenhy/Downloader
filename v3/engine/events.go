package engine

// EventName 引擎事件的名称。
type EventName string

const (
	// EventTasksChanged 任务列表快照变化（含运行中实时进度），携带 Tasks。
	EventTasksChanged EventName = "tasks:changed"
	// EventTaskCreated 新建任务，携带 Task。
	EventTaskCreated EventName = "task:created"
	// EventTaskPaused 任务暂停（排队中立即暂停 / 运行中取消后落定），携带 Task。
	EventTaskPaused EventName = "task:paused"
	// EventTaskFinished 任务完成或失败，携带 Task。
	EventTaskFinished EventName = "task:finished"
	// EventQueueIdle 队列清空且全部任务结束（一轮只发一次），携带最后结束的任务。
	EventQueueIdle EventName = "queue:idle"
)

// Event 引擎向 UI 层推送的事件。
// 列表类事件携带 Tasks（任务快照），单任务类事件携带 Task，
// 消费方按类型取值，无需对 interface{} 做断言。
type Event struct {
	Name  EventName
	Task  Task
	Tasks []Task
}
