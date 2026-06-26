package queue

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// 检查hook的实现是否出现问题
var (
	_ rivertype.HookInsertBegin = &JobHook{}
	_ rivertype.HookWorkBegin   = &JobHook{}
	_ rivertype.HookWorkEnd     = &JobHook{}
)

const (
	StatusPending = iota
	StatusAvailable
	StatusRunning
	StatusSuccess
	StatusFailed
)

type HookData struct {
	JobID  int64       `json:"job_id"`
	ErrMsg string      `json:"err_msg"`
	Status int         `json:"status"`
	Data   interface{} `json:"data"`
}

type JobHook struct {
	river.HookDefaults
}

func (j JobHook) InsertBegin(ctx context.Context, job *rivertype.JobInsertParams) error {
	j.notify(ctx, HookData{
		Status: StatusAvailable,
		Data:   fmt.Sprintf("JobHook.InsertBegin job.id:%d taskType:%s status:%s \n", job.ID, job.Kind, job.State),
	})
	return nil
}

func (j JobHook) WorkBegin(ctx context.Context, job *rivertype.JobRow) error {
	j.notify(ctx, HookData{
		JobID:  job.ID,
		Status: StatusRunning,
		Data:   fmt.Sprintf("JobHook.WorkBegin job.id:%d taskType:%s status:%s \n", job.ID, job.Kind, job.State),
	})
	return nil
}

func (j JobHook) WorkEnd(ctx context.Context, job *rivertype.JobRow, err error) error {
	var hookData = HookData{
		JobID: job.ID,
	}
	if err != nil {
		hookData.ErrMsg = err.Error()
		if job.Attempt >= job.MaxAttempts {
			hookData.Status = StatusFailed
		} else {
			hookData.Status = StatusRunning
		}
	} else {
		hookData.Status = StatusSuccess
	}

	hookData.Data = fmt.Sprintf("JobHook.WorkEnd job.id:%d args:%s  status:%d meta:%s \n", job.ID, string(job.EncodedArgs), hookData.Status, job.Metadata)

	j.notify(ctx, hookData)

	return err
}

func (j JobHook) notify(ctx context.Context, message HookData) {
	select {
	case <-ctx.Done():
		fmt.Printf("JobHook.notify context done\n")
	case globalQueue.GetHookChan() <- message:
	default:
		fmt.Printf("JobHook.notify.channel channel full \n")
	}
}
