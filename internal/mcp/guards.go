package mcp

import (
	"os"
	"strconv"
	"strings"
)

// guardConfig holds MCP safety/policy settings from the environment.
//
//	GONEXUS_MCP_READ_ONLY=1            reject mutating tools (reindex, rename apply)
//	GONEXUS_MCP_ALLOWED_REPOS=a,b      restrict queries to these repos
//	GONEXUS_MCP_DEFAULT_MAX_TOKENS=n   cap response size (approx; truncates lists)
type guardConfig struct {
	readOnly  bool
	allowed   map[string]bool // empty = all allowed
	maxTokens int
}

func loadGuardConfig() guardConfig {
	c := guardConfig{allowed: map[string]bool{}}
	c.readOnly = os.Getenv("GONEXUS_MCP_READ_ONLY") == "1"
	for _, r := range strings.Split(os.Getenv("GONEXUS_MCP_ALLOWED_REPOS"), ",") {
		if r = strings.TrimSpace(r); r != "" {
			c.allowed[r] = true
		}
	}
	if n, err := strconv.Atoi(os.Getenv("GONEXUS_MCP_DEFAULT_MAX_TOKENS")); err == nil && n > 0 {
		c.maxTokens = n
	}
	return c
}

func (c guardConfig) repoAllowed(name string) bool {
	return len(c.allowed) == 0 || c.allowed[name]
}

// listCap converts the token budget to an approximate max list length
// (~20 tokens per item). Zero budget => no cap (returns the fallback).
func (c guardConfig) listCap(fallback int) int {
	if c.maxTokens == 0 {
		return fallback
	}
	n := c.maxTokens / 20
	if n < 1 {
		n = 1
	}
	return n
}
