package task

import "time"

// Status AI 任务状态。
type Status int16

const (
	StatusPending   Status = 1 // 已入库，等待 worker 拉取
	StatusRunning   Status = 2 // worker 处理中
	StatusSucceeded Status = 3 // 处理完成
	StatusFailed    Status = 4 // 重试用尽，最终失败
)

// Task 是 AI 任务聚合根。
// 字段全部小写，外部只能通过构造函数 + Mark* 方法 + Snapshot/Restore 访问。
type Task struct {
	id          int64
	model       string
	messages    []AIMessage
	status      Status
	content     string
	totalTokens int
	errorMsg    string
	createdAt   time.Time
	updatedAt   time.Time
}

// NewAITask 业务创建：状态置为 Pending，id 留 0 待 repo.Save 回填。
func NewAITask(model string, messages []AIMessage) *Task {
	now := time.Now()
	return &Task{
		model:     model,
		messages:  messages,
		status:    StatusPending,
		createdAt: now,
		updatedAt: now,
	}
}

func (t *Task) ID() int64           { return t.id }
func (t *Task) Status() Status      { return t.status }
func (t *Task) Content() string     { return t.content }
func (t *Task) Model() string       { return t.model }
func (t *Task) Messages() []AIMessage { return t.messages }

// MarkRunning 进入处理中。worker 拉到 job 时调用。
func (t *Task) MarkRunning() {
	t.status = StatusRunning
	t.updatedAt = time.Now()
}

// MarkSucceeded 处理成功，保存模型输出 + token 用量。
func (t *Task) MarkSucceeded(content string, totalTokens int) {
	t.status = StatusSucceeded
	t.content = content
	t.totalTokens = totalTokens
	t.updatedAt = time.Now()
}

// MarkFailed 处理失败，保存错误原因。
func (t *Task) MarkFailed(reason string) {
	t.status = StatusFailed
	t.errorMsg = reason
	t.updatedAt = time.Now()
}

// State 聚合的可序列化快照。
// 仅 persistence 层使用：Snapshot 写库、Restore 重建。业务代码不要碰。
type State struct {
	ID          int64
	Model       string
	Messages    []AIMessage
	Status      Status
	Content     string
	TotalTokens int
	ErrorMsg    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Snapshot 导出当前状态供 repo 写库。
func (t *Task) Snapshot() State {
	return State{
		ID:          t.id,
		Model:       t.model,
		Messages:    t.messages,
		Status:      t.status,
		Content:     t.content,
		TotalTokens: t.totalTokens,
		ErrorMsg:    t.errorMsg,
		CreatedAt:   t.createdAt,
		UpdatedAt:   t.updatedAt,
	}
}

// Restore 由数据库行重建聚合，仅 repo 调用。
func Restore(s State) *Task {
	return &Task{
		id:          s.ID,
		model:       s.Model,
		messages:    s.Messages,
		status:      s.Status,
		content:     s.Content,
		totalTokens: s.TotalTokens,
		errorMsg:    s.ErrorMsg,
		createdAt:   s.CreatedAt,
		updatedAt:   s.UpdatedAt,
	}
}

// SetID 仅供 repo 在 INSERT RETURNING 之后回填，业务层勿调。
func (t *Task) SetID(id int64) { t.id = id }
