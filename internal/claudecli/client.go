package claudecli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"code-review/internal/prompt"
)

type Client struct {
	binPath          string // claude 二进制路径，默认 "claude"
	model            string // 可选；为空时不传 --model
	systemPromptFile string // 审查规则文件路径
}

func NewClient(binPath, model, systemPromptFile string) *Client {
	if binPath == "" {
		binPath = "claude"
	}
	if systemPromptFile == "" {
		systemPromptFile = "prompts/review-system-prompt.md"
	}
	return &Client{binPath: binPath, model: model, systemPromptFile: systemPromptFile}
}

// newCmd 构造隔离的 exec.Cmd：
//   - --system-prompt-file 替换全局 CLAUDE.md，屏蔽本机 skill/rule 干扰
//   - Dir=/tmp 避免捡到项目级 CLAUDE.md
func (c *Client) newCmd(ctx context.Context, outputFormat, userPrompt string) (*exec.Cmd, error) {
	// 验证规则文件存在，给出明确报错
	if _, err := os.Stat(c.systemPromptFile); err != nil {
		return nil, fmt.Errorf("审查规则文件不存在 %q: %w", c.systemPromptFile, err)
	}

	args := []string{
		"-p", prompt.UserPromptPrefix + userPrompt,
		"--system-prompt-file", c.systemPromptFile,
		"--output-format", outputFormat,
	}
	if outputFormat == "stream-json" {
		args = append(args, "--verbose")
	}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	cmd := exec.CommandContext(ctx, c.binPath, args...)
	cmd.Dir = os.TempDir() // 中性工作目录，避免捡到任何项目 CLAUDE.md
	return cmd, nil
}

// jsonResult 对应 --output-format json 的顶层输出结构。
type jsonResult struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// streamLine 对应 --output-format stream-json 的每行 NDJSON 结构。
type streamLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	// stream_event 格式（delta）
	Event *struct {
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	// assistant 格式（累积文本）
	Message *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	// result 格式（终止行）
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// Review 以非流式方式调用 claude CLI，返回完整审查结果。
func (c *Client) Review(ctx context.Context, diff string) (string, error) {
	cmd, err := c.newCmd(ctx, "json", diff)
	if err != nil {
		return "", err
	}
	log.Printf("claudecli: review project via %s (prompt-file=%s)", c.binPath, c.systemPromptFile)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("claude 退出码 %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return "", fmt.Errorf("claude 执行失败: %w", err)
	}

	var res jsonResult
	if err := json.Unmarshal(out, &res); err != nil {
		preview := string(out)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return "", fmt.Errorf("解析 claude 输出失败: %w (raw: %s)", err, preview)
	}
	if res.IsError {
		return "", fmt.Errorf("claude 返回错误: %s", res.Result)
	}
	return res.Result, nil
}

// ReviewStream 以流式方式调用 claude CLI，每收到一个文本增量就调用 onToken。
func (c *Client) ReviewStream(ctx context.Context, diff string, onToken func(string)) error {
	cmd, err := c.newCmd(ctx, "stream-json", diff)
	if err != nil {
		return err
	}
	log.Printf("claudecli: stream review via %s (prompt-file=%s)", c.binPath, c.systemPromptFile)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("获取 stdout pipe 失败: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 claude 失败: %w", err)
	}

	// 1 MB 行缓冲，防止大 diff 导致 scanner token too long
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	var prevLen int // 记录已推送的累积文本长度（assistant 格式用）

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var line streamLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue // 跳过非 JSON 行
		}

		switch line.Type {
		case "stream_event":
			if line.Event != nil && line.Event.Delta != nil &&
				line.Event.Delta.Type == "text_delta" && line.Event.Delta.Text != "" {
				onToken(line.Event.Delta.Text)
			}

		case "assistant":
			if line.Message == nil {
				continue
			}
			var full strings.Builder
			for _, blk := range line.Message.Content {
				if blk.Type == "text" {
					full.WriteString(blk.Text)
				}
			}
			fullStr := full.String()
			if len(fullStr) > prevLen {
				onToken(fullStr[prevLen:])
				prevLen = len(fullStr)
			}

		case "result":
			if line.IsError {
				_ = cmd.Wait()
				return fmt.Errorf("claude 审查出错: %s", line.Result)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		return fmt.Errorf("读取 claude 输出失败: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stderr := strings.TrimSpace(stderrBuf.String())
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("claude 退出码 %d: %s", exitErr.ExitCode(), stderr)
		}
		return fmt.Errorf("claude 执行失败: %w (stderr: %s)", err, stderr)
	}
	return nil
}
