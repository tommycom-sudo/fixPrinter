package monitor

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultPushPlusURL = "https://www.pushplus.plus/send"
	defaultPushPlusToken = "8745d9c0245d4e96a7e58b8e7de78f1a"
)

// ExecutionResult represents the result of a HTTP request execution
type ExecutionResult struct {
	Success      bool          `json:"success"`
	StatusCode   int           `json:"statusCode"`
	DurationMs   int64         `json:"durationMs"`
	TimedOut     bool          `json:"timedOut"`
	ErrorMessage string        `json:"errorMessage,omitempty"`
	ResponseSize int64         `json:"responseSize"`
	ResponseBody string        `json:"responseBody,omitempty"`
}

// Executor handles the execution of monitoring tasks
type Executor struct {
	client *http.Client
}

// NewExecutor creates a new Executor
func NewExecutor() *Executor {
	return &Executor{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Execute executes a parsed request using the client's timeout settings.
func (e *Executor) Execute(ctx context.Context, req *ParsedRequest) *ExecutionResult {
	result := &ExecutionResult{}
	startTime := time.Now()

	// Validate request
	if err := req.Validate(); err != nil {
		result.ErrorMessage = fmt.Sprintf("Invalid request: %v", err)
		return result
	}

	// Convert to HTTP request
	httpReq, err := req.ToHTTPRequest()
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to create HTTP request: %v", err)
		return result
	}

	httpReq = httpReq.WithContext(ctx)

	// Execute request
	resp, err := e.client.Do(httpReq)
	if err != nil {
		result.DurationMs = time.Since(startTime).Milliseconds()
		if strings.Contains(err.Error(), "context deadline exceeded") ||
		   strings.Contains(err.Error(), "timeout") {
			result.TimedOut = true
			result.ErrorMessage = fmt.Sprintf("Request timeout after %dms", result.DurationMs)
		} else {
			result.ErrorMessage = fmt.Sprintf("Request failed: %v", err)
		}
		return result
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.DurationMs = time.Since(startTime).Milliseconds()
		result.ErrorMessage = fmt.Sprintf("Failed to read response: %v", err)
		return result
	}

	// Set response data
	result.ResponseSize = int64(len(body))
	result.ResponseBody = string(body)
	result.StatusCode = resp.StatusCode
	result.DurationMs = time.Since(startTime).Milliseconds()
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

	// Check if response indicates an error
	if !result.Success {
		result.ErrorMessage = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return result
}

// ExecuteString executes a curl command string directly
func (e *Executor) ExecuteString(ctx context.Context, curlCmd string) *ExecutionResult {
	parsed, err := ParseCURLCommand(curlCmd)
	if err != nil {
		return &ExecutionResult{
			ErrorMessage: fmt.Sprintf("Failed to parse curl command: %v", err),
		}
	}

	return e.Execute(ctx, parsed)
}

// TestPushPlus tests the pushplus notification configuration
func (e *Executor) TestPushPlus(token, title, content string) error {
	// Use default token if not provided
	if token == "" {
		token = defaultPushPlusToken
		log.Printf("[INFO] Using default PushPlus token")
	}

	url := fmt.Sprintf("%s?token=%s&title=%s&content=%s",
		defaultPushPlusURL,
		token,
		url.QueryEscape(title),
		url.QueryEscape(content),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send notification failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("pushplus returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[INFO] PushPlus notification sent successfully, response: %s", string(body))
	return nil
}

// SendAlert sends an alert notification via pushplus
func (e *Executor) SendAlert(token, taskName string, result *ExecutionResult, thresholdMs int64) error {
	// Use default token if not provided
	if token == "" {
		token = defaultPushPlusToken
		log.Printf("[INFO] Using default PushPlus token for alert")
	}

	log.Printf("[INFO] Sending PushPlus alert for task '%s' (token: %s)", taskName, maskToken(token))

	var content strings.Builder
	content.WriteString(fmt.Sprintf("【API 监控告警】\n\n"))
	content.WriteString(fmt.Sprintf("时间: %s\n", time.Now().Format("15:04:05")))
	content.WriteString(fmt.Sprintf("任务名称: %s\n", taskName))
	content.WriteString(fmt.Sprintf("状态: 执行超时/失败\n"))
	content.WriteString(fmt.Sprintf("耗时: %d ms\n", result.DurationMs))
	content.WriteString(fmt.Sprintf("阈值: %d ms\n", thresholdMs))

	if result.TimedOut {
		content.WriteString(fmt.Sprintf("原因: 请求超时\n"))
	} else if result.ErrorMessage != "" {
		content.WriteString(fmt.Sprintf("原因: %s\n", result.ErrorMessage))
	} else if !result.Success {
		content.WriteString(fmt.Sprintf("原因: HTTP %d\n", result.StatusCode))
	}

	title := fmt.Sprintf("超时 %dms %s ", result.DurationMs, taskName)
	url := fmt.Sprintf("%s?token=%s&title=%s&content=%s",
		defaultPushPlusURL,
		token,
		url.QueryEscape(title),
		url.QueryEscape(content.String()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create alert request failed: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send alert failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("pushplus returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[INFO] PushPlus alert sent successfully for task '%s' (token: %s), response: %s", taskName, maskToken(token), string(body))
	return nil
}

// maskToken masks the pushplus token for logging (shows first 4 and last 4 chars)
func maskToken(token string) string {
	if token == "" {
		return "(empty)"
	}
	if len(token) <= 8 {
		return token
	}
	return token[:4] + "****" + token[len(token)-4:]
}
