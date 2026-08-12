package main

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	protocolVersion     = 1
	eventSchemaVersion  = 3
	maxRequestBytes     = 2 * 1024 * 1024
	maxEventRecordBytes = 4*1024*1024 + 64*1024
	maxResponseBytes    = 8 * 1024 * 1024
	maxPromptBytes      = 256 * 1024
)

var protocolCapabilities = []string{
	"events.normalized.v3",
	"system.snapshot",
	"support.bundle.v1",
	"deployments.v1",
	"approvals.list",
	"tasks.lifecycle",
	"tasks.page",
	"events.page",
	"tasks.resume",
	"tasks.restart",
	"tasks.fork",
	"tasks.models",
	"tasks.permissions",
	"tasks.images",
	"models.capabilities.v1",
	"workspaces.browse",
	"bridge.stdio",
}

type request struct {
	Protocol int             `json:"protocol"`
	ID       string          `json:"id"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
}

type response struct {
	Protocol int            `json:"protocol"`
	ID       string         `json:"id"`
	OK       bool           `json:"ok"`
	Result   any            `json:"result,omitempty"`
	Error    *protocolError `json:"error,omitempty"`
}

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type taskEvent struct {
	Protocol   int              `json:"protocol"`
	Kind       string           `json:"kind"`
	TaskID     string           `json:"taskId"`
	Sequence   uint64           `json:"sequence"`
	Time       time.Time        `json:"time"`
	Event      json.RawMessage  `json:"event"`
	Normalized *normalizedEvent `json:"normalized,omitempty"`
}

type normalizedEvent struct {
	Schema int            `json:"schema"`
	Type   string         `json:"type"`
	Data   map[string]any `json:"data,omitempty"`
}

type capabilityInfo struct {
	ProtocolMin     int      `json:"protocolMin"`
	ProtocolMax     int      `json:"protocolMax"`
	EventSchema     int      `json:"eventSchema"`
	Capabilities    []string `json:"capabilities"`
	MaximumRequest  int      `json:"maximumRequestBytes"`
	MaximumResponse int      `json:"maximumResponseBytes"`
	MaximumPrompt   int      `json:"maximumPromptBytes"`
	MaximumTasks    int      `json:"maximumActiveTasks"`
	MaximumRetained int      `json:"maximumRetainedTasks"`
}

func success(id string, result any) response {
	return response{Protocol: protocolVersion, ID: id, OK: true, Result: result}
}

func failure(id, code string, err error) response {
	return response{
		Protocol: protocolVersion,
		ID:       id,
		OK:       false,
		Error:    &protocolError{Code: code, Message: err.Error()},
	}
}

func validateRequest(req request) error {
	if req.Protocol != protocolVersion {
		return fmt.Errorf("unsupported protocol version %d; expected %d", req.Protocol, protocolVersion)
	}
	if req.ID == "" || len(req.ID) > 128 {
		return fmt.Errorf("request id must contain 1 to 128 characters")
	}
	if req.Method == "" || len(req.Method) > 128 {
		return fmt.Errorf("request method must contain 1 to 128 characters")
	}
	return nil
}
