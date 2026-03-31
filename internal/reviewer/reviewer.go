package reviewer

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"code-review/internal/gitlab"
	"code-review/internal/openai"
)

type Reviewer struct {
	gitlab       *gitlab.Client
	openai       *openai.Client
	maxDiffBytes int
}

func New(gl *gitlab.Client, ai *openai.Client, maxDiffBytes int) *Reviewer {
	return &Reviewer{
		gitlab:       gl,
		openai:       ai,
		maxDiffBytes: maxDiffBytes,
	}
}

// Process fetches the MR diff, calls OpenAI for review, and posts the result as a comment.
// Runs asynchronously; all errors are logged rather than returned.
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

// buildDiff formats changes into a single diff string, capping at maxBytes (per-file granularity).
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
