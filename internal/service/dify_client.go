package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type DifyClient struct {
	BaseURL           string
	APIKey            string
	Client            *http.Client
	AppType           string
	ResponseMode      string
	WorkflowSystemKey string
	WorkflowQueryKey  string
	WorkflowOutputKey string
}

func NewDifyClient(baseURL, apiKey string, appType string, responseMode string, workflowSystemKey string, workflowQueryKey string, workflowOutputKey string) *DifyClient {
	if responseMode == "" {
		responseMode = "blocking"
	}
	if workflowSystemKey == "" {
		workflowSystemKey = "system"
	}
	if workflowQueryKey == "" {
		workflowQueryKey = "query"
	}
	return &DifyClient{
		BaseURL:           baseURL,
		APIKey:            apiKey,
		AppType:           appType,
		ResponseMode:      responseMode,
		WorkflowSystemKey: workflowSystemKey,
		WorkflowQueryKey:  workflowQueryKey,
		WorkflowOutputKey: workflowOutputKey,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func difyMaxRetries() int {
	// 只针对超时/临时网络错误做“少量重试”，避免实验被偶发 timeout 直接污染
	// 默认 1 次重试（总共最多 2 次请求）
	v := strings.TrimSpace(os.Getenv("DIFY_MAX_RETRIES"))
	if v == "" {
		return 1
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 1
	}
	if n < 0 {
		return 0
	}
	if n > 3 {
		return 3
	}
	return n
}

func difyRetryBackoff(attempt int) time.Duration {
	// attempt: 1..N（第几次重试，不含首次）
	base := 300 * time.Millisecond
	// 简单指数退避 + jitter（避免多个并发同时重试）
	d := base * time.Duration(1<<(attempt-1))
	if d > 3*time.Second {
		d = 3 * time.Second
	}
	j := time.Duration(rand.Intn(200)) * time.Millisecond
	return d + j
}

func isRetryableDifyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() || ne.Temporary() {
			return true
		}
	}
	// 兜底：部分错误链会被包成字符串
	s := err.Error()
	if strings.Contains(s, "Client.Timeout exceeded") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "EOF") {
		return true
	}
	return false
}

func isRetryableStatus(code int) bool {
	// 429/5xx 典型可重试
	if code == http.StatusTooManyRequests {
		return true
	}
	if code >= 500 && code <= 599 {
		return true
	}
	return false
}

type ChatRequest struct {
	Inputs         map[string]interface{} `json:"inputs"`
	Query          string                 `json:"query"`
	ResponseMode   string                 `json:"response_mode"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	User           string                 `json:"user"`
}

type ChatResponse struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	Answer         string `json:"answer"`
	Metadata       struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	} `json:"metadata"`
}

type CompletionRequest struct {
	Inputs       map[string]interface{} `json:"inputs"`
	Query        string                 `json:"query"`
	ResponseMode string                 `json:"response_mode"`
	User         string                 `json:"user"`
}

type CompletionResponse struct {
	MessageID string `json:"message_id"`
	Answer    string `json:"answer"`
	Metadata  struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	} `json:"metadata"`
}

type WorkflowRunRequest struct {
	Inputs       map[string]interface{} `json:"inputs"`
	ResponseMode string                 `json:"response_mode"`
	User         string                 `json:"user"`
}

type WorkflowRunResponse struct {
	TaskID string `json:"task_id"`
	Data   struct {
		ID      string                 `json:"id"`
		Outputs map[string]interface{} `json:"outputs"`
		Status  string                 `json:"status"`
		Error   string                 `json:"error"`
	} `json:"data"`
}

// Chat 使用chat-messages端点（适用于chat模式应用）
func (c *DifyClient) Chat(prompt string, inputs map[string]interface{}) (*ChatResponse, error) {
	url := fmt.Sprintf("%s/chat-messages", c.BaseURL)

	reqBody := ChatRequest{
		Inputs:       inputs,
		Query:        prompt,
		ResponseMode: c.ResponseMode,
		User:         "experiment",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	var lastErr error
	maxR := difyMaxRetries()
	for attempt := 0; attempt <= maxR; attempt++ {
		req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("创建请求失败: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

		resp, err := c.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求失败: %w", err)
			if attempt < maxR && isRetryableDifyErr(err) {
				sleep := difyRetryBackoff(attempt + 1)
				log.Printf("[dify] chat retry=%d/%d sleep=%s err=%v", attempt+1, maxR, sleep, err)
				time.Sleep(sleep)
				continue
			}
			return nil, lastErr
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyStr := string(body)
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500] + "..."
			}
			lastErr = fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, bodyStr)
			if attempt < maxR && isRetryableStatus(resp.StatusCode) {
				sleep := difyRetryBackoff(attempt + 1)
				log.Printf("[dify] chat retry=%d/%d sleep=%s status=%d", attempt+1, maxR, sleep, resp.StatusCode)
				time.Sleep(sleep)
				continue
			}
			return nil, lastErr
		}

		var chatResp ChatResponse
		if err := json.Unmarshal(body, &chatResp); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}
		return &chatResp, nil
	}
	return nil, lastErr
}

// Completion 使用completions端点（适用于completion模式应用）
func (c *DifyClient) Completion(prompt string, inputs map[string]interface{}) (*CompletionResponse, error) {
	url := fmt.Sprintf("%s/completions", c.BaseURL)

	reqBody := CompletionRequest{
		Inputs:       inputs,
		Query:        prompt,
		ResponseMode: c.ResponseMode,
		User:         "experiment",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	var lastErr error
	maxR := difyMaxRetries()
	for attempt := 0; attempt <= maxR; attempt++ {
		req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("创建请求失败: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

		resp, err := c.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求失败: %w", err)
			if attempt < maxR && isRetryableDifyErr(err) {
				sleep := difyRetryBackoff(attempt + 1)
				log.Printf("[dify] completion retry=%d/%d sleep=%s err=%v", attempt+1, maxR, sleep, err)
				time.Sleep(sleep)
				continue
			}
			return nil, lastErr
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyStr := string(body)
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500] + "..."
			}
			lastErr = fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, bodyStr)
			if attempt < maxR && isRetryableStatus(resp.StatusCode) {
				sleep := difyRetryBackoff(attempt + 1)
				log.Printf("[dify] completion retry=%d/%d sleep=%s status=%d", attempt+1, maxR, sleep, resp.StatusCode)
				time.Sleep(sleep)
				continue
			}
			return nil, lastErr
		}

		var completionResp CompletionResponse
		if err := json.Unmarshal(body, &completionResp); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}
		return &completionResp, nil
	}
	return nil, lastErr
}

func (c *DifyClient) WorkflowRun(inputs map[string]interface{}) (string, int, error) {
	url := fmt.Sprintf("%s/workflows/run", c.BaseURL)

	reqBody := WorkflowRunRequest{
		Inputs:       inputs,
		ResponseMode: c.ResponseMode,
		User:         "experiment",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("序列化请求失败: %w", err)
	}

	var lastErr error
	maxR := difyMaxRetries()
	for attempt := 0; attempt <= maxR; attempt++ {
		req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", 0, fmt.Errorf("创建请求失败: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

		resp, err := c.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求失败: %w", err)
			if attempt < maxR && isRetryableDifyErr(err) {
				sleep := difyRetryBackoff(attempt + 1)
				log.Printf("[dify] workflow retry=%d/%d sleep=%s err=%v", attempt+1, maxR, sleep, err)
				time.Sleep(sleep)
				continue
			}
			return "", 0, lastErr
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyStr := string(body)
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500] + "..."
			}
			lastErr = fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, bodyStr)
			if attempt < maxR && isRetryableStatus(resp.StatusCode) {
				sleep := difyRetryBackoff(attempt + 1)
				log.Printf("[dify] workflow retry=%d/%d sleep=%s status=%d", attempt+1, maxR, sleep, resp.StatusCode)
				time.Sleep(sleep)
				continue
			}
			return "", 0, lastErr
		}

		// streaming 模式会返回 SSE；本项目不解析 SSE，建议用 blocking
		var runResp WorkflowRunResponse
		if err := json.Unmarshal(body, &runResp); err != nil {
			return "", 0, fmt.Errorf("解析响应失败: %w", err)
		}

		answer := extractWorkflowAnswer(runResp.Data.Outputs, c.WorkflowOutputKey)
		return answer, 0, nil
	}
	return "", 0, lastErr
}

func extractWorkflowAnswer(outputs map[string]interface{}, outputKey string) string {
	if outputs == nil {
		return ""
	}

	if outputKey != "" {
		if v, ok := outputs[outputKey]; ok {
			if s, ok := v.(string); ok {
				return s
			}
			b, _ := json.Marshal(v)
			return string(b)
		}
	}

	for _, k := range []string{"answer", "text", "output", "result"} {
		if v, ok := outputs[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
			b, _ := json.Marshal(v)
			return string(b)
		}
	}

	for _, v := range outputs {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}

	b, _ := json.Marshal(outputs)
	return string(b)
}

// ChatOrCompletion 智能选择API端点：workflow -> completion -> chat
func (c *DifyClient) ChatOrCompletion(prompt string, inputs map[string]interface{}) (*ChatResponse, error) {
	if c.AppType == "workflow" {
		ans, tokens, err := c.WorkflowRun(inputs)
		if err != nil {
			return nil, err
		}
		var resp ChatResponse
		resp.Answer = ans
		resp.Metadata.Usage.TotalTokens = tokens
		return &resp, nil
	}

	completionResp, err := c.Completion(prompt, inputs)
	if err == nil {
		// 转换为ChatResponse格式
		return &ChatResponse{
			Answer: completionResp.Answer,
			Metadata: struct {
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}{
				Usage: completionResp.Metadata.Usage,
			},
		}, nil
	}
	completionErr := err

	chatResp, chatErr := c.Chat(prompt, inputs)
	if chatErr == nil {
		return chatResp, nil
	}

	// 所有模式都失败，返回详细错误信息
	return nil, fmt.Errorf("所有API端点都失败: appType=%s, completion(%v), chat(%v)",
		c.AppType, completionErr, chatErr)
}
