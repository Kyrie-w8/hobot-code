package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const providerCredentialPrompt = "API key: "

type providerListModel struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow"`
	MaxTokens     int    `json:"maxTokens"`
	Reasoning     bool   `json:"reasoning"`
	Image         bool   `json:"image"`
}

type providerListItem struct {
	ID              string              `json:"id"`
	Name            string              `json:"name,omitempty"`
	API             string              `json:"api"`
	Models          []providerListModel `json:"models"`
	Credential      string              `json:"credential"`
	CredentialUsers int                 `json:"credentialUsers"`
}

func runProviderCLI(cfg config, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printProviderUsage(stdout)
		return nil
	}
	switch args[0] {
	case "list":
		return runProviderList(cfg, args[1:], stdout, stderr)
	case "add":
		return runProviderAdd(cfg, args[1:], stdin, stdout, stderr)
	case "rotate":
		return runProviderRotate(cfg, args[1:], stdin, stdout, stderr)
	case "remove":
		return runProviderRemove(cfg, args[1:], stdin, stdout, stderr)
	default:
		return fmt.Errorf("unknown provider command %q; use `hobot provider --help`", args[0])
	}
}

func printProviderUsage(output io.Writer) {
	fmt.Fprintln(output, `Manage API-key model providers without storing secrets in providers.json.

Usage:
  hobot provider list [--json]
  hobot provider add PROVIDER --base-url URL --model MODEL [options]
  hobot provider rotate PROVIDER [--token-stdin] [--yes-shared]
  hobot provider remove PROVIDER --yes [--keep-credential]

Add options:
  --api anthropic-messages|openai-completions|openai-responses|google-generative-ai
  --name NAME                 Provider display name
  --model-name NAME           Model display name
  --context-window TOKENS     1024..4000000
  --max-tokens TOKENS         128..131072
  --reasoning                 Advertise reasoning support
  --image                     Advertise image input support
  --auth-header               Send the API key in an Authorization header
  --token-stdin               Read one API key line from stdin for automation

Without --token-stdin, the API key is read privately from the controlling
terminal. Native Pi OAuth providers remain available through /login in the TUI.`)
}

func runProviderList(cfg config, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("provider list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: hobot provider list [--json]")
	}
	providers, err := loadManagedProviderDefinitions(managedProviderConfigPath(cfg))
	if err != nil {
		return err
	}
	bundle, err := decodeGatewayCredentialBundle([]byte(gatewayCredentialPayload(cfg)))
	if err != nil {
		return err
	}
	items := providerListItems(providers, bundle.ProviderKeys)
	if *jsonOutput {
		encoded, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(encoded))
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "No Hobot-managed providers are configured.")
		fmt.Fprintln(stdout, "Use `hobot provider add --help` or Pi `/login` in the TUI.")
		return nil
	}
	fmt.Fprintf(stdout, "%-20s %-22s %-8s %s\n", "PROVIDER", "API", "KEY", "MODELS")
	for _, item := range items {
		models := make([]string, 0, len(item.Models))
		for _, model := range item.Models {
			models = append(models, model.ID)
		}
		credential := item.Credential
		if item.CredentialUsers > 1 {
			credential = fmt.Sprintf("%s:%d", credential, item.CredentialUsers)
		}
		fmt.Fprintf(stdout, "%-20s %-22s %-8s %s\n", item.ID, item.API, credential, strings.Join(models, ", "))
	}
	return nil
}

func providerListItems(providers []map[string]any, credentials map[string]string) []providerListItem {
	credentialUsers := map[string]int{}
	for _, provider := range providers {
		credentialUsers[provider["credentialEnv"].(string)]++
	}
	items := make([]providerListItem, 0, len(providers))
	for _, provider := range providers {
		credential := "missing"
		if credentials[provider["credentialEnv"].(string)] != "" {
			credential = "ready"
		}
		credentialName := provider["credentialEnv"].(string)
		item := providerListItem{ID: provider["id"].(string), API: provider["api"].(string), Credential: credential, CredentialUsers: credentialUsers[credentialName]}
		if name, ok := provider["name"].(string); ok {
			item.Name = name
		}
		for _, raw := range provider["models"].([]any) {
			model := raw.(map[string]any)
			contextWindow, _ := optionalBoundedInteger(model["contextWindow"], 1024, 4_000_000, 128_000)
			maxTokens, _ := optionalBoundedInteger(model["maxTokens"], 128, 131_072, 16_384)
			listed := providerListModel{ID: model["id"].(string), ContextWindow: contextWindow, MaxTokens: maxTokens}
			listed.Name, _ = model["name"].(string)
			listed.Reasoning, _ = model["reasoning"].(bool)
			if input, ok := model["input"].([]any); ok {
				for _, kind := range input {
					listed.Image = listed.Image || kind == "image"
				}
			}
			item.Models = append(item.Models, listed)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].ID < items[right].ID })
	return items
}

type providerAddOptions struct {
	ID            string
	Name          string
	BaseURL       string
	API           string
	Model         string
	ModelName     string
	ContextWindow int
	MaxTokens     int
	Reasoning     bool
	Image         bool
	AuthHeader    bool
	TokenStdin    bool
}

type providerAddRequest struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	BaseURL       string `json:"baseUrl"`
	API           string `json:"api"`
	Model         string `json:"model"`
	ModelName     string `json:"modelName,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
	Reasoning     bool   `json:"reasoning,omitempty"`
	Image         bool   `json:"image,omitempty"`
	AuthHeader    bool   `json:"authHeader,omitempty"`
}

type providerRemoveRequest struct {
	ID             string `json:"id"`
	KeepCredential bool   `json:"keepCredential,omitempty"`
}

type providerRotateRequest struct {
	ID          string `json:"id"`
	AllowShared bool   `json:"allowShared,omitempty"`
}

func parseProviderAddArgs(args []string, output io.Writer) (providerAddOptions, error) {
	var options providerAddOptions
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		options.ID = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("provider add", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&options.Name, "name", "", "provider display name")
	flags.StringVar(&options.BaseURL, "base-url", "", "provider API base URL")
	flags.StringVar(&options.API, "api", "openai-completions", "Pi provider API")
	flags.StringVar(&options.Model, "model", "", "model ID")
	flags.StringVar(&options.ModelName, "model-name", "", "model display name")
	flags.IntVar(&options.ContextWindow, "context-window", 0, "model context window")
	flags.IntVar(&options.MaxTokens, "max-tokens", 0, "model output limit")
	flags.BoolVar(&options.Reasoning, "reasoning", false, "advertise reasoning support")
	flags.BoolVar(&options.Image, "image", false, "advertise image input support")
	flags.BoolVar(&options.AuthHeader, "auth-header", false, "use an Authorization header")
	flags.BoolVar(&options.TokenStdin, "token-stdin", false, "read API key from stdin")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if options.ID == "" && flags.NArg() == 1 {
		options.ID = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return options, fmt.Errorf("usage: hobot provider add PROVIDER --base-url URL --model MODEL [options]")
	}
	if err := validateProviderAddOptions(options); err != nil {
		return options, err
	}
	return options, nil
}

func validateProviderAddOptions(options providerAddOptions) error {
	if !managedProviderIDPattern.MatchString(options.ID) || options.ID == "drobotics" {
		return fmt.Errorf("provider ID must use 1 to 64 lowercase letters, digits, '.', '_' or '-', and cannot be drobotics")
	}
	if !managedProviderAPIs[options.API] {
		return fmt.Errorf("unsupported provider API: %s", options.API)
	}
	if !safeManagedProviderURL(options.BaseURL) {
		return fmt.Errorf("base URL must be HTTPS without credentials, query, or fragment; localhost may use HTTP")
	}
	if !managedModelIDPattern.MatchString(options.Model) {
		return fmt.Errorf("model ID is required and contains unsupported characters")
	}
	if !optionalSafeLabel(optionalString(options.Name), 120) || !optionalSafeLabel(optionalString(options.ModelName), 120) {
		return fmt.Errorf("provider and model names must be printable single-line text up to 120 characters")
	}
	if options.ContextWindow != 0 && (options.ContextWindow < 1024 || options.ContextWindow > 4_000_000) {
		return fmt.Errorf("context window must be between 1024 and 4000000 tokens")
	}
	if options.MaxTokens != 0 && (options.MaxTokens < 128 || options.MaxTokens > 131_072) {
		return fmt.Errorf("maximum output must be between 128 and 131072 tokens")
	}
	contextWindow := options.ContextWindow
	if contextWindow == 0 {
		contextWindow = 128_000
	}
	maxTokens := options.MaxTokens
	if maxTokens == 0 {
		maxTokens = 16_384
	}
	if maxTokens > contextWindow {
		return fmt.Errorf("maximum output cannot exceed the context window")
	}
	return nil
}

func optionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func runProviderAdd(cfg config, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 1 && args[0] == "--request-stdin" {
		options, token, err := readProviderAddRequest(stdin)
		if err != nil {
			return err
		}
		defer clearBytes(token)
		return addManagedProvider(cfg, options, token, stdout, stderr)
	}
	options, err := parseProviderAddArgs(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	_, providers, err := loadProviderConfigurationForEdit(managedProviderConfigPath(cfg))
	if err != nil {
		return err
	}
	if providerWithID(providers, options.ID) != nil {
		return fmt.Errorf("provider %s already exists; remove it before replacing its configuration", options.ID)
	}
	environmentPath := filepath.Join(cfg.ConfigRoot, "hobot.env")
	environmentBytes, err := loadEnvironmentForEdit(environmentPath)
	if err != nil {
		return err
	}
	defer clearBytes(environmentBytes)
	credentialName, err := providerCredentialName(options.ID, providers, environmentBytes)
	if err != nil {
		return err
	}
	entry := providerEntry(options, credentialName)
	candidate := append(append([]map[string]any(nil), providers...), entry)
	if err := validateProviderSet(candidate); err != nil {
		return err
	}
	token, err := readProviderCredential(options.TokenStdin, stdin, stderr)
	if err != nil {
		return err
	}
	defer clearBytes(token)
	return addManagedProvider(cfg, options, token, stdout, stderr)
}

func addManagedProvider(cfg config, options providerAddOptions, token []byte, stdout, stderr io.Writer) error {
	unlock, err := lockProviderConfiguration(cfg.ConfigRoot)
	if err != nil {
		return err
	}
	defer unlock()

	// Re-read after acquiring the cross-process lock so a concurrent edit cannot
	// be overwritten after the user entered a credential.
	providerPath := managedProviderConfigPath(cfg)
	_, providers, err := loadProviderConfigurationForEdit(providerPath)
	if err != nil {
		return err
	}
	if providerWithID(providers, options.ID) != nil {
		return fmt.Errorf("provider %s was added by another process", options.ID)
	}
	environmentPath := filepath.Join(cfg.ConfigRoot, "hobot.env")
	environmentBytes, err := loadEnvironmentForEdit(environmentPath)
	if err != nil {
		return err
	}
	defer clearBytes(environmentBytes)
	credentialName, err := providerCredentialName(options.ID, providers, environmentBytes)
	if err != nil {
		return err
	}
	entry := providerEntry(options, credentialName)
	candidate := append(append([]map[string]any(nil), providers...), entry)
	providerOutput, err := encodeProviderConfiguration(candidate)
	if err != nil {
		return err
	}
	environmentOutput, err := setEnvironmentCredential(environmentBytes, credentialName, token)
	if err != nil {
		return err
	}
	defer clearBytes(environmentOutput)

	// Publishing the credential first is crash-safe: an interrupted add can
	// leave only an unused key, never a provider that references a missing key.
	if err := writePrivateFileDurable(environmentPath, environmentOutput); err != nil {
		return fmt.Errorf("save provider credential: %w; no provider was published, and an unused credential may remain", err)
	}
	if err := writePrivateFileDurable(providerPath, providerOutput); err != nil {
		return fmt.Errorf("save provider configuration: %w; inspect `hobot provider list` before retrying, and an unused credential may remain", err)
	}
	fmt.Fprintf(stdout, "Added managed provider %s with model %s.\n", options.ID, options.Model)
	fmt.Fprintln(stdout, "The API key was saved to the private Hobot Code credential file and was not written to providers.json.")
	printProviderReloadAdvice(cfg, stdout, stderr, options.ID+"/"+options.Model)
	return nil
}

func providerEntry(options providerAddOptions, credential string) map[string]any {
	model := map[string]any{"id": options.Model}
	if options.ModelName != "" {
		model["name"] = options.ModelName
	}
	if options.ContextWindow != 0 {
		model["contextWindow"] = options.ContextWindow
	}
	if options.MaxTokens != 0 {
		model["maxTokens"] = options.MaxTokens
	}
	if options.Reasoning {
		model["reasoning"] = true
	}
	if options.Image {
		model["input"] = []any{"text", "image"}
	}
	provider := map[string]any{
		"id": options.ID, "baseUrl": options.BaseURL, "api": options.API,
		"credentialEnv": credential, "models": []any{model},
	}
	if options.Name != "" {
		provider["name"] = options.Name
	}
	if options.AuthHeader {
		provider["authHeader"] = true
	}
	return provider
}

func runProviderRotate(cfg config, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 1 && args[0] == "--request-stdin" {
		request, token, err := readProviderRotateRequest(stdin)
		if err != nil {
			return err
		}
		defer clearBytes(token)
		return rotateManagedProvider(cfg, request.ID, token, request.AllowShared, stdout, stderr)
	}
	providerID := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		providerID = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("provider rotate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tokenStdin := flags.Bool("token-stdin", false, "read API key from stdin")
	allowShared := flags.Bool("yes-shared", false, "confirm rotating a credential shared by multiple providers")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if providerID == "" && flags.NArg() == 1 {
		providerID = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return fmt.Errorf("usage: hobot provider rotate PROVIDER [--token-stdin] [--yes-shared]")
	}
	if !managedProviderIDPattern.MatchString(providerID) || providerID == "drobotics" {
		return fmt.Errorf("usage: hobot provider rotate PROVIDER [--token-stdin] [--yes-shared]")
	}
	providers, err := loadManagedProviderDefinitions(managedProviderConfigPath(cfg))
	if err != nil {
		return err
	}
	target := providerWithID(providers, providerID)
	if target == nil {
		return fmt.Errorf("provider %s is not configured", providerID)
	}
	if providerCredentialUsers(providers, target["credentialEnv"].(string)) > 1 && !*allowShared {
		return fmt.Errorf("provider %s shares its credential; repeat with --yes-shared to rotate every provider using that key", providerID)
	}
	token, err := readProviderCredential(*tokenStdin, stdin, stderr)
	if err != nil {
		return err
	}
	defer clearBytes(token)
	return rotateManagedProvider(cfg, providerID, token, *allowShared, stdout, stderr)
}

func rotateManagedProvider(cfg config, providerID string, token []byte, allowShared bool, stdout, stderr io.Writer) error {
	if err := validateProviderCredential(token); err != nil {
		return err
	}
	unlock, err := lockProviderConfiguration(cfg.ConfigRoot)
	if err != nil {
		return err
	}
	defer unlock()
	_, providers, err := loadProviderConfigurationForEdit(managedProviderConfigPath(cfg))
	if err != nil {
		return err
	}
	target := providerWithID(providers, providerID)
	if target == nil {
		return fmt.Errorf("provider %s is not configured", providerID)
	}
	credential := target["credentialEnv"].(string)
	users := providerCredentialUsers(providers, credential)
	if users > 1 && !allowShared {
		return fmt.Errorf("provider %s shares its credential; explicit shared rotation confirmation is required", providerID)
	}
	environmentPath := filepath.Join(cfg.ConfigRoot, "hobot.env")
	environmentBytes, err := loadEnvironmentForEdit(environmentPath)
	if err != nil {
		return err
	}
	defer clearBytes(environmentBytes)
	environmentOutput, err := replaceEnvironmentCredential(environmentBytes, credential, token)
	if err != nil {
		return err
	}
	defer clearBytes(environmentOutput)
	if err := writePrivateFileDurable(environmentPath, environmentOutput); err != nil {
		return fmt.Errorf("rotate provider credential: %w", err)
	}
	fmt.Fprintf(stdout, "Rotated the private credential used by provider %s.\n", providerID)
	if users > 1 {
		fmt.Fprintf(stdout, "The credential is shared by %d providers, so all of them now use the new key.\n", users)
	}
	printProviderReloadAdvice(cfg, stdout, stderr, providerID)
	return nil
}

func providerCredentialUsers(providers []map[string]any, credential string) int {
	users := 0
	for _, provider := range providers {
		if provider["credentialEnv"] == credential {
			users++
		}
	}
	return users
}

func runProviderRemove(cfg config, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 1 && args[0] == "--request-stdin" {
		request, err := readProviderRemoveRequest(stdin)
		if err != nil {
			return err
		}
		return removeManagedProvider(cfg, request.ID, request.KeepCredential, stdout, stderr)
	}
	providerID := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		providerID = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("provider remove", flag.ContinueOnError)
	flags.SetOutput(stderr)
	confirmed := flags.Bool("yes", false, "confirm removal")
	keepCredential := flags.Bool("keep-credential", false, "retain the API key")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if providerID == "" && flags.NArg() == 1 {
		providerID = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return fmt.Errorf("usage: hobot provider remove PROVIDER --yes [--keep-credential]")
	}
	if !managedProviderIDPattern.MatchString(providerID) || !*confirmed {
		return fmt.Errorf("usage: hobot provider remove PROVIDER --yes [--keep-credential]")
	}
	return removeManagedProvider(cfg, providerID, *keepCredential, stdout, stderr)
}

func removeManagedProvider(cfg config, providerID string, keepCredential bool, stdout, stderr io.Writer) error {
	unlock, err := lockProviderConfiguration(cfg.ConfigRoot)
	if err != nil {
		return err
	}
	defer unlock()
	providerPath := managedProviderConfigPath(cfg)
	_, providers, err := loadProviderConfigurationForEdit(providerPath)
	if err != nil {
		return err
	}
	target := providerWithID(providers, providerID)
	if target == nil {
		return fmt.Errorf("provider %s is not configured", providerID)
	}
	credential := target["credentialEnv"].(string)
	remaining := make([]map[string]any, 0, len(providers)-1)
	credentialStillUsed := false
	for _, provider := range providers {
		if provider["id"] == providerID {
			continue
		}
		remaining = append(remaining, provider)
		credentialStillUsed = credentialStillUsed || provider["credentialEnv"] == credential
	}
	providerOutput, err := encodeProviderConfiguration(remaining)
	if err != nil {
		return err
	}
	environmentPath := filepath.Join(cfg.ConfigRoot, "hobot.env")
	environmentBytes, err := loadEnvironmentForEdit(environmentPath)
	if err != nil {
		return err
	}
	defer clearBytes(environmentBytes)
	environmentOutput := environmentBytes
	removeCredential := !keepCredential && !credentialStillUsed
	if removeCredential {
		environmentOutput, err = removeEnvironmentCredential(environmentBytes, credential)
		if err != nil {
			return err
		}
	}
	defer clearBytes(environmentOutput)

	// Remove the reference first. A crash between the two writes can leave an
	// unused key but cannot publish a provider with no key.
	if err := writePrivateFileDurable(providerPath, providerOutput); err != nil {
		return fmt.Errorf("remove provider configuration: %w; inspect `hobot provider list` before retrying", err)
	}
	if removeCredential {
		if err := writePrivateFileDurable(environmentPath, environmentOutput); err != nil {
			return fmt.Errorf("provider was removed, but its now-unreferenced credential could not be removed: %w", err)
		}
	}
	fmt.Fprintf(stdout, "Removed managed provider %s.\n", providerID)
	if keepCredential || credentialStillUsed {
		fmt.Fprintln(stdout, "Its credential was retained.")
	} else {
		fmt.Fprintln(stdout, "Its unreferenced credential was removed.")
	}
	printProviderReloadAdvice(cfg, stdout, stderr, "")
	return nil
}

func readProviderAddRequest(input io.Reader) (providerAddOptions, []byte, error) {
	reader := bufio.NewReader(io.LimitReader(input, maximumGatewayTokenBytes+16*1024+3))
	metadata, err := readBoundedProviderLine(reader, 16*1024)
	if err != nil {
		return providerAddOptions{}, nil, fmt.Errorf("read provider request: %w", err)
	}
	if err := rejectDuplicateJSONKeys(string(metadata)); err != nil {
		return providerAddOptions{}, nil, fmt.Errorf("provider request is invalid")
	}
	var request providerAddRequest
	decoder := json.NewDecoder(bytes.NewReader(metadata))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return providerAddOptions{}, nil, fmt.Errorf("provider request is invalid")
	}
	options := providerAddOptions{
		ID: request.ID, Name: request.Name, BaseURL: request.BaseURL, API: request.API,
		Model: request.Model, ModelName: request.ModelName, ContextWindow: request.ContextWindow,
		MaxTokens: request.MaxTokens, Reasoning: request.Reasoning, Image: request.Image,
		AuthHeader: request.AuthHeader, TokenStdin: true,
	}
	if err := validateProviderAddOptions(options); err != nil {
		return providerAddOptions{}, nil, err
	}
	token, err := readBoundedProviderLine(reader, maximumGatewayTokenBytes)
	if err != nil {
		return providerAddOptions{}, nil, fmt.Errorf("read API key: %w", err)
	}
	if err := validateProviderCredential(token); err != nil {
		clearBytes(token)
		return providerAddOptions{}, nil, err
	}
	if extra, err := reader.ReadByte(); err == nil || !errors.Is(err, io.EOF) {
		clearBytes(token)
		_ = extra
		return providerAddOptions{}, nil, fmt.Errorf("provider request contains trailing data")
	}
	return options, token, nil
}

func readProviderRemoveRequest(input io.Reader) (providerRemoveRequest, error) {
	reader := bufio.NewReader(io.LimitReader(input, 4098))
	metadata, err := readBoundedProviderLine(reader, 4096)
	if err != nil {
		return providerRemoveRequest{}, fmt.Errorf("read provider removal request: %w", err)
	}
	if err := rejectDuplicateJSONKeys(string(metadata)); err != nil {
		return providerRemoveRequest{}, fmt.Errorf("provider removal request is invalid")
	}
	var request providerRemoveRequest
	decoder := json.NewDecoder(bytes.NewReader(metadata))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || !managedProviderIDPattern.MatchString(request.ID) || request.ID == "drobotics" {
		return providerRemoveRequest{}, fmt.Errorf("provider removal request is invalid")
	}
	if extra, err := reader.ReadByte(); err == nil || !errors.Is(err, io.EOF) {
		_ = extra
		return providerRemoveRequest{}, fmt.Errorf("provider removal request contains trailing data")
	}
	return request, nil
}

func readProviderRotateRequest(input io.Reader) (providerRotateRequest, []byte, error) {
	reader := bufio.NewReader(io.LimitReader(input, maximumGatewayTokenBytes+4099))
	metadata, err := readBoundedProviderLine(reader, 4096)
	if err != nil {
		return providerRotateRequest{}, nil, fmt.Errorf("read provider rotation request: %w", err)
	}
	if err := rejectDuplicateJSONKeys(string(metadata)); err != nil {
		return providerRotateRequest{}, nil, fmt.Errorf("provider rotation request is invalid")
	}
	var request providerRotateRequest
	decoder := json.NewDecoder(bytes.NewReader(metadata))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || !managedProviderIDPattern.MatchString(request.ID) || request.ID == "drobotics" {
		return providerRotateRequest{}, nil, fmt.Errorf("provider rotation request is invalid")
	}
	token, err := readBoundedProviderLine(reader, maximumGatewayTokenBytes)
	if err != nil {
		return providerRotateRequest{}, nil, fmt.Errorf("read API key: %w", err)
	}
	if err := validateProviderCredential(token); err != nil {
		clearBytes(token)
		return providerRotateRequest{}, nil, err
	}
	if extra, err := reader.ReadByte(); err == nil || !errors.Is(err, io.EOF) {
		clearBytes(token)
		_ = extra
		return providerRotateRequest{}, nil, fmt.Errorf("provider rotation request contains trailing data")
	}
	return request, token, nil
}

func readBoundedProviderLine(reader *bufio.Reader, maximum int) ([]byte, error) {
	value, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(value) == 0 || len(value) > maximum+1 || (len(value) == maximum+1 && value[len(value)-1] != '\n') {
		return nil, fmt.Errorf("input line must contain 1 to %d bytes", maximum)
	}
	value = bytes.TrimSuffix(value, []byte("\n"))
	value = bytes.TrimSuffix(value, []byte("\r"))
	if len(value) == 0 || len(value) > maximum {
		return nil, fmt.Errorf("input line must contain 1 to %d bytes", maximum)
	}
	return value, nil
}

func loadProviderConfigurationForEdit(path string) ([]byte, []map[string]any, error) {
	raw, missing, err := readPrivateConfigBytes(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read managed provider configuration: %w", err)
	}
	if missing {
		raw = []byte("{\n  \"schemaVersion\": 1,\n  \"providers\": []\n}\n")
	}
	var document map[string]any
	if rejectDuplicateJSONKeys(string(raw)) != nil || json.Unmarshal(raw, &document) != nil || document == nil {
		return nil, nil, fmt.Errorf("managed provider configuration is invalid")
	}
	providers, err := validateManagedProviderDocument(document)
	if err != nil {
		return nil, nil, err
	}
	return raw, providers, nil
}

func validateProviderSet(providers []map[string]any) error {
	document := map[string]any{"schemaVersion": 1, "providers": providerMapsToAny(providers)}
	_, err := validateManagedProviderDocument(document)
	if err != nil {
		return fmt.Errorf("managed provider values failed validation")
	}
	return nil
}

func encodeProviderConfiguration(providers []map[string]any) ([]byte, error) {
	if err := validateProviderSet(providers); err != nil {
		return nil, err
	}
	sort.Slice(providers, func(left, right int) bool { return providers[left]["id"].(string) < providers[right]["id"].(string) })
	document := map[string]any{"schemaVersion": 1, "providers": providerMapsToAny(providers)}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func providerMapsToAny(providers []map[string]any) []any {
	values := make([]any, len(providers))
	for index, provider := range providers {
		values[index] = provider
	}
	return values
}

func providerWithID(providers []map[string]any, id string) map[string]any {
	for _, provider := range providers {
		if provider["id"] == id {
			return provider
		}
	}
	return nil
}

func providerCredentialName(id string, providers []map[string]any, environment []byte) (string, error) {
	suffix := strings.ToUpper(id)
	suffix = strings.Map(func(value rune) rune {
		if (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') {
			return value
		}
		return '_'
	}, suffix)
	base := providerTokenPrefix + suffix
	used := map[string]bool{}
	for _, provider := range providers {
		used[provider["credentialEnv"].(string)] = true
	}
	keys, err := environmentKeys(environment)
	if err != nil {
		return "", err
	}
	for name := range keys {
		used[name] = true
	}
	if !used[base] && validProviderCredentialEnvironment(base) {
		return base, nil
	}
	digest := sha256.Sum256([]byte(id))
	candidate := base + "_" + hex.EncodeToString(digest[:4])
	if used[candidate] || !validProviderCredentialEnvironment(candidate) {
		return "", fmt.Errorf("could not allocate a unique credential slot for provider %s", id)
	}
	return candidate, nil
}

func loadEnvironmentForEdit(path string) ([]byte, error) {
	raw, missing, err := readPrivateConfigBytes(path)
	if err != nil {
		return nil, fmt.Errorf("read private credential file: %w", err)
	}
	if missing {
		return []byte("# Hobot Code model credentials.\n"), nil
	}
	if _, err := environmentKeys(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func environmentKeys(raw []byte) (map[string]bool, error) {
	if bytes.IndexByte(raw, 0) >= 0 || bytes.IndexByte(raw, '\r') >= 0 || len(raw) > maximumInventoryConfigBytes {
		return nil, fmt.Errorf("private credential file contains unsupported data")
	}
	keys := map[string]bool{}
	for index, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !validEnvironmentKey(key) || unmatchedEnvironmentQuote(value) {
			return nil, fmt.Errorf("private credential file has invalid syntax at line %d", index+1)
		}
		if keys[key] {
			return nil, fmt.Errorf("private credential file repeats %s", key)
		}
		keys[key] = true
	}
	return keys, nil
}

func validEnvironmentKey(value string) bool {
	if value == "" || (value[0] >= '0' && value[0] <= '9') {
		return false
	}
	for _, current := range value {
		if (current < 'A' || current > 'Z') && (current < 'a' || current > 'z') && (current < '0' || current > '9') && current != '_' {
			return false
		}
	}
	return true
}

func unmatchedEnvironmentQuote(value string) bool {
	return (strings.HasPrefix(value, "\"") != strings.HasSuffix(value, "\"")) || (strings.HasPrefix(value, "'") != strings.HasSuffix(value, "'"))
}

func setEnvironmentCredential(raw []byte, name string, token []byte) ([]byte, error) {
	keys, err := environmentKeys(raw)
	if err != nil {
		return nil, err
	}
	if keys[name] {
		return nil, fmt.Errorf("credential slot %s already exists", name)
	}
	result := append([]byte(nil), raw...)
	if len(result) > 0 && result[len(result)-1] != '\n' {
		result = append(result, '\n')
	}
	result = append(result, name...)
	result = append(result, '=')
	result = append(result, token...)
	result = append(result, '\n')
	if len(result) > maximumInventoryConfigBytes {
		return nil, fmt.Errorf("private credential file exceeds %d bytes", maximumInventoryConfigBytes)
	}
	return result, nil
}

func replaceEnvironmentCredential(raw []byte, name string, token []byte) ([]byte, error) {
	keys, err := environmentKeys(raw)
	if err != nil {
		return nil, err
	}
	if !validProviderCredentialEnvironment(name) {
		return nil, fmt.Errorf("provider credential slot is invalid")
	}
	if !keys[name] {
		return setEnvironmentCredential(raw, name, token)
	}
	lines := bytes.Split(raw, []byte("\n"))
	result := make([]byte, 0, len(raw)-64+len(token))
	for index, line := range lines {
		key, _, ok := bytes.Cut(line, []byte("="))
		if ok && string(key) == name {
			result = append(result, name...)
			result = append(result, '=')
			result = append(result, token...)
		} else {
			result = append(result, line...)
		}
		if index < len(lines)-1 {
			result = append(result, '\n')
		}
	}
	if len(result) > maximumInventoryConfigBytes {
		clearBytes(result)
		return nil, fmt.Errorf("private credential file exceeds %d bytes", maximumInventoryConfigBytes)
	}
	return result, nil
}

func removeEnvironmentCredential(raw []byte, name string) ([]byte, error) {
	keys, err := environmentKeys(raw)
	if err != nil {
		return nil, err
	}
	if !keys[name] {
		return raw, nil
	}
	lines := strings.Split(string(raw), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if ok && key == name {
			continue
		}
		result = append(result, line)
	}
	return []byte(strings.Join(result, "\n")), nil
}

func readProviderCredential(fromStdin bool, stdin io.Reader, stderr io.Writer) ([]byte, error) {
	if fromStdin {
		return readCredentialLine(stdin)
	}
	return readCredentialFromTerminal(stderr)
}

func readCredentialLine(input io.Reader) ([]byte, error) {
	reader := bufio.NewReader(io.LimitReader(input, maximumGatewayTokenBytes+2))
	value, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read API key: %w", err)
	}
	value = bytes.TrimSuffix(value, []byte("\n"))
	value = bytes.TrimSuffix(value, []byte("\r"))
	if err := validateProviderCredential(value); err != nil {
		return nil, err
	}
	return value, nil
}

func validateProviderCredential(value []byte) error {
	if len(value) == 0 || len(value) > maximumGatewayTokenBytes {
		return fmt.Errorf("API key must contain 1 to %d bytes", maximumGatewayTokenBytes)
	}
	for _, current := range value {
		if current < 0x21 || current > 0x7e || current == '\'' || current == '"' {
			return fmt.Errorf("API key must use visible ASCII characters without quotes or whitespace")
		}
	}
	return nil
}

func readCredentialFromTerminal(stderr io.Writer) ([]byte, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("interactive credential entry requires a controlling terminal; pipe the key with --token-stdin")
	}
	defer tty.Close()
	fmt.Fprint(stderr, providerCredentialPrompt)
	if err := setTerminalEcho(tty, false); err != nil {
		return nil, fmt.Errorf("disable terminal echo: %w", err)
	}
	echoDisabled := true
	defer func() {
		if echoDisabled {
			_ = setTerminalEcho(tty, true)
			fmt.Fprintln(stderr)
		}
	}()

	type readResult struct {
		value []byte
		err   error
	}
	result := make(chan readResult, 1)
	go func() {
		value, readErr := readCredentialLine(tty)
		result <- readResult{value: value, err: readErr}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	select {
	case current := <-result:
		_ = setTerminalEcho(tty, true)
		echoDisabled = false
		fmt.Fprintln(stderr)
		return current.value, current.err
	case caught := <-signals:
		_ = setTerminalEcho(tty, true)
		echoDisabled = false
		fmt.Fprintln(stderr)
		_ = tty.Close()
		return nil, fmt.Errorf("credential entry interrupted by %s", caught)
	}
}

func setTerminalEcho(tty *os.File, enabled bool) error {
	argument := "-echo"
	if enabled {
		argument = "echo"
	}
	command := exec.Command("stty", argument)
	command.Stdin = tty
	command.Stdout = tty
	command.Stderr = tty
	return command.Run()
}

func readPrivateConfigBytes(path string) ([]byte, bool, error) {
	if !filepath.IsAbs(path) {
		return nil, false, fmt.Errorf("path must be absolute")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maximumInventoryConfigBytes {
		return nil, false, fmt.Errorf("path must be a private regular file")
	}
	if owner, ok := fileOwner(info); ok && owner != os.Getuid() {
		return nil, false, fmt.Errorf("path is owned by uid %d, expected %d", owner, os.Getuid())
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, false, fmt.Errorf("private file changed while opening")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumInventoryConfigBytes+1))
	if err != nil || len(content) > maximumInventoryConfigBytes {
		if err != nil {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("private file exceeds %d bytes", maximumInventoryConfigBytes)
	}
	return content, false, nil
}

func writePrivateFileDurable(path string, content []byte) error {
	if len(content) == 0 || len(content) > maximumInventoryConfigBytes {
		return fmt.Errorf("private configuration must contain 1 to %d bytes", maximumInventoryConfigBytes)
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".hobot-provider.*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func printProviderReloadAdvice(cfg config, stdout, stderr io.Writer, model string) {
	client := daemonClient{cfg: cfg}
	if _, err := client.ping(); err == nil {
		fmt.Fprintln(stderr, "The background service is still using its previous configuration. Run: hobot daemon restart")
		return
	}
	if model != "" {
		fmt.Fprintf(stdout, "Verify it when ready: hobot model check %s\n", model)
	}
}
