.PHONY: run build test test-unit test-integration clean install experiment

run:
	go run main.go

# 只保留“全组(A-F)”的入口，避免 abcd/abcde 等子集脚本造成维护负担
# 可通过环境变量覆盖：
# - HOST=http://localhost:8080
# - RUNS=100
# - TASK_TYPE=lottery 或 lottery_multi
# - RULE_MODE=none/low/high
# - EXP_GROUPS='["A","B","C","D","E","F"]'
EXP_HOST ?= http://localhost:8080
EXP_RUNS ?= 100
EXP_GROUPS ?= ["A","B","C","D","E","F"]

experiment:
	HOST=$(EXP_HOST) RUNS=$(EXP_RUNS) EXP_GROUPS='$(EXP_GROUPS)' ./scripts/run_experiment_100.sh

experiment-low:
	HOST=$(EXP_HOST) RUNS=$(EXP_RUNS) RULE_MODE=low EXP_GROUPS='$(EXP_GROUPS)' ./scripts/run_experiment_100.sh

experiment-high:
	HOST=$(EXP_HOST) RUNS=$(EXP_RUNS) RULE_MODE=high EXP_GROUPS='$(EXP_GROUPS)' ./scripts/run_experiment_100.sh

experiment-multi:
	HOST=$(EXP_HOST) RUNS=$(EXP_RUNS) TASK_TYPE=lottery_multi EXP_GROUPS='$(EXP_GROUPS)' ./scripts/run_experiment_100.sh

experiment-multi-low:
	HOST=$(EXP_HOST) RUNS=$(EXP_RUNS) TASK_TYPE=lottery_multi RULE_MODE=low EXP_GROUPS='$(EXP_GROUPS)' ./scripts/run_experiment_100.sh

experiment-multi-high:
	HOST=$(EXP_HOST) RUNS=$(EXP_RUNS) TASK_TYPE=lottery_multi RULE_MODE=high EXP_GROUPS='$(EXP_GROUPS)' ./scripts/run_experiment_100.sh

build:
	go build -o bin/mem-test main.go

test:
	go test ./... -short

test-unit:
	@echo "运行单元测试（不需要数据库）..."
	@mkdir -p outputs
	go test -v ./internal/service -run "TestMemoryEvolution|TestMemoryRetrieval|TestMemoryDeprecation" > outputs/memory_unit_test_result.txt 2>&1
	@echo "单元测试结果已保存到: outputs/memory_unit_test_result.txt"
	@tail -20 outputs/memory_unit_test_result.txt

test-integration:
	mkdir -p outputs
	HOST=$(EXP_HOST) RUNS=$(EXP_RUNS) EXP_GROUPS='$(EXP_GROUPS)' ./scripts/run_experiment_100.sh

clean:
	rm -rf bin/

install:
	go mod download
	go mod tidy

