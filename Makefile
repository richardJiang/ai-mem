.PHONY: run build test test-memory clean install experiment

run:
	go run main.go

experiment:
	HOST=http://localhost:8080 RUNS=100 ./scripts/run_experiment_100.sh

build:
	go build -o bin/mem-test main.go

test:
	go test ./...

test-memory:
	@echo "运行记忆演化测试..."
	@mkdir -p outputs
	go test -v ./internal/service -run TestMemory > outputs/memory_evolution_test_result.txt 2>&1
	@echo "测试结果已保存到: outputs/memory_evolution_test_result.txt"
	@tail -20 outputs/memory_evolution_test_result.txt

clean:
	rm -rf bin/

install:
	go mod download
	go mod tidy

