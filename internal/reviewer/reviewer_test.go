package reviewer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"code-review/internal/gitlab"
)

// ---- mock 实现 ----

type mockGitLab struct {
	changes         *gitlab.MRChanges
	getMRChangesErr error
	postCommentErr  error
	postedBodies    []string
}

func (m *mockGitLab) GetMRChanges(_, _ int) (*gitlab.MRChanges, error) {
	return m.changes, m.getMRChangesErr
}

func (m *mockGitLab) PostComment(_, _ int, body string) error {
	m.postedBodies = append(m.postedBodies, body)
	return m.postCommentErr
}

type mockAI struct {
	result string
	err    error
}

func (m *mockAI) Review(_ context.Context, _ string) (string, error) {
	return m.result, m.err
}

// ---- 辅助函数 ----

func makeChanges(files ...gitlab.Change) *gitlab.MRChanges {
	return &gitlab.MRChanges{Changes: files}
}

func change(path, diff string) gitlab.Change {
	return gitlab.Change{NewPath: path, Diff: diff}
}

func deletedChange(path string) gitlab.Change {
	return gitlab.Change{NewPath: path, Diff: "x", DeletedFile: true}
}

// ---- buildDiff 测试（纯函数，白盒）----

func TestBuildDiff_Empty(t *testing.T) {
	got, truncated := buildDiff(nil, 10000)
	if got != "" || truncated {
		t.Errorf("buildDiff(nil) = (%q, %v), want ('', false)", got, truncated)
	}
}

func TestBuildDiff_DeletedFileSkipped(t *testing.T) {
	got, truncated := buildDiff([]gitlab.Change{deletedChange("gone.go")}, 10000)
	if got != "" || truncated {
		t.Errorf("deleted file should be skipped, got (%q, %v)", got, truncated)
	}
}

func TestBuildDiff_SingleFile(t *testing.T) {
	got, truncated := buildDiff([]gitlab.Change{change("main.go", "+hello")}, 10000)
	if !strings.Contains(got, "--- File: main.go ---") {
		t.Errorf("expected file header in output, got: %q", got)
	}
	if !strings.Contains(got, "+hello") {
		t.Errorf("expected diff content in output, got: %q", got)
	}
	if truncated {
		t.Error("should not be truncated")
	}
}

func TestBuildDiff_TruncatesAtFileBoundary(t *testing.T) {
	// 第一个文件约 30 字节，maxBytes 设为 40，第二个文件应被截断
	c1 := change("a.go", "aaa")
	c2 := change("b.go", "bbb")
	got, truncated := buildDiff([]gitlab.Change{c1, c2}, 40)
	if !strings.Contains(got, "a.go") {
		t.Error("first file should be present")
	}
	if strings.Contains(got, "b.go") {
		t.Error("second file should be truncated")
	}
	if !truncated {
		t.Error("truncated should be true")
	}
}

func TestBuildDiff_MultipleFiles(t *testing.T) {
	changes := []gitlab.Change{
		change("a.go", "aaa"),
		change("b.go", "bbb"),
		change("c.go", "ccc"),
	}
	got, truncated := buildDiff(changes, 100000)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if !strings.Contains(got, name) {
			t.Errorf("expected %s in output", name)
		}
	}
	if truncated {
		t.Error("should not be truncated")
	}
}

func TestBuildDiff_FallsBackToOldPath(t *testing.T) {
	c := gitlab.Change{OldPath: "old.go", NewPath: "", Diff: "xxx"}
	got, _ := buildDiff([]gitlab.Change{c}, 10000)
	if !strings.Contains(got, "old.go") {
		t.Errorf("should fall back to OldPath, got: %q", got)
	}
}

func TestBuildDiff_UsesNewPathOverOldPath(t *testing.T) {
	c := gitlab.Change{OldPath: "old.go", NewPath: "new.go", Diff: "xxx"}
	got, _ := buildDiff([]gitlab.Change{c}, 10000)
	if !strings.Contains(got, "new.go") {
		t.Errorf("should prefer NewPath, got: %q", got)
	}
	if strings.Contains(got, "old.go") {
		t.Errorf("should not contain OldPath when NewPath is set, got: %q", got)
	}
}

// ---- Process 测试 ----

func TestProcess_HappyPath(t *testing.T) {
	gl := &mockGitLab{changes: makeChanges(change("main.go", "+hello world"))}
	ai := &mockAI{result: "代码整体 LGTM"}

	rev := New(gl, ai, 100000)
	rev.Process(1, 2)

	if len(gl.postedBodies) != 1 {
		t.Fatalf("expected 1 comment posted, got %d", len(gl.postedBodies))
	}
	body := gl.postedBodies[0]
	if !strings.Contains(body, "Automated Code Review") {
		t.Errorf("comment should contain header, got: %q", body)
	}
	if !strings.Contains(body, "代码整体 LGTM") {
		t.Errorf("comment should contain AI review, got: %q", body)
	}
}

func TestProcess_GitLabError(t *testing.T) {
	gl := &mockGitLab{getMRChangesErr: errors.New("not found")}
	ai := &mockAI{result: "review"}

	rev := New(gl, ai, 100000)
	rev.Process(1, 2)

	if len(gl.postedBodies) != 0 {
		t.Errorf("no comment should be posted on GitLab error, got %d", len(gl.postedBodies))
	}
}

func TestProcess_EmptyDiff(t *testing.T) {
	// 全是删除文件，buildDiff 返回空字符串
	gl := &mockGitLab{changes: makeChanges(deletedChange("gone.go"))}
	ai := &mockAI{result: "review"}

	rev := New(gl, ai, 100000)
	rev.Process(1, 2)

	if len(gl.postedBodies) != 0 {
		t.Errorf("no comment should be posted for empty diff, got %d", len(gl.postedBodies))
	}
}

func TestProcess_AIError(t *testing.T) {
	gl := &mockGitLab{changes: makeChanges(change("main.go", "+x"))}
	ai := &mockAI{err: errors.New("rate limited")}

	rev := New(gl, ai, 100000)
	rev.Process(1, 2)

	if len(gl.postedBodies) != 1 {
		t.Fatalf("expected error comment, got %d", len(gl.postedBodies))
	}
	if !strings.Contains(gl.postedBodies[0], "review failed") {
		t.Errorf("error comment should say review failed, got: %q", gl.postedBodies[0])
	}
}

func TestProcess_TruncatedDiff(t *testing.T) {
	// 两个文件，maxDiffBytes 只够放第一个（每块约 121 字节，设为 150 只容纳第一块）
	c1 := change("a.go", strings.Repeat("a", 100))
	c2 := change("b.go", strings.Repeat("b", 100))
	gl := &mockGitLab{changes: makeChanges(c1, c2)}
	ai := &mockAI{result: "ok"}

	rev := New(gl, ai, 150) // 容纳第一个文件块（121 字节），截断第二个
	rev.Process(1, 2)

	if len(gl.postedBodies) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(gl.postedBodies))
	}
	if !strings.Contains(gl.postedBodies[0], "truncated") {
		t.Errorf("comment should mention truncation, got: %q", gl.postedBodies[0])
	}
}

func TestProcess_PostCommentError(t *testing.T) {
	// PostComment 失败时不应 panic，仅记录日志
	gl := &mockGitLab{
		changes:        makeChanges(change("main.go", "+x")),
		postCommentErr: errors.New("gitlab down"),
	}
	ai := &mockAI{result: "ok"}

	rev := New(gl, ai, 100000)
	rev.Process(1, 2) // 不应 panic
}
