package openai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	goopenai "github.com/sashabaranov/go-openai"

	"code-review/internal/prompt"
)

// debugTransport 在响应不是 JSON 时记录原始响应体，帮助排查 URL 配置问题。
type debugTransport struct {
	inner http.RoundTripper
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	ct := resp.Header.Get("Content-Type")
	// event-stream 是正常的流式响应，跳过 body 读取，否则会将整个流缓存到内存，破坏逐 token 推送
	if !strings.Contains(ct, "json") && !strings.Contains(ct, "event-stream") {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// 截断到 500 字节避免日志过长
		preview := string(body)
		if len(preview) > 500 {
			preview = preview[:500] + "...(truncated)"
		}
		log.Printf("WARN sub2api returned non-JSON response: status=%s content-type=%q url=%s body=%s",
			resp.Status, ct, req.URL, preview)
		// 还原 body，让 go-openai 继续读取（会报 JSON 解析错误，但日志里已有原始内容）
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, nil
}

type Client struct {
	inner *goopenai.Client
	model string
}

// NewClient 创建 OpenAI 客户端。
// baseURL 为空时使用官方接口地址，非空时（如 sub2api）使用自定义地址。
func NewClient(apiKey, model, baseURL string) *Client {
	cfg := goopenai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	cfg.HTTPClient = &http.Client{
		Transport: &debugTransport{inner: http.DefaultTransport},
	}
	return &Client{
		inner: goopenai.NewClientWithConfig(cfg),
		model: model,
	}
}

func (c *Client) messages(diff string) []goopenai.ChatCompletionMessage {
	return []goopenai.ChatCompletionMessage{
		{Role: goopenai.ChatMessageRoleSystem, Content: prompt.SystemPrompt},
		{Role: goopenai.ChatMessageRoleUser, Content: prompt.UserPromptPrefix + diff},
	}
}

func (c *Client) Review(ctx context.Context, diff string) (string, error) {
	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model:       c.model,
		Temperature: 0.2,
		Messages:    c.messages(diff),
	})
	if err != nil {
		return "", fmt.Errorf("openai chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

// ReviewStream 以流式方式获取审查结果，每收到一个 token 就调用 onToken 回调。
func (c *Client) ReviewStream(ctx context.Context, diff string, onToken func(string)) error {
	stream, err := c.inner.CreateChatCompletionStream(ctx, goopenai.ChatCompletionRequest{
		Model:       c.model,
		Temperature: 0.2,
		Stream:      true,
		Messages:    c.messages(diff),
	})
	if err != nil {
		return fmt.Errorf("openai stream: %w", err)
	}
	defer stream.Close()

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream recv: %w", err)
		}
		if len(resp.Choices) > 0 {
			if token := resp.Choices[0].Delta.Content; token != "" {
				onToken(token)
			}
		}
	}
}
