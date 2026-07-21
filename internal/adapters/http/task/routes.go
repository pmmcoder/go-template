// Package task 提供 task 聚合的 HTTP 路由 + handler + DTO（基于 gin）。
// 通过 Register(r, svc) 注册到上层 gin 路由，与其他聚合互不感知。
package task

import (
	"encoding/json"
	"errors"
	"fmt"
	apptask "my_project/internal/application/task"
	domaintask "my_project/internal/domain/task"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Register 把 task 聚合的路由注册到给定 gin router。
// 新增路由只需在这里加一行。
func Register(r gin.IRouter, svc *apptask.Service) {
	h := &handler{svc: svc}
	r.POST("/tasks", h.submit)
	r.GET("/tasks/:id", h.get)
	r.GET("/async/tasks/:id", h.asyncGet)
}

type handler struct {
	svc *apptask.Service
}

// ---- 请求/响应 DTO ----

type submitRequest struct {
	Model     string                 `json:"model"`
	Messages  []domaintask.AIMessage `json:"messages"`
	UserID    string                 `json:"user_id"`
	ModelOpts map[string]interface{} `json:"model_opts"`
}

type submitResponse struct {
	TaskID int64 `json:"task_id"`
}

type getResponse struct {
	TaskID  int64             `json:"task_id"`
	Status  domaintask.Status `json:"status"`
	Model   string            `json:"model"`
	Content string            `json:"content,omitempty"`
}

// ---- handlers ----

func (h *handler) submit(c *gin.Context) {
	var req submitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages required"})
		return
	}
	id, err := h.svc.Submit(c.Request.Context(), apptask.SubmitInput{
		UserID:    req.UserID,
		Model:     req.Model,
		ModelOpts: req.ModelOpts,
		Messages:  req.Messages,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, submitResponse{TaskID: id})
}

func (h *handler) get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	v, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domaintask.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, getResponse{
		TaskID:  v.ID,
		Status:  v.Status,
		Model:   v.Model,
		Content: v.Content,
	})
}

func (h *handler) asyncGet(c *gin.Context) {
	resp := asyncGetResp{
		HttpCode: http.StatusOK,
		Code:     CodeSuccess,
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
		return
	}

	c.Writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.HttpCode = http.StatusBadRequest
		resp.Code = CodeFailed
		resp.Message = "invalid id"

		c.Writer.WriteString(fmt.Sprintf("data: %s", structToString(resp)))
		flusher.Flush()
		return
	}

	recChan := make(chan string, 1)

	go func() {
		for {
			select {
			case rec := <-recChan:
				resp.Message = fmt.Sprintf("%v", rec)

				c.Writer.WriteString(fmt.Sprintf("data: %s \n\n", structToString(resp)))
				flusher.Flush()
			case <-c.Request.Context().Done():
				return
			}
		}
	}()

	v, err := h.svc.AsyncGet(c, id, recChan)
	if err != nil {
		resp.HttpCode = http.StatusBadRequest
		resp.Code = CodeFailed
		if errors.Is(err, domaintask.ErrNotFound) {
			resp.Message = "task not found"

			c.Writer.WriteString(fmt.Sprintf("data: %s \n\n", structToString(resp)))
			flusher.Flush()
			return
		}
		resp.Message = err.Error()

		c.Writer.WriteString(fmt.Sprintf("data: %s \n\n", structToString(resp)))
		flusher.Flush()
		return
	}

	c.JSON(http.StatusOK, getResponse{
		TaskID:  v.ID,
		Status:  v.Status,
		Model:   v.Model,
		Content: v.Content,
	})
}

type asyncGetResp struct {
	HttpCode int    `json:"http_code"`
	Code     int    `json:"code"`
	Message  string `json:"message"`
}

const (
	CodeSuccess = 1 + iota
	CodeFailed
)

func structToString(input asyncGetResp) string {
	bytes, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(bytes)
}
