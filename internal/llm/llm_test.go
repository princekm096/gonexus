package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFromEnvUnset(t *testing.T) {
	t.Setenv("GONEXUS_LLM_URL", "")
	if FromEnv() != nil {
		t.Fatal("FromEnv should be nil without GONEXUS_LLM_URL")
	}
}

func TestComplete(t *testing.T) {
	var gotReq chatReq
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "hello docs"}}},
		})
	}))
	defer srv.Close()

	t.Setenv("GONEXUS_LLM_URL", srv.URL)
	t.Setenv("GONEXUS_LLM_MODEL", "m")
	t.Setenv("GONEXUS_LLM_KEY", "k")

	c := FromEnv()
	if c == nil {
		t.Fatal("nil client despite URL set")
	}
	out, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello docs" {
		t.Fatalf("content = %q, want 'hello docs'", out)
	}
	if len(gotReq.Messages) != 2 || gotReq.Messages[0].Role != "system" {
		t.Fatalf("messages = %+v", gotReq.Messages)
	}
	if gotAuth != "Bearer k" {
		t.Fatalf("auth = %q", gotAuth)
	}
}
