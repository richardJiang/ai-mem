package service

import (
	"mem-test/internal/config"
)

type ServiceContext struct {
	AgentService      *AgentService
	CoachService      *CoachService
	ReflectionService *ReflectionService
}

func NewServiceContext(cfg *config.Config) *ServiceContext {
	difyClient := NewDifyClient(
		cfg.Dify.BaseURL,
		cfg.Dify.APIKey,
		cfg.Dify.AppType,
		cfg.Dify.ResponseMode,
		cfg.Dify.WorkflowSystemKey,
		cfg.Dify.WorkflowQueryKey,
		cfg.Dify.WorkflowOutputKey,
	)
	memosClient := NewMemOSClient(cfg.MemOS.BaseURL, cfg.MemOS.TopK)

	return &ServiceContext{
		AgentService:      NewAgentService(difyClient, memosClient, cfg.MemOS.UserPrefix),
		CoachService:      NewCoachService(),
		ReflectionService: NewReflectionService(difyClient, memosClient, cfg.MemOS.UserPrefix),
	}
}
