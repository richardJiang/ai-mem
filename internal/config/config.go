package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	Dify     DifyConfig     `yaml:"dify"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	Charset  string `yaml:"charset"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type DifyConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	// 应用类型：workflow/chat/completion（本项目主要支持 workflow）
	AppType string `yaml:"app_type"`
	// response_mode: blocking/streaming（后端为简化处理建议 blocking）
	ResponseMode string `yaml:"response_mode"`
	// workflow 必填 inputs：system + query（字段名可配置，默认 system/query）
	WorkflowSystemKey string `yaml:"workflow_system_key"`
	WorkflowQueryKey  string `yaml:"workflow_query_key"`
	// Workflow 输出字段名（从 outputs 中取该 key 作为 answer；为空则自动猜测）
	WorkflowOutputKey string `yaml:"workflow_output_key"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, nil
}
