// Package llm is an optional OpenAI-compatible chat client for narrative wiki
// prose. Unset GONEXUS_LLM_URL => FromEnv returns nil and the wiki stays
// structural (deterministic) only.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type Client interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// FromEnv builds a client from environment config, or nil if not configured.
//
//	GONEXUS_LLM_URL    chat endpoint, e.g. http://localhost:11434/v1/chat/completions
//	GONEXUS_LLM_MODEL  model name
//	GONEXUS_LLM_KEY    optional Bearer token
func FromEnv() Client {
	url := os.Getenv("GONEXUS_LLM_URL")
	if url == "" {
		return nil
	}
	return &httpClient{
		url:    url,
		model:  os.Getenv("GONEXUS_LLM_MODEL"),
		key:    os.Getenv("GONEXUS_LLM_KEY"),
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

type httpClient struct {
	url, model, key string
	client          *http.Client
}

type chatReq struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatResp struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}

func (c *httpClient) Complete(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(chatReq{
		Model:    c.model,
		Messages: []message{{Role: "system", Content: system}, {Role: "user", Content: user}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm: %s", resp.Status)
	}
	var out chatResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: no choices")
	}
	return out.Choices[0].Message.Content, nil
}
