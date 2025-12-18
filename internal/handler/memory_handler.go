package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"mem-test/internal/db"
	"mem-test/internal/model"
)

type MemoryHandler struct {
}

func NewMemoryHandler() *MemoryHandler {
	return &MemoryHandler{}
}

// ListMemories 列出所有记忆
func (h *MemoryHandler) ListMemories(c *gin.Context) {
	var memories []model.Memory
	
	query := db.DB.Order("created_at DESC")
	
	if limit := c.Query("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			query = query.Limit(l)
		}
	}

	if err := query.Find(&memories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"memories": memories,
	})
}

// GetMemory 获取单个记忆
func (h *MemoryHandler) GetMemory(c *gin.Context) {
	id := c.Param("id")
	
	var memory model.Memory
	if err := db.DB.First(&memory, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "记忆不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"memory": memory,
	})
}

// DeleteMemory 删除记忆
func (h *MemoryHandler) DeleteMemory(c *gin.Context) {
	id := c.Param("id")
	
	if err := db.DB.Delete(&model.Memory{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "删除成功",
	})
}

