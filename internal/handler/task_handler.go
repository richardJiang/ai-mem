package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"mem-test/internal/db"
	"mem-test/internal/model"
	"mem-test/internal/service"

	"gorm.io/gorm"
)

type TaskHandler struct {
	agentService      *service.AgentService
	coachService      *service.CoachService
	reflectionService *service.ReflectionService
}

func NewTaskHandler(agentService *service.AgentService, coachService *service.CoachService, reflectionService *service.ReflectionService) *TaskHandler {
	return &TaskHandler{
		agentService:      agentService,
		coachService:      coachService,
		reflectionService: reflectionService,
	}
}

// ExecuteTask 执行任务
func (h *TaskHandler) ExecuteTask(c *gin.Context) {
	var req struct {
		TaskType  string `json:"task_type" binding:"required"`
		Input     string `json:"input" binding:"required"`
		GroupType string `json:"group_type"` // A/B/C
		UseMemory bool   `json:"use_memory"` // 是否使用记忆
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.GroupType == "" {
		req.GroupType = "C" // 默认C组
	}

	task, err := h.agentService.ExecuteTask(c.Request.Context(), req.TaskType, req.Input, req.GroupType, req.UseMemory)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task": task,
	})
}

// SubmitFeedback 提交反馈
func (h *TaskHandler) SubmitFeedback(c *gin.Context) {
	var req struct {
		TaskID       uint   `json:"task_id" binding:"required"`
		FeedbackType string `json:"feedback_type" binding:"required"` // correct/incorrect/better
		Content      string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	feedback, err := h.coachService.SubmitFeedback(c.Request.Context(), req.TaskID, req.FeedbackType, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"feedback": feedback,
	})
}

// AutoJudgeAndReflect 自动判断并反思（完整流程）
func (h *TaskHandler) AutoJudgeAndReflect(c *gin.Context) {
	var req struct {
		TaskID uint `json:"task_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取任务
	var task model.Task
	if err := db.DB.First(&task, req.TaskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	// 自动判断（规则引擎）
	var feedback *model.Feedback
	var err error
	switch task.TaskType {
	case "lottery_v2":
		feedback, err = h.coachService.JudgeLotteryV2Task(c.Request.Context(), &task)
	default:
		feedback, err = h.coachService.JudgeLotteryTask(c.Request.Context(), &task)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 如果是错误反馈，进行反思
	if feedback.Type == "incorrect" {
		memory, err := h.reflectionService.ReflectAndSaveMemory(c.Request.Context(), req.TaskID, feedback)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"feedback": feedback,
			"memory":   memory,
		})
		return
	}

	// 判对：对本次使用到的记忆做“验证时间”更新，帮助规则变更场景下优先检索当前有效规则
	if feedback.Type == "correct" && task.MemoryIDs != "" {
		ids := service.ParseMemoryIDs(task.MemoryIDs)
		if len(ids) > 0 {
			now := time.Now()
			_ = db.DB.WithContext(c.Request.Context()).
				Model(&model.Memory{}).
				Where("id IN ?", ids).
				Updates(map[string]interface{}{
					"last_verified_at": now,
					"confidence":       gorm.Expr("LEAST(confidence + ?, 1)", 0.01),
				}).Error
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"feedback": feedback,
		"message":  "判断正确，无需反思",
	})
}

// ReflectAndSave 反思并保存记忆
func (h *TaskHandler) ReflectAndSave(c *gin.Context) {
	var req struct {
		TaskID     uint `json:"task_id" binding:"required"`
		FeedbackID uint `json:"feedback_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取反馈
	var feedback model.Feedback
	if err := db.DB.First(&feedback, req.FeedbackID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "反馈不存在"})
		return
	}

	memory, err := h.reflectionService.ReflectAndSaveMemory(c.Request.Context(), req.TaskID, &feedback)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"memory":  memory,
		"message": "记忆已保存",
	})
}
