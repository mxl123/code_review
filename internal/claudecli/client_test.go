package claudecli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// writeFakeClaudeScript 在临时目录写一个 shell 脚本，模拟 claude 二进制。
// stdout 输出 output（含真实换行符），退出码为 exitCode。
func writeFakeClaudeScript(t *testing.T, output string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	dataPath := dir + "/output.txt"
	if err := os.WriteFile(dataPath, []byte(output), 0644); err != nil {
		t.Fatalf("write fake claude data: %v", err)
	}
	scriptPath := dir + "/claude"
	script := fmt.Sprintf("#!/bin/sh\ncat %q\nexit %d\n", dataPath, exitCode)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}
	return scriptPath
}

// newTestClient 创建带假 claude 脚本的 Client。
func newTestClient(t *testing.T, output string, exitCode int) *Client {
	t.Helper()
	return NewClient(writeFakeClaudeScript(t, output, exitCode), "")
}

// ---- Review 测试 ----

func TestReview_Success(t *testing.T) {
	fixture := `{"type":"result","subtype":"success","result":"LGTM","is_error":false}`
	c := newTestClient(t, fixture, 0)

	got, err := c.Review(context.Background(), "diff here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "LGTM" {
		t.Errorf("result = %q, want LGTM", got)
	}
}

func TestReview_IsErrorTrue(t *testing.T) {
	fixture := `{"type":"result","subtype":"error","result":"rate limit","is_error":true}`
	c := newTestClient(t, fixture, 0)

	_, err := c.Review(context.Background(), "diff")
	if err == nil {
		t.Fatal("expected error for is_error:true")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error %q should mention rate limit", err)
	}
}

func TestReview_NonZeroExitCode(t *testing.T) {
	c := newTestClient(t, "", 1)

	_, err := c.Review(context.Background(), "diff")
	if err == nil {
		t.Fatal("expected error for non-zero exit code")
	}
}

func TestReview_InvalidJSON(t *testing.T) {
	c := newTestClient(t, "not json", 0)

	_, err := c.Review(context.Background(), "diff")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---- ReviewStream 测试 ----

func TestReviewStream_StreamEventDelta(t *testing.T) {
	// stream_event 格式：每行是独立增量
	lines := strings.Join([]string{
		`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"Hello"}}}`,
		`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":" world"}}}`,
		`{"type":"result","subtype":"success","result":"Hello world","is_error":false}`,
	}, "\n") + "\n"

	c := newTestClient(t, lines, 0)

	var tokens []string
	err := c.ReviewStream(context.Background(), "diff", func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(tokens, ""); got != "Hello world" {
		t.Errorf("reconstructed = %q, want 'Hello world'", got)
	}
}

func TestReviewStream_AssistantCumulativeText(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`,
		`{"type":"result","subtype":"success","result":"Hello world","is_error":false}`,
	}, "\n") + "\n"

	c := newTestClient(t, lines, 0)

	var tokens []string
	err := c.ReviewStream(context.Background(), "diff", func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Join(tokens, ""); got != "Hello world" {
		t.Errorf("reconstructed = %q, want 'Hello world'", got)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 delta emissions, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != "Hello" || tokens[1] != " world" {
		t.Errorf("deltas = %v, want ['Hello', ' world']", tokens)
	}
}

func TestReviewStream_ErrorResult(t *testing.T) {
	lines := `{"type":"result","subtype":"error_max_turns","result":"timeout","is_error":true}` + "\n"
	c := newTestClient(t, lines, 0)

	err := c.ReviewStream(context.Background(), "diff", func(_ string) {})
	if err == nil {
		t.Fatal("expected error for is_error:true result")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error %q should mention timeout", err)
	}
}

func TestReviewStream_NonJSONLinesSkipped(t *testing.T) {
	lines := strings.Join([]string{
		`not json at all`,
		`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"ok"}}}`,
		`{"type":"result","subtype":"success","result":"ok","is_error":false}`,
	}, "\n") + "\n"

	c := newTestClient(t, lines, 0)

	var tokens []string
	err := c.ReviewStream(context.Background(), "diff", func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(tokens, "") != "ok" {
		t.Errorf("got tokens %v, want ['ok']", tokens)
	}
}
