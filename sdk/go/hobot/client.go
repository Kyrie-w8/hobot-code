package hobot

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultConnectTimeout = 8 * time.Second
	maximumResponseBytes  = 8 * 1024 * 1024
	maximumEventBytes     = 4*1024*1024 + 64*1024
	maximumErrorBytes     = 32 * 1024
)

type Client struct {
	config Config
	callMu sync.Mutex
	proc   *bridgeProcess
}

type bridgeProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	lines   chan lineResult
	stderr  *boundedBuffer
}

type lineResult struct {
	line []byte
	err  error
}

func NewClient(config Config) (*Client, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	return &Client{config: normalized}, nil
}

func normalizeConfig(config Config) (Config, error) {
	config.Host = strings.TrimSpace(config.Host)
	config.User = strings.TrimSpace(config.User)
	if config.Host == "" || strings.HasPrefix(config.Host, "-") || strings.ContainsAny(config.Host, "@ \t\r\n") {
		return Config{}, fmt.Errorf("SSH host is invalid")
	}
	if config.User == "" {
		config.User = "root"
	}
	if strings.HasPrefix(config.User, "-") || strings.ContainsAny(config.User, "@:/\\ \t\r\n") {
		return Config{}, fmt.Errorf("SSH user is invalid")
	}
	if config.Port == 0 {
		config.Port = 22
	}
	if config.Port < 1 || config.Port > 65535 {
		return Config{}, fmt.Errorf("SSH port must be between 1 and 65535")
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = defaultConnectTimeout
	}
	if config.ConnectTimeout < time.Second || config.ConnectTimeout > time.Minute {
		return Config{}, fmt.Errorf("SSH connection timeout must be between 1 and 60 seconds")
	}
	if config.HostKeyPolicy == "" {
		config.HostKeyPolicy = "accept-new"
	}
	if config.HostKeyPolicy != "strict" && config.HostKeyPolicy != "accept-new" {
		return Config{}, fmt.Errorf("SSH host key policy must be strict or accept-new")
	}
	if config.IdentityFile != "" {
		absolute, err := filepath.Abs(config.IdentityFile)
		if err != nil {
			return Config{}, err
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() {
			return Config{}, fmt.Errorf("SSH identity file must be a readable regular file")
		}
		config.IdentityFile = absolute
	}
	if config.SSHBinary == "" {
		config.SSHBinary = "ssh"
	}
	binary, err := exec.LookPath(config.SSHBinary)
	if err != nil {
		return Config{}, fmt.Errorf("OpenSSH client is unavailable: %w", err)
	}
	config.SSHBinary = binary
	return config, nil
}

func (client *Client) Call(ctx context.Context, method string, params any, target any) error {
	if method == "" || len(method) > 128 {
		return fmt.Errorf("agentd method is invalid")
	}
	client.callMu.Lock()
	defer client.callMu.Unlock()
	process, err := client.ensureProcess()
	if err != nil {
		return err
	}
	id, err := requestID()
	if err != nil {
		return err
	}
	request := map[string]any{"protocol": ProtocolVersion, "id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if err := writeAll(process.stdin, append(encoded, '\n')); err != nil {
		client.stopProcess()
		return fmt.Errorf("write SSH bridge request: %w", err)
	}
	select {
	case <-ctx.Done():
		client.stopProcess()
		return ctx.Err()
	case result, ok := <-process.lines:
		if !ok {
			message := strings.TrimSpace(process.stderr.String())
			client.stopProcess()
			if message != "" {
				return fmt.Errorf("SSH bridge closed without a response: %s", message)
			}
			return fmt.Errorf("SSH bridge closed without a response")
		}
		if result.err != nil {
			message := strings.TrimSpace(process.stderr.String())
			client.stopProcess()
			if message != "" {
				return fmt.Errorf("SSH bridge closed: %s", message)
			}
			return fmt.Errorf("SSH bridge closed: %w", result.err)
		}
		return decodeResponse(result.line, id, target)
	}
}

func (client *Client) ensureProcess() (*bridgeProcess, error) {
	if client.proc != nil && client.proc.command.Process != nil {
		return client.proc, nil
	}
	command := exec.Command(client.config.SSHBinary, client.sshArgs()...)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &boundedBuffer{maximum: maximumErrorBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start SSH bridge: %w", err)
	}
	process := &bridgeProcess{command: command, stdin: stdin, lines: make(chan lineResult, 2), stderr: stderr}
	client.proc = process
	go scanLines(stdout, maximumResponseBytes, process.lines)
	go func() { _ = command.Wait() }()
	return process, nil
}

func (client *Client) sshArgs() []string {
	timeout := int(client.config.ConnectTimeout.Round(time.Second) / time.Second)
	args := []string{
		"-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=" + strconv.Itoa(timeout),
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=" + client.config.HostKeyPolicy,
		"-p", strconv.Itoa(client.config.Port),
	}
	if client.config.IdentityFile != "" {
		args = append(args, "-i", client.config.IdentityFile, "-o", "IdentitiesOnly=yes")
	}
	host := client.config.Host
	if net.ParseIP(host) != nil && strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return append(args, client.config.User+"@"+host, "hobot bridge --stdio")
}

func (client *Client) Close() error {
	client.callMu.Lock()
	defer client.callMu.Unlock()
	client.stopProcess()
	return nil
}

func (client *Client) stopProcess() {
	if client.proc == nil {
		return
	}
	_ = client.proc.stdin.Close()
	if client.proc.command.Process != nil {
		_ = client.proc.command.Process.Kill()
	}
	client.proc = nil
}

func decodeResponse(line []byte, id string, target any) error {
	var envelope responseEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		return fmt.Errorf("decode agentd response: %w", err)
	}
	if envelope.Protocol != ProtocolVersion || envelope.ID != id {
		return fmt.Errorf("agentd returned a mismatched response envelope")
	}
	if !envelope.OK {
		if envelope.Error == nil {
			return fmt.Errorf("agentd request failed")
		}
		return envelope.Error
	}
	if target == nil || len(envelope.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		return fmt.Errorf("decode agentd result: %w", err)
	}
	return nil
}

func requestID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "hobot-code-" + hex.EncodeToString(value), nil
}

func scanLines(reader io.Reader, maximum int, output chan<- lineResult) {
	defer close(output)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maximum)
	for scanner.Scan() {
		line := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		output <- lineResult{line: append([]byte(nil), line...)}
	}
	if err := scanner.Err(); err != nil {
		output <- lineResult{err: err}
	}
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		value = value[written:]
	}
	return nil
}

type boundedBuffer struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	maximum int
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	original := len(value)
	remaining := buffer.maximum - buffer.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = buffer.buffer.Write(value)
	}
	return original, nil
}

func (buffer *boundedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}
