package router

import (
	"mem-test/internal/handler"
	"mem-test/internal/service"

	"github.com/gin-gonic/gin"
)

func SetupRouter(cfg *service.ServiceContext) *gin.Engine {
	r := gin.Default()

	// CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// 初始化handlers
	taskHandler := handler.NewTaskHandler(cfg.AgentService, cfg.CoachService, cfg.ReflectionService)
	memoryHandler := handler.NewMemoryHandler()
	experimentRunner := service.NewExperimentRunner(cfg.AgentService, cfg.CoachService, cfg.ReflectionService)
	experimentHandler := handler.NewExperimentHandler(experimentRunner)

	// API路由
	api := r.Group("/api")
	{
		// 任务相关
		tasks := api.Group("/tasks")
		{
			tasks.POST("/execute", taskHandler.ExecuteTask)
			tasks.POST("/feedback", taskHandler.SubmitFeedback)
			tasks.POST("/judge", taskHandler.AutoJudgeAndReflect)
			tasks.POST("/reflect", taskHandler.ReflectAndSave)
		}

		// 记忆相关
		memories := api.Group("/memories")
		{
			memories.GET("", memoryHandler.ListMemories)
			memories.GET("/:id", memoryHandler.GetMemory)
			memories.DELETE("/:id", memoryHandler.DeleteMemory)
		}

		// 实验相关
		experiments := api.Group("/experiments")
		{
			experiments.GET("/stats", experimentHandler.GetExperimentStats)
			experiments.GET("/compare", experimentHandler.CompareGroups)
			experiments.GET("/compare-modes", experimentHandler.CompareGroupsByModes)
			experiments.GET("/trend", experimentHandler.GetErrorTrend)
			experiments.POST("/reset", experimentHandler.ResetAll)
			experiments.POST("/run", experimentHandler.RunExperiment)
		}
	}

	return r
}
