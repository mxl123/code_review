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
	systemPromptFile string // 审查规则文件，通过 --system-prompt-file 加载，不影响项目开发
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

// newCmd 构造 claude 子进程：
//   - --system-prompt-file 替换全局 CLAUDE.md，隔离本机开发环境配置
//   - cmd.Dir 不设置，继承服务工作目录（需能找到 systemPromptFile）
func (c *Client) newCmd(ctx context.Context, outputFormat, diff string) (*exec.Cmd, error) {
	if _, err := os.Stat(c.systemPromptFile); err != nil {
		return nil, fmt.Errorf("审查规则文件不存在 %q: %w", c.systemPromptFile, err)
	}

	args := []string{
		"-p", prompt.UserPromptPrefix + diff,
		"--system-prompt-file", c.systemPromptFile,
		"--output-format", outputFormat,
	}
	if outputFormat == "stream-json" {
		args = append(args, "--verbose")
	}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	return exec.CommandContext(ctx, c.binPath, args...), nil
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
	Event   *struct {
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	Message *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// Review 以非流式方式调用 claude CLI，返回完整审查结果。
func (c *Client) Review(ctx context.Context, diff string) (string, error) {
	cmd, err := c.newCmd(ctx, "json", diff)
	if err != nil {
		return "", err
	}
	log.Printf("claudecli: review (prompt-file=%s)", c.systemPromptFile)

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
	log.Printf("claudecli: stream review (prompt-file=%s)", c.systemPromptFile)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("获取 stdout pipe 失败: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 claude 失败: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	var prevLen int

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var line streamLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
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
