.PHONY: run build test test-unit test-integration clean install experiment

run:
	go run main.go

experiment:
	HOST=http://localhost:8080 RUNS=100 ./scripts/run_experiment_100.sh

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
	@echo "运行集成测试（需要数据库和Dify）..."
	@mkdir -p outputs
	go test -v ./internal/service -run "Integration" > outputs/memory_integration_test_result.txt 2>&1
	@echo "集成测试结果已保存到: outputs/memory_integration_test_result.txt"
	@tail -30 outputs/memory_integration_test_result.txt

clean:
	rm -rf bin/

install:
	go mod download
	go mod tidy

