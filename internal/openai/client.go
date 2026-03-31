package openai

import (
	"context"
	"fmt"

	goopenai "github.com/sashabaranov/go-openai"
)

const systemPrompt = `You are an expert software engineer performing a code review. Your goal is to provide actionable, constructive feedback. Focus on:

1. **BUGS & CORRECTNESS** - logic errors, off-by-one errors, nil pointer risks, race conditions
2. **SECURITY** - injection vulnerabilities, improper input validation, secret exposure, insecure defaults
3. **PERFORMANCE** - O(n²) operations, unnecessary allocations, blocking calls in hot paths
4. **MAINTAINABILITY** - overly complex logic, missing error handling, poor naming, non-obvious code without comments
5. **BEST PRACTICES** - language idioms, test coverage gaps, API design issues

Format your response as Markdown suitable for posting on a GitLab merge request.
Use headers (##) for each category where you have findings. Omit categories with no issues.
Start each finding with the file path in bold. Be specific and concise.
If the changes look good overall, say so briefly at the end.`

type Client struct {
	inner *goopenai.Client
	model string
}

func NewClient(apiKey, model string) *Client {
	return &Client{
		inner: goopenai.NewClient(apiKey),
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
