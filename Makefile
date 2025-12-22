.PHONY: run build test test-unit test-integration clean install experiment

run:
	go run main.go

experiment:
	HOST=http://localhost:8080 RUNS=100 ./scripts/run_experiment_100.sh

experiment-abcd:
	HOST=http://localhost:8080 RUNS=100 EXP_GROUPS='["A","B","C","D"]' ./scripts/run_experiment_100.sh

experiment-abcde:
	HOST=http://localhost:8080 RUNS=100 EXP_GROUPS='["A","B","C","D","E"]' ./scripts/run_experiment_100.sh

experiment-low:
	HOST=http://localhost:8080 RUNS=100 RULE_MODE=low ./scripts/run_experiment_100.sh

experiment-abcd-low:
	HOST=http://localhost:8080 RUNS=100 RULE_MODE=low EXP_GROUPS='["A","B","C","D"]' ./scripts/run_experiment_100.sh

experiment-abcde-low:
	HOST=http://localhost:8080 RUNS=100 RULE_MODE=low EXP_GROUPS='["A","B","C","D","E"]' ./scripts/run_experiment_100.sh

experiment-high:
	HOST=http://localhost:8080 RUNS=100 RULE_MODE=high ./scripts/run_experiment_100.sh

experiment-abcd-high:
	HOST=http://localhost:8080 RUNS=100 RULE_MODE=high EXP_GROUPS='["A","B","C","D"]' ./scripts/run_experiment_100.sh

experiment-abcde-high:
	HOST=http://localhost:8080 RUNS=100 RULE_MODE=high EXP_GROUPS='["A","B","C","D","E"]' ./scripts/run_experiment_100.sh

experiment-multi:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi ./scripts/run_experiment_100.sh

experiment-multi-abcd:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi EXP_GROUPS='["A","B","C","D"]' ./scripts/run_experiment_100.sh

experiment-multi-abcde:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi EXP_GROUPS='["A","B","C","D","E"]' ./scripts/run_experiment_100.sh

experiment-multi-low:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi RULE_MODE=low ./scripts/run_experiment_100.sh

experiment-multi-abcd-low:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi RULE_MODE=low EXP_GROUPS='["A","B","C","D"]' ./scripts/run_experiment_100.sh

experiment-multi-abcde-low:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi RULE_MODE=low EXP_GROUPS='["A","B","C","D","E"]' ./scripts/run_experiment_100.sh

experiment-multi-high:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi RULE_MODE=high ./scripts/run_experiment_100.sh

experiment-multi-abcd-high:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi RULE_MODE=high EXP_GROUPS='["A","B","C","D"]' ./scripts/run_experiment_100.sh

experiment-multi-abcde-high:
	HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi RULE_MODE=high EXP_GROUPS='["A","B","C","D","E"]' ./scripts/run_experiment_100.sh

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
	HOST=http://localhost:8080 RUNS=100 ./scripts/run_experiment_100.sh

clean:
	rm -rf bin/

install:
	go mod download
	go mod tidy

