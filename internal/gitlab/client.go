package gitlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type MRChanges struct {
	Changes []Change `json:"changes"`
}

type Change struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	DeletedFile bool   `json:"deleted_file"`
	RenamedFile bool   `json:"renamed_file"`
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) GetMRChanges(projectID, mrIID int) (*MRChanges, error) {
	url := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%s/changes",
		c.baseURL, strconv.Itoa(projectID), strconv.Itoa(mrIID))

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get MR changes: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab API error %d: %.200s", resp.StatusCode, string(body))
	}

	var changes MRChanges
	if err := json.Unmarshal(body, &changes); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &changes, nil
}

func (c *Client) PostComment(projectID, mrIID int, body string) error {
	url := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%s/notes",
		c.baseURL, strconv.Itoa(projectID), strconv.Itoa(mrIID))

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("marshal comment: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitlab API error %d: %.200s", resp.StatusCode, string(b))
	}
	return nil
}
