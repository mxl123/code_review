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
	systemPromptFile string // 提示词文件模式：规则文件路径
	skill            string // 技能模式：~/.claude/commands/ 中的 skill 名称（不含 /）
}

func NewClient(binPath, model, systemPromptFile, skill string) *Client {
	if binPath == "" {
		binPath = "claude"
	}
	return &Client{
		binPath:          binPath,
		model:            model,
		systemPromptFile: systemPromptFile,
		skill:            skill,
	}
}

// newCmd 根据配置构造子进程：
//
//   - 技能模式（CLAUDE_SKILL 已设置）：
//     prompt = "/<skill>\n\n<diff>"，不使用 --system-prompt-file，
//     由 skill 定义审查逻辑，工作目录设为 /tmp 避免捡到项目 CLAUDE.md。
//
//   - 提示词文件模式（默认）：
//     使用 --system-prompt-file 完全替换 CLAUDE.md，完全隔离本机配置。
func (c *Client) newCmd(ctx context.Context, outputFormat, diff string) (*exec.Cmd, error) {
	var p string
	var extraArgs []string

	if c.skill != "" {
		// 技能模式：skill 的规则写入用户消息，--system-prompt-file 指向空文件屏蔽 CLAUDE.md
		p = "/" + c.skill + "\n\n" + prompt.UserPromptPrefix + diff
		extraArgs = []string{"--system-prompt-file", "/dev/null"}
	} else {
		// 提示词文件模式：验证文件存在后加 --system-prompt-file
		if c.systemPromptFile == "" {
			c.systemPromptFile = "prompts/review-system-prompt.md"
		}
		if _, err := os.Stat(c.systemPromptFile); err != nil {
			return nil, fmt.Errorf("审查规则文件不存在 %q: %w", c.systemPromptFile, err)
		}
		p = prompt.UserPromptPrefix + diff
		extraArgs = []string{"--system-prompt-file", c.systemPromptFile}
	}

	args := []string{"-p", p, "--output-format", outputFormat}
	args = append(args, extraArgs...)
	if outputFormat == "stream-json" {
		args = append(args, "--verbose")
	}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	cmd := exec.CommandContext(ctx, c.binPath, args...)
	cmd.Dir = os.TempDir() // 中性目录，避免捡到项目级 CLAUDE.md
	return cmd, nil
}

func (c *Client) mode() string {
	if c.skill != "" {
		return "skill=" + c.skill
	}
	return "prompt-file=" + c.systemPromptFile
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
	log.Printf("claudecli: review (%s)", c.mode())

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
	log.Printf("claudecli: stream review (%s)", c.mode())

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
