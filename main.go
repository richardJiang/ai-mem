package main

import (
	"fmt"
	"log"

	"mem-test/internal/config"
	"mem-test/internal/db"
	"mem-test/internal/router"
	"mem-test/internal/service"
)

func main() {
	// 加载配置
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化数据库
	if err := db.InitDB(cfg); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	// 初始化服务
	svcCtx := service.NewServiceContext(cfg)

	// 初始化路由
	r := router.SetupRouter(svcCtx)

	// 启动服务
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("服务启动在 %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}

