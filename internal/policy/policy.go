package policy

import (
	"path/filepath"
	"strings"

	"github.com/Kyrie-w8/aster-edge/internal/config"
)

type Decision struct {
	Allowed          bool
	RequiresApproval bool
	Reason           string
}

type Engine struct {
	config config.SecurityConfig
}

func New(cfg config.SecurityConfig) *Engine { return &Engine{config: cfg} }

func (e *Engine) Decide(name, risk string) Decision {
	if matches(name, e.config.DeniedTools) {
		return Decision{Reason: "tool is explicitly denied"}
	}
	if !matches(name, e.config.AllowedTools) {
		return Decision{Reason: "tool is not allowed"}
	}
	approval := matches(name, e.config.ApprovalTools)
	if (risk == "write" || risk == "dangerous") && !e.config.ApproveWrites {
		approval = true
	}
	return Decision{Allowed: true, RequiresApproval: approval, Reason: "allowed by policy"}
}

func matches(name string, patterns []string) bool {
	for _, pattern := range patterns {
		ok, err := filepath.Match(pattern, name)
		if err == nil && ok {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(name, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}
