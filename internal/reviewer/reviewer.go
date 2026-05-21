package reviewer

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"code-review/internal/gitlab"
)

// AIClient 定义 Reviewer 所需的 AI 审查操作，方便测试时注入 mock。
type AIClient interface {
	Review(ctx context.Context, diff string) (string, error)
	ReviewStream(ctx context.Context, diff string, onToken func(string)) error
}

type Reviewer struct {
	gitlab       gitlab.GitLabClient
	openai       AIClient
	maxDiffBytes int
}

func New(gl gitlab.GitLabClient, ai AIClient, maxDiffBytes int) *Reviewer {
	return &Reviewer{
		gitlab:       gl,
		openai:       ai,
		maxDiffBytes: maxDiffBytes,
	}
}

// Process 获取 MR diff，调用 OpenAI 进行审查，并将结果以评论形式发布到 MR。
// 异步执行，所有错误只记录日志，不返回。
func (r *Reviewer) Process(projectID, mrIID int) {
	log.Printf("processing MR !%d in project %d", mrIID, projectID)

	changes, err := r.gitlab.GetMRChanges(projectID, mrIID)
	if err != nil {
		log.Printf("ERROR get MR changes project=%d mr=%d: %v", projectID, mrIID, err)
		return
	}

	diff, truncated := buildDiff(changes.Changes, r.maxDiffBytes)
	if strings.TrimSpace(diff) == "" {
		log.Printf("no reviewable diff for MR !%d, skipping", mrIID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	review, err := r.openai.Review(ctx, diff)
	if err != nil {
		log.Printf("ERROR openai review project=%d mr=%d: %v", projectID, mrIID, err)
		errComment := fmt.Sprintf("## Automated Code Review\n\n> ⚠️ Automated review failed: %v\n\n*Please retry or review manually.*", err)
		_ = r.gitlab.PostComment(projectID, mrIID, errComment)
		return
	}

	var sb strings.Builder
	sb.WriteString("## Automated Code Review\n\n")
	if truncated {
		sb.WriteString("> **Note:** The diff was truncated due to size. Only the first files are shown.\n\n")
	}
	sb.WriteString(review)
	sb.WriteString("\n\n---\n*This review was generated automatically by AI. Always apply human judgment.*")

	if err := r.gitlab.PostComment(projectID, mrIID, sb.String()); err != nil {
		log.Printf("ERROR post comment project=%d mr=%d: %v", projectID, mrIID, err)
	} else {
		log.Printf("review posted for MR !%d in project %d", mrIID, projectID)
	}
}

// RunReview 获取 MR diff 并返回审查结果，不发布到 GitLab，供主动触发 API 使用。
// gl 参数允许传入使用用户自己 token 的临时客户端。
func (r *Reviewer) RunReview(gl gitlab.GitLabClient, projectID, mrIID int) (string, error) {
	changes, err := gl.GetMRChanges(projectID, mrIID)
	if err != nil {
		return "", fmt.Errorf("获取 MR 变更失败: %w", err)
	}

	diff, truncated := buildDiff(changes.Changes, r.maxDiffBytes)
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("没有可审查的代码变更（可能全是删除操作）")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := r.openai.Review(ctx, diff)
	if err != nil {
		return "", fmt.Errorf("AI 审查失败: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("## 自动代码审查\n\n")
	if truncated {
		sb.WriteString("> **注意：** 由于变更较大，仅展示部分文件的审查结果。\n\n")
	}
	sb.WriteString(result)
	sb.WriteString("\n\n---\n*本审查由 AI 自动生成，请结合实际情况判断。*")
	return sb.String(), nil
}


// RunReviewStream 流式执行审查，每收到一个 token 就调用 onToken，供 SSE 接口使用。
func (r *Reviewer) RunReviewStream(gl gitlab.GitLabClient, projectID, mrIID int, onToken func(string)) error {
	changes, err := gl.GetMRChanges(projectID, mrIID)
	if err != nil {
		return fmt.Errorf("获取 MR 变更失败: %w", err)
	}

	diff, truncated := buildDiff(changes.Changes, r.maxDiffBytes)
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("没有可审查的代码变更（可能全是删除操作）")
	}

	if truncated {
		onToken("> **注意：** 由于变更较大，仅展示部分文件的审查结果。\n\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	return r.openai.ReviewStream(ctx, diff, onToken)
}

// buildDiff 将变更列表拼接成 diff 字符串，按文件粒度截断，总大小不超过 maxBytes。
func buildDiff(changes []gitlab.Change, maxBytes int) (string, bool) {
	var sb strings.Builder
	truncated := false

	for _, c := range changes {
		if c.DeletedFile {
			continue
		}
		path := c.NewPath
		if path == "" {
			path = c.OldPath
		}
		block := fmt.Sprintf("--- File: %s ---\n%s\n\n", path, c.Diff)
		if sb.Len()+len(block) > maxBytes {
			truncated = true
			break
		}
		sb.WriteString(block)
	}

	return sb.String(), truncated
}
