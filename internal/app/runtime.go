package app

import (
	"context"
	"fmt"

	"github.com/Kyrie-w8/aster-edge/internal/agent"
	"github.com/Kyrie-w8/aster-edge/internal/board"
	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/policy"
	"github.com/Kyrie-w8/aster-edge/internal/provider"
	"github.com/Kyrie-w8/aster-edge/internal/session"
	"github.com/Kyrie-w8/aster-edge/internal/skills"
	"github.com/Kyrie-w8/aster-edge/internal/tools"
)

type Runtime struct {
	Config   config.Config
	Board    board.Snapshot
	Catalog  *skills.Catalog
	Registry *tools.Registry
	Store    *session.Store
	Agent    *agent.Engine
	MCP      *tools.MCPSet
}

func New(ctx context.Context, cfg config.Config, approval tools.ApprovalFunc) (*Runtime, error) {
	snapshot := board.Detect(cfg.Board.Profile)
	engine := policy.New(cfg.Security)
	registry := tools.New(engine, approval, cfg.Security.MaxToolOutput)
	if err := tools.RegisterBuiltins(registry, cfg, snapshot); err != nil {
		return nil, err
	}
	if err := tools.RegisterPlugins(registry, cfg.Plugins, cfg.Security.ToolTimeoutSec); err != nil {
		return nil, err
	}
	mcp, err := tools.RegisterMCP(ctx, registry, cfg.MCP, cfg.Security.ToolTimeoutSec)
	if err != nil {
		return nil, err
	}
	catalog, err := skills.Discover(cfg.Skills.Roots)
	if err != nil {
		mcp.Close()
		return nil, err
	}
	loaded, err := catalog.Load(cfg.Agent.EnabledSkills, snapshot.Profile, registry.Available())
	if err != nil {
		mcp.Close()
		return nil, err
	}
	prompt, err := agent.ComposePrompt(cfg, snapshot, loaded)
	if err != nil {
		mcp.Close()
		return nil, err
	}
	model, err := provider.New(cfg.Provider)
	if err != nil {
		mcp.Close()
		return nil, err
	}
	store, err := session.New(cfg.Session.Dir)
	if err != nil {
		mcp.Close()
		return nil, err
	}
	runtime := &Runtime{Config: cfg, Board: snapshot, Catalog: catalog, Registry: registry, Store: store, MCP: mcp}
	runtime.Agent = &agent.Engine{Config: cfg, Provider: model, Tools: registry, Store: store, SystemPrompt: prompt}
	return runtime, nil
}

func (r *Runtime) Close() {
	if r.MCP != nil {
		r.MCP.Close()
	}
}

func (r *Runtime) Doctor() map[string]any {
	return map[string]any{"ok": true, "agent": r.Config.Agent.Name, "provider": r.Config.Provider.Type, "model": r.Config.Provider.Model, "board": r.Board, "tools": r.Registry.Definitions(), "skills": r.Catalog.List(), "session_dir": r.Config.Session.Dir, "workspace_root": r.Config.Security.WorkspaceRoot}
}

func (r *Runtime) Summary() string {
	return fmt.Sprintf("%s | %s/%s | %s | %d tools", r.Config.Agent.Name, r.Config.Provider.Type, r.Config.Provider.Model, r.Board.Profile, len(r.Registry.Definitions()))
}
