package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
)

type MCPSet struct{ clients []*mcpClient }

func (s *MCPSet) Close() {
	for _, client := range s.clients {
		client.close()
	}
}

type mcpClient struct {
	name    string
	cmd     *exec.Cmd
	in      io.WriteCloser
	decoder *json.Decoder
	mu      sync.Mutex
	nextID  int64
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func RegisterMCP(ctx context.Context, registry *Registry, configs []config.MCPConfig, timeoutSec int) (*MCPSet, error) {
	set := &MCPSet{}
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		client, defs, err := startMCP(ctx, cfg)
		if err != nil {
			set.Close()
			return nil, err
		}
		set.clients = append(set.clients, client)
		for _, definition := range defs {
			definition := definition
			if definition.Risk == "" {
				definition.Risk = "dangerous"
			}
			name := definition.Name
			definition.Name = cfg.Name + "__" + name
			if err := registry.Add(core.Tool{Definition: definition, TimeoutSec: timeoutSec, Handler: func(callCtx context.Context, args map[string]any) (any, error) {
				return client.callTool(callCtx, name, args)
			}}); err != nil {
				set.Close()
				return nil, err
			}
		}
	}
	return set, nil
}

func startMCP(ctx context.Context, cfg config.MCPConfig) (*mcpClient, []core.ToolDefinition, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for key, value := range cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+os.ExpandEnv(value))
	}
	stderr, _ := cmd.StderrPipe()
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	go io.Copy(io.Discard, stderr)
	client := &mcpClient{name: cfg.Name, cmd: cmd, in: in, decoder: json.NewDecoder(bufio.NewReader(out)), nextID: 1}
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var initResult any
	protocolVersion := cfg.ProtocolVersion
	if protocolVersion == "" {
		protocolVersion = "2025-06-18"
	}
	if err := client.call(initCtx, "initialize", map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "aster", "version": "0.1.0"}}, &initResult); err != nil {
		client.close()
		return nil, nil, fmt.Errorf("MCP %s initialize: %w", cfg.Name, err)
	}
	if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
		client.close()
		return nil, nil, err
	}
	var list struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := client.call(initCtx, "tools/list", map[string]any{}, &list); err != nil {
		client.close()
		return nil, nil, fmt.Errorf("MCP %s list tools: %w", cfg.Name, err)
	}
	defs := make([]core.ToolDefinition, 0, len(list.Tools))
	for _, tool := range list.Tools {
		defs = append(defs, core.ToolDefinition{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema, Risk: "dangerous"})
	}
	return client, defs, nil
}

func (c *mcpClient) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	var result struct {
		Content    []map[string]any `json:"content"`
		IsError    bool             `json:"isError"`
		Structured any              `json:"structuredContent"`
	}
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &result); err != nil {
		return nil, err
	}
	if result.IsError {
		return nil, fmt.Errorf("MCP tool %s returned an error: %v", name, result.Content)
	}
	if result.Structured != nil {
		return result.Structured, nil
	}
	return result.Content, nil
}

func (c *mcpClient) call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	b, _ := json.Marshal(request)
	b = append(b, '\n')
	if _, err := c.in.Write(b); err != nil {
		return err
	}
	type answer struct {
		response rpcResponse
		err      error
	}
	ch := make(chan answer, 1)
	go func() {
		for {
			var response rpcResponse
			if err := c.decoder.Decode(&response); err != nil {
				ch <- answer{err: err}
				return
			}
			if len(response.ID) > 0 {
				ch <- answer{response: response}
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		_ = c.in.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		return ctx.Err()
	case answer := <-ch:
		if answer.err != nil {
			return answer.err
		}
		if answer.response.Error != nil {
			return fmt.Errorf("RPC %d: %s", answer.response.Error.Code, answer.response.Error.Message)
		}
		if out != nil {
			return json.Unmarshal(answer.response.Result, out)
		}
		return nil
	}
}

func (c *mcpClient) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	b = append(b, '\n')
	_, err := c.in.Write(b)
	return err
}
func (c *mcpClient) close() {
	_ = c.in.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}
