package openai

import (
	"context"
	"fmt"

	goopenai "github.com/sashabaranov/go-openai"
)

const systemPrompt = `你是一位资深软件工程师，正在对 Merge Request 进行代码审查。请用**中文**提供具体、可操作的改进建议，重点关注以下方面：

1. **缺陷与正确性** — 逻辑错误、边界问题、空指针风险、竞态条件
2. **安全性** — 注入漏洞、输入校验缺失、敏感信息泄露、不安全的默认配置
3. **性能** — O(n²) 操作、不必要的内存分配、热路径中的阻塞调用
4. **可维护性** — 过于复杂的逻辑、缺少错误处理、命名不清晰、非显而易见的代码缺少注释
5. **最佳实践** — 语言惯用写法、测试覆盖不足、API 设计问题

输出格式要求：
- 使用 Markdown 格式，适合直接发布到 GitLab MR 评论
- 每个有问题的分类使用二级标题（##），没有问题的分类直接省略
- 每条问题以**文件路径加粗**开头，说明具体位置和改进建议
- 如果整体代码质量良好，在末尾简短说明
- **所有内容必须使用中文输出**`

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
	return &Client{
		inner: goopenai.NewClientWithConfig(cfg),
		model: model,
	}
}

func (c *Client) Review(ctx context.Context, diff string) (string, error) {
	resp, err := c.inner.CreateChatCompletion(ctx, goopenai.ChatCompletionRequest{
		Model:       c.model,
		Temperature: 0.2,
		Messages: []goopenai.ChatCompletionMessage{
			{Role: goopenai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: goopenai.ChatMessageRoleUser, Content: "Please review the following code diff from a merge request:\n\n" + diff},
		},
	})
	if err != nil {
		return "", fmt.Errorf("openai chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}
