#!/bin/bash

# Dify API 测试脚本
# 用于测试不同的 Dify API 端点

BASE_URL="http://dify.ickey.com.cn/v1"
API_KEY="app-4kBYdLfSFgL2HZ3NTeUYpdpL"

echo "=== 测试 Dify API 端点 ==="
echo "Base URL: $BASE_URL"
echo "API Key: $API_KEY"
echo ""

# 测试请求体
REQUEST_BODY='{
  "inputs": {},
  "query": "你好",
  "response_mode": "blocking",
  "user": "test"
}'

# 测试端点列表
ENDPOINTS=(
  "chat-messages"
  "completion-messages"
  "messages"
  "workflows/run"
)

for endpoint in "${ENDPOINTS[@]}"; do
  echo "=== 测试: POST $BASE_URL/$endpoint ==="
  
  response=$(curl -s -w "\nHTTP_CODE:%{http_code}" \
    -X POST "$BASE_URL/$endpoint" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d "$REQUEST_BODY")
  
  http_code=$(echo "$response" | grep "HTTP_CODE:" | cut -d: -f2)
  body=$(echo "$response" | sed '/HTTP_CODE:/d')
  
  echo "状态码: $http_code"
  echo "响应: $body" | head -c 300
  echo ""
  echo "---"
  echo ""
done

echo "=== 测试完成 ==="
echo ""
echo "请根据上面的测试结果，找到返回 200 状态码的端点"
echo "然后更新 config.yaml 或代码中的端点配置"

