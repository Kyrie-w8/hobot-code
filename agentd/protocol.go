package main

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	protocolVersion     = 1
	eventSchemaVersion  = 4
	maxRequestBytes     = 2 * 1024 * 1024
	maxEventRecordBytes = 4*1024*1024 + 64*1024
	maxResponseBytes    = 8 * 1024 * 1024
	maxPromptBytes      = 256 * 1024
)

var protocolCapabilities = []string{
	"events.normalized.v3",
	"events.normalized.v4",
	"events.items.v1",
	"events.retention.v1",
	"extensions.catalog.v1",
	"system.snapshot",
	"diagnostics.inspect.v1",
	"diagnostics.repair.v1",
	"support.bundle.v1",
	"support.bundle.v2",
	"deployments.v1",
	"approvals.list",
	"tasks.lifecycle",
	"tasks.queue.v1",
	"tasks.failure.v1",
	"tasks.turn-evidence.v1",
	"tasks.page",
	"events.page",
	"tasks.resume",
	"tasks.restart",
	"tasks.fork",
	"tasks.fork.deferred-prompt.v1",
	"tasks.models",
	"tasks.permissions",
	"tasks.sandbox.v1",
	"tasks.network.v1",
	"tasks.images",
	"models.capabilities.v1",
	"models.health.v1",
	"models.conformance.v1",
	"models.runtime-probe.v1",
	"models.rdk-probe.v1",
	"models.rdk-matrix.v1",
	"models.qualification.v1",
	"providers.manage.v1",
	"configuration.fingerprint.v1",
	"build.identity.v1",
	"pi.compatibility.v1",
	"workspaces.browse",
	"workspaces.changes.v1",
	"workspaces.isolation.v1",
	"workspaces.write-leases.v1",
	"workspaces.delivery.v1",
	"bridge.stdio",
}

type request struct {
	Protocol          int             `json:"protocol"`
	ID                string          `json:"id"`
	Method            string          `json:"method"`
	Params            json.RawMessage `json:"params,omitempty"`
	ConfigFingerprint string          `json:"configFingerprint,omitempty"`
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
	Schema int             `json:"schema"`
	Type   string          `json:"type"`
	Data   map[string]any  `json:"data,omitempty"`
	Item   *normalizedItem `json:"item,omitempty"`
}

// normalizedItem gives rich clients a stable, product-level description of an
// event without requiring them to understand the upstream Pi event payload.
// Data remains on normalizedEvent so schema 2/3 clients can keep their existing
// rendering path while schema 4 clients gain explicit item lifecycle semantics.
type normalizedItem struct {
	ID     string `json:"id,omitempty"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type capabilityInfo struct {
	ProtocolMin     int               `json:"protocolMin"`
	ProtocolMax     int               `json:"protocolMax"`
	EventSchema     int               `json:"eventSchema"`
	Capabilities    []string          `json:"capabilities"`
	MaximumRequest  int               `json:"maximumRequestBytes"`
	MaximumResponse int               `json:"maximumResponseBytes"`
	MaximumPrompt   int               `json:"maximumPromptBytes"`
	MaximumTasks    int               `json:"maximumActiveTasks"`
	MaximumRetained int               `json:"maximumRetainedTasks"`
	Sandbox         sandboxCapability `json:"sandbox"`
}

type sandboxCapability struct {
	Available        bool     `json:"available"`
	Backend          string   `json:"backend,omitempty"`
	Profiles         []string `json:"profiles,omitempty"`
	NetworkModes     []string `json:"networkModes,omitempty"`
	FilesystemWrites bool     `json:"filesystemWritesRestricted"`
	Devices          bool     `json:"devicesRestricted"`
	Capabilities     bool     `json:"capabilitiesDropped"`
	Network          bool     `json:"networkRestricted"`
	Reason           string   `json:"reason,omitempty"`
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
