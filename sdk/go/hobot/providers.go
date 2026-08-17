package hobot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
)

const maximumProviderCommandOutput = 512 * 1024

var managedProviderIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var managedModelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

var managedProviderAPIs = map[string]bool{
	"anthropic-messages":   true,
	"openai-completions":   true,
	"openai-responses":     true,
	"google-generative-ai": true,
}

func (client *Client) ManagedProviders(ctx context.Context) ([]ManagedProvider, error) {
	output, err := client.runBoardCommand(ctx, "hobot provider list --json", nil)
	if err != nil {
		return nil, err
	}
	var providers []ManagedProvider
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&providers); err != nil {
		return nil, fmt.Errorf("decode managed provider list: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode managed provider list: %w", err)
	}
	if err := validateManagedProviders(providers); err != nil {
		return nil, fmt.Errorf("board returned an invalid managed provider list: %w", err)
	}
	return providers, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func validateManagedProviders(providers []ManagedProvider) error {
	if len(providers) > 64 {
		return fmt.Errorf("too many providers")
	}
	seenProviders := make(map[string]bool, len(providers))
	for _, provider := range providers {
		if !managedProviderIDPattern.MatchString(provider.ID) || provider.ID == "drobotics" || seenProviders[provider.ID] {
			return fmt.Errorf("invalid or duplicate provider ID")
		}
		seenProviders[provider.ID] = true
		if !safeProviderLabel(provider.Name, 120, true) || !managedProviderAPIs[provider.API] {
			return fmt.Errorf("invalid provider metadata")
		}
		if (provider.Credential != "ready" && provider.Credential != "missing") || provider.CredentialUsers < 1 || provider.CredentialUsers > 64 {
			return fmt.Errorf("invalid provider credential state")
		}
		if len(provider.Models) < 1 || len(provider.Models) > 128 {
			return fmt.Errorf("invalid provider model count")
		}
		seenModels := make(map[string]bool, len(provider.Models))
		for _, model := range provider.Models {
			if !managedModelIDPattern.MatchString(model.ID) || seenModels[model.ID] {
				return fmt.Errorf("invalid or duplicate model ID")
			}
			seenModels[model.ID] = true
			if !safeProviderLabel(model.Name, 120, true) || model.ContextWindow < 1024 || model.ContextWindow > 4_000_000 || model.MaxTokens < 128 || model.MaxTokens > 131_072 || model.MaxTokens > model.ContextWindow {
				return fmt.Errorf("invalid model metadata")
			}
		}
	}
	return nil
}

func safeProviderLabel(value string, maximum int, optional bool) bool {
	if value == "" {
		return optional
	}
	if strings.TrimSpace(value) != value || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func (client *Client) AddManagedProvider(ctx context.Context, request AddManagedProviderRequest, apiKey string) error {
	metadata, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if len(metadata) > 16*1024 || len(apiKey) == 0 || len(apiKey) > 8192 || bytes.ContainsAny([]byte(apiKey), "\r\n") {
		return fmt.Errorf("managed provider request is invalid")
	}
	payload := make([]byte, 0, len(metadata)+len(apiKey)+2)
	payload = append(payload, metadata...)
	payload = append(payload, '\n')
	payload = append(payload, apiKey...)
	payload = append(payload, '\n')
	defer clearSensitiveBytes(payload)
	_, err = client.runBoardCommand(ctx, "hobot provider add --request-stdin", payload)
	return err
}

func (client *Client) RemoveManagedProvider(ctx context.Context, id string, keepCredential bool) error {
	payload, err := json.Marshal(map[string]any{"id": id, "keepCredential": keepCredential})
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	_, err = client.runBoardCommand(ctx, "hobot provider remove --request-stdin", payload)
	return err
}

func (client *Client) RotateManagedProviderCredential(ctx context.Context, id, apiKey string, allowShared bool) error {
	if !managedProviderIDPattern.MatchString(id) || id == "drobotics" || len(apiKey) == 0 || len(apiKey) > 8192 || bytes.ContainsAny([]byte(apiKey), "\r\n") {
		return fmt.Errorf("managed provider credential request is invalid")
	}
	metadata, err := json.Marshal(map[string]any{"id": id, "allowShared": allowShared})
	if err != nil {
		return err
	}
	payload := make([]byte, 0, len(metadata)+len(apiKey)+2)
	payload = append(payload, metadata...)
	payload = append(payload, '\n')
	payload = append(payload, apiKey...)
	payload = append(payload, '\n')
	defer clearSensitiveBytes(payload)
	_, err = client.runBoardCommand(ctx, "hobot provider rotate --request-stdin", payload)
	return err
}

func (client *Client) RestartDaemon(ctx context.Context) error {
	_, err := client.runBoardCommand(ctx, "hobot daemon restart", nil)
	return err
}

func (client *Client) runBoardCommand(ctx context.Context, remoteCommand string, input []byte) ([]byte, error) {
	var reader io.Reader
	if input != nil {
		reader = bytes.NewReader(input)
	}
	return client.runBoardCommandWithReader(ctx, remoteCommand, reader)
}

func (client *Client) runBoardCommandWithReader(ctx context.Context, remoteCommand string, reader io.Reader) ([]byte, error) {
	command := exec.CommandContext(ctx, client.config.SSHBinary, append(client.sshTransportArgs(), client.sshTarget(), remoteCommand)...)
	if reader != nil {
		command.Stdin = reader
	}

	stdout := &boundedBuffer{maximum: maximumProviderCommandOutput}
	stderr := &boundedBuffer{maximum: maximumErrorBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		message := bytes.TrimSpace([]byte(stderr.String()))
		if len(message) > 0 {
			return nil, fmt.Errorf("board command failed: %s", safeProviderCommandError(string(message)))
		}
		return nil, fmt.Errorf("board command failed: %w", err)
	}
	if stdout.buffer.Len() >= maximumProviderCommandOutput {
		return nil, fmt.Errorf("board command exceeded the output limit")
	}
	return []byte(stdout.String()), nil
}

func safeProviderCommandError(value string) string {
	value = ansiEscapePattern.ReplaceAllString(value, "")
	var result strings.Builder
	result.Grow(min(len(value), 1024))
	space := false
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || unicode.IsSpace(character) {
			space = result.Len() > 0
			continue
		}
		if space {
			result.WriteByte(' ')
			space = false
		}
		if result.Len()+len(string(character)) > 1024 {
			break
		}
		result.WriteRune(character)
	}
	if result.Len() == 0 {
		return "remote command failed"
	}
	return result.String()
}

func clearSensitiveBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
