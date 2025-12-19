package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// MemOS Product API（本地自建）文档参考：
// https://github.com/MemTensor/MemOS

type MemOSClient struct {
	baseURL string
	http    *http.Client
	topK    int
}

func NewMemOSClient(baseURL string, topK int) *MemOSClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if topK <= 0 {
		topK = 5
	}
	return &MemOSClient{
		baseURL: baseURL,
		topK:    topK,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *MemOSClient) Enabled() bool {
	return c != nil && c.baseURL != ""
}

type memosBaseResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Detail  string          `json:"detail"` // FastAPI traceback
}

func (c *MemOSClient) RegisterUser(ctx context.Context, userID string) error {
	if !c.Enabled() {
		return fmt.Errorf("memos disabled")
	}
	body := map[string]any{
		"user_id": userID,
	}
	_, err := c.post(ctx, "/product/users/register", body)
	return err
}

type MemOSSearchItem struct {
	ID      string
	Content string
	Score   float64
}

func (c *MemOSClient) Search(ctx context.Context, userID, query string) ([]MemOSSearchItem, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("memos disabled")
	}
	if memosDebugEnabled() {
		log.Printf("[memos] search req user=%s query=%s", userID, truncate(query, 300))
	}
	body := map[string]any{
		"user_id": userID,
		"query":   query,
		"top_k":   c.topK,
	}
	raw, err := c.post(ctx, "/product/search", body)
	if err != nil {
		if memosDebugEnabled() {
			log.Printf("[memos] search resp err=%v", err)
		}
		return nil, err
	}
	if memosDebugEnabled() {
		log.Printf("[memos] search resp raw=%s", truncate(string(raw), 600))
	}

	// SearchResponse.data 是一个 dict，结构随版本变化；这里做最大容错解析
	var base memosBaseResp
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, err
	}
	if base.Detail != "" && base.Code == 0 && base.Message == "" {
		return nil, fmt.Errorf("memos search error: %s", base.Detail)
	}
	// 可能是 BaseResponse[dict]：{code,message,data:{...}}
	// data 里通常会含 text_mem 或 references 等字段
	var data map[string]any
	_ = json.Unmarshal(base.Data, &data)

	var out []MemOSSearchItem
	// 1) 兼容 data.text_mem: []string / []object
	if v, ok := data["text_mem"]; ok {
		switch vv := v.(type) {
		case []any:
			for _, it := range vv {
				switch t := it.(type) {
				case string:
					out = append(out, MemOSSearchItem{Content: t})
				case map[string]any:
					out = append(out, MemOSSearchItem{
						ID:      toString(t["id"]),
						Content: toString(t["content"]),
						Score:   toFloat(t["score"]),
					})
				}
			}
		}
	}
	// 2) 兼容 data.references: []object
	if len(out) == 0 {
		if v, ok := data["references"]; ok {
			if arr, ok := v.([]any); ok {
				for _, it := range arr {
					if m, ok := it.(map[string]any); ok {
						out = append(out, MemOSSearchItem{
							ID:      toString(m["id"]),
							Content: toString(m["content"]),
							Score:   toFloat(m["score"]),
						})
					}
				}
			}
		}
	}

	// 兜底：如果 data 本身就是 list
	if len(out) == 0 {
		var arr []any
		if err := json.Unmarshal(base.Data, &arr); err == nil {
			for _, it := range arr {
				if s, ok := it.(string); ok {
					out = append(out, MemOSSearchItem{Content: s})
				}
			}
		}
	}

	if memosDebugEnabled() {
		log.Printf("[memos] search parsed hits=%d", len(out))
	}
	return out, nil
}

func (c *MemOSClient) AddMemory(ctx context.Context, userID, content, source string) error {
	if !c.Enabled() {
		return fmt.Errorf("memos disabled")
	}
	if memosDebugEnabled() {
		log.Printf("[memos] add req user=%s source=%s content=%s", userID, truncate(source, 120), truncate(content, 300))
	}
	body := map[string]any{
		"user_id":        userID,
		"memory_content": content,
		"source":         source,
	}
	raw, err := c.post(ctx, "/product/add", body)
	if memosDebugEnabled() {
		if err != nil {
			log.Printf("[memos] add resp err=%v", err)
		} else {
			log.Printf("[memos] add resp raw=%s", truncate(string(raw), 300))
		}
	}
	return err
}

func (c *MemOSClient) post(ctx context.Context, path string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("memos http=%d body=%s", resp.StatusCode, truncate(string(raw), 300))
	}
	// MemOS 有时会返回 200 + {"detail":"Traceback ..."}，这里统一当作错误，避免静默失败
	var maybe map[string]any
	if err := json.Unmarshal(raw, &maybe); err == nil {
		if d, ok := maybe["detail"]; ok {
			ds := toString(d)
			if strings.TrimSpace(ds) != "" {
				return nil, fmt.Errorf("memos detail=%s", truncate(ds, 300))
			}
		}
	}
	return raw, nil
}

func memosDebugEnabled() bool {
	return os.Getenv("MEMOS_DEBUG") == "1"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}
