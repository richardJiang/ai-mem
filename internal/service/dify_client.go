package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// 尝试解析错误信息
		var errResp map[string]interface{}
		if json.Unmarshal(body, &errResp) == nil {
			if msg, ok := errResp["message"].(string); ok {
				return nil, fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, msg)
			}
		}
		// 如果无法解析，返回原始body（截取前500字符避免过长）
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "..."
		}
		return nil, fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, bodyStr)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &chatResp, nil
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

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// 尝试解析错误信息
		var errResp map[string]interface{}
		if json.Unmarshal(body, &errResp) == nil {
			if msg, ok := errResp["message"].(string); ok {
				return nil, fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, msg)
			}
		}
		// 如果无法解析，返回原始body（截取前500字符避免过长）
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "..."
		}
		return nil, fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, bodyStr)
	}

	var completionResp CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completionResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &completionResp, nil
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

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", 0, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "..."
		}
		return "", 0, fmt.Errorf("API返回错误: %d, %s", resp.StatusCode, bodyStr)
	}

	// streaming 模式会返回 SSE；本项目不解析 SSE，建议用 blocking
	var runResp WorkflowRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&runResp); err != nil {
		return "", 0, fmt.Errorf("解析响应失败: %w", err)
	}

	answer := extractWorkflowAnswer(runResp.Data.Outputs, c.WorkflowOutputKey)
	return answer, 0, nil
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
