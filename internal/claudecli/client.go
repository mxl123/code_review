package claudecli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"

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
// 使用 PTY 启动子进程，使 claude CLI 认为 stdout 是终端，从而启用行缓冲实时输出。
func (c *Client) ReviewStream(ctx context.Context, diff string, onToken func(string)) error {
	cmd, err := c.newCmd(ctx, "stream-json", diff)
	if err != nil {
		return err
	}
	log.Printf("claudecli: stream review (prompt-file=%s)", c.systemPromptFile)

	// pty.Start 分配一个伪终端并启动子进程，使其以为 stdout 是 TTY，
	// 从而强制行缓冲输出，实现真正的流式传输。
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("启动 claude 失败: %w", err)
	}
	defer ptmx.Close()

	scanner := bufio.NewScanner(ptmx)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	var prevLen int

	for scanner.Scan() {
		// PTY 在 cooked 模式下会将 \n 转为 \r\n，需要去掉尾部 \r
		raw := strings.TrimRight(scanner.Text(), "\r")
		log.Printf("claudecli: line at %s len=%d prefix=%q", time.Now().Format("15:04:05.000"), len(raw), truncate(raw, 60))
		if len(raw) == 0 {
			continue
		}
		var line streamLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
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

	// PTY slave 关闭后，从 master 读取会返回 EIO，属于正常结束信号。
	if err := scanner.Err(); err != nil && !errors.Is(err, syscall.EIO) {
		_ = cmd.Wait()
		return fmt.Errorf("读取 claude 输出失败: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("claude 退出码 %d", exitErr.ExitCode())
		}
		return fmt.Errorf("claude 执行失败: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
