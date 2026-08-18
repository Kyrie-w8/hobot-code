package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	modelEgressPath             = "/v1/drobotics/messages"
	modelEgressProviderPrefix   = "/v1/providers/"
	modelEgressSocketEnv        = "HOBOT_CODE_MODEL_EGRESS_SOCKET"
	modelEgressProvidersEnv     = "HOBOT_CODE_MODEL_EGRESS_PROVIDERS"
	maximumModelEgressRequest   = 32 * 1024 * 1024
	maximumModelEgressResponse  = 64 * 1024 * 1024
	maximumModelEgressModelName = 256
)

type modelEgressRoute struct {
	ID         string
	API        string
	Endpoint   string
	Credential string
	AuthHeader bool
	Models     map[string]bool
}

type modelEgressServer struct {
	cfg      config
	client   *http.Client
	listener *net.UnixListener
	server   *http.Server
	slots    chan struct{}
	routes   map[string]modelEgressRoute
	close    sync.Once
}

func (service *modelEgressServer) completeJSON(ctx context.Context, provider string, body []byte, maximumResponse int64) ([]byte, error) {
	route, ok := service.routes[provider]
	if !ok {
		return nil, fmt.Errorf("model provider is unavailable: %s", provider)
	}
	if len(body) == 0 || len(body) > maximumModelEgressRequest || !json.Valid(body) {
		return nil, fmt.Errorf("invalid model request")
	}
	if maximumResponse <= 0 || maximumResponse > maximumModelEgressResponse {
		return nil, fmt.Errorf("invalid model response limit")
	}
	select {
	case service.slots <- struct{}{}:
		defer func() { <-service.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, route.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	switch route.API {
	case "drobotics-anthropic":
		request.Header.Set("Authorization", "Bearer "+route.Credential)
		request.Header.Set("Anthropic-Version", "2023-06-01")
	case "anthropic-messages":
		if route.AuthHeader {
			request.Header.Set("Authorization", "Bearer "+route.Credential)
		} else {
			request.Header.Set("X-Api-Key", route.Credential)
		}
		request.Header.Set("Anthropic-Version", "2023-06-01")
	case "openai-completions", "openai-responses":
		request.Header.Set("Authorization", "Bearer "+route.Credential)
	default:
		return nil, fmt.Errorf("model provider API is unsupported")
	}
	request.Header.Set("User-Agent", "hobot-code-agentd/"+version)
	response, err := service.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, maximumResponse+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximumResponse {
		return nil, fmt.Errorf("model response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("model gateway returned HTTP %d", response.StatusCode)
	}
	if !json.Valid(content) {
		return nil, fmt.Errorf("model gateway returned invalid JSON")
	}
	return content, nil
}

func normalizeModelEgressBaseURL(value string) (string, error) {
	return normalizeModelEgressURL(value, defaultDroboticsBaseURL, "ANTHROPIC_BASE_URL")
}

func normalizeModelEgressURL(value, fallback, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s is invalid", label)
	}
	host := strings.ToLower(parsed.Hostname())
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && local) {
		return "", fmt.Errorf("%s must use HTTPS, or HTTP on localhost", label)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func managedModelEgressAPI(api string) bool {
	switch api {
	case "anthropic-messages", "openai-completions", "openai-responses":
		return true
	default:
		return false
	}
}

func modelEgressEndpoint(baseURL, api string) (string, error) {
	baseURL, err := normalizeModelEgressURL(baseURL, "", "model provider base URL")
	if err != nil {
		return "", err
	}
	suffix := ""
	switch api {
	case "drobotics-anthropic", "anthropic-messages":
		suffix = "/v1/messages"
	case "openai-completions":
		suffix = "/chat/completions"
	case "openai-responses":
		suffix = "/responses"
	default:
		return "", fmt.Errorf("model egress API is unsupported: %s", api)
	}
	return strings.TrimRight(baseURL, "/") + suffix, nil
}

func loadModelEgressRoutes(cfg config) (map[string]modelEgressRoute, error) {
	if cfg.modelEgressRoutes != nil {
		return cfg.modelEgressRoutes, nil
	}
	return buildModelEgressRoutes(cfg)
}

func buildModelEgressRoutes(cfg config) (map[string]modelEgressRoute, error) {
	routes := map[string]modelEgressRoute{}
	bundle, err := decodeGatewayCredentialBundle([]byte(gatewayCredentialPayload(cfg)))
	if err != nil {
		return nil, err
	}
	droboticsCredential := strings.TrimSpace(cfg.gatewayToken)
	if droboticsCredential == "" {
		droboticsCredential = bundle.DRobotics
	}
	if droboticsCredential != "" && cfg.DRoboticsBaseURL != "" {
		endpoint, err := modelEgressEndpoint(cfg.DRoboticsBaseURL, "drobotics-anthropic")
		if err != nil {
			return nil, err
		}
		routes["drobotics"] = modelEgressRoute{
			ID: "drobotics", API: "drobotics-anthropic", Endpoint: endpoint,
			Credential: droboticsCredential, AuthHeader: true,
		}
	}
	providers, err := loadManagedProviderDefinitions(managedProviderConfigPath(cfg))
	if err != nil {
		return nil, err
	}
	for _, provider := range providers {
		id := provider["id"].(string)
		api := provider["api"].(string)
		if !managedModelEgressAPI(api) {
			continue
		}
		credential := bundle.ProviderKeys[provider["credentialEnv"].(string)]
		if credential == "" {
			continue
		}
		endpoint, err := modelEgressEndpoint(provider["baseUrl"].(string), api)
		if err != nil {
			return nil, err
		}
		models := map[string]bool{}
		for _, raw := range provider["models"].([]any) {
			model := raw.(map[string]any)
			models[model["id"].(string)] = true
		}
		authHeader, _ := provider["authHeader"].(bool)
		routes[id] = modelEgressRoute{
			ID: id, API: api, Endpoint: endpoint, Credential: credential,
			AuthHeader: authHeader, Models: models,
		}
	}
	return routes, nil
}

func modelEgressAvailable(cfg config) bool {
	if !filepath.IsAbs(cfg.ModelEgressSocket) {
		return false
	}
	routes, err := loadModelEgressRoutes(cfg)
	return err == nil && len(routes) > 0
}

func modelEgressProviderAvailable(cfg config, provider, model string) bool {
	routes, err := loadModelEgressRoutes(cfg)
	if err != nil {
		return false
	}
	route, ok := routes[provider]
	if !ok {
		return false
	}
	return len(route.Models) == 0 || route.Models[model]
}

func modelEgressProviderList(cfg config) string {
	routes, err := loadModelEgressRoutes(cfg)
	if err != nil {
		return ""
	}
	providers := make([]string, 0, len(routes))
	for provider := range routes {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	return strings.Join(providers, ",")
}

func modelEgressSocketReady(cfg config) bool {
	info, err := os.Lstat(cfg.ModelEgressSocket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	owner, known := fileOwner(info)
	return !known || owner == os.Getuid()
}

func newModelEgressServer(cfg config) (*modelEgressServer, error) {
	routes, err := loadModelEgressRoutes(cfg)
	if err != nil {
		return nil, fmt.Errorf("load model egress routes: %w", err)
	}
	timeout := cfg.ModelEgressTimeout
	if timeout <= 0 {
		timeout = 50 * time.Minute
	}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	service := &modelEgressServer{
		cfg: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("model egress redirects are disabled")
			},
		},
		slots:  make(chan struct{}, max(2, min(cfg.MaxTasks*2, 16))),
		routes: routes,
	}
	service.server = &http.Server{
		Handler:           service,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	return service, nil
}

func (service *modelEgressServer) listen() error {
	if len(service.routes) == 0 {
		return nil
	}
	if err := ensurePrivateDir(service.cfg.ModelEgressRoot); err != nil {
		return err
	}
	if err := removeStaleSocket(service.cfg.ModelEgressSocket); err != nil {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: service.cfg.ModelEgressSocket, Net: "unix"})
	if err != nil {
		return err
	}
	if err := os.Chmod(service.cfg.ModelEgressSocket, 0o600); err != nil {
		_ = listener.Close()
		return err
	}
	service.listener = listener
	return nil
}

func (service *modelEgressServer) serve() {
	if service.listener == nil {
		return
	}
	err := service.server.Serve(peerVerifiedUnixListener{UnixListener: service.listener})
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		log.Printf("model egress server stopped unexpectedly: %v", err)
	}
}

func (service *modelEgressServer) shutdown() {
	service.close.Do(func() {
		if service.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = service.server.Shutdown(ctx)
		}
		if service.listener != nil {
			_ = service.listener.Close()
		}
		if service.cfg.ModelEgressSocket != "" {
			_ = os.Remove(service.cfg.ModelEgressSocket)
		}
	})
}

type peerVerifiedUnixListener struct{ *net.UnixListener }

func (listener peerVerifiedUnixListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			return nil, err
		}
		if err := verifyPeer(connection); err != nil {
			_ = connection.Close()
			continue
		}
		return connection, nil
	}
}

func (service *modelEgressServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	provider := ""
	if request.URL.Path == modelEgressPath {
		provider = "drobotics"
	} else if strings.HasPrefix(request.URL.Path, modelEgressProviderPrefix) {
		provider = strings.TrimPrefix(request.URL.Path, modelEgressProviderPrefix)
	}
	route, routeOK := service.routes[provider]
	if request.Method != http.MethodPost || !routeOK || !managedProviderIDPattern.MatchString(provider) || request.URL.RawQuery != "" {
		http.Error(writer, "model egress route not found", http.StatusNotFound)
		return
	}
	select {
	case service.slots <- struct{}{}:
		defer func() { <-service.slots }()
	default:
		http.Error(writer, "model egress concurrency limit reached", http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maximumModelEgressRequest+1))
	if err != nil || len(body) == 0 || len(body) > maximumModelEgressRequest || !json.Valid(body) {
		http.Error(writer, "invalid or oversized model request", http.StatusBadRequest)
		return
	}
	var envelope struct {
		Model  string `json:"model"`
		Stream any    `json:"stream"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Model == "" || len(envelope.Model) > maximumModelEgressModelName || strings.ContainsAny(envelope.Model, "\x00\r\n") || (len(route.Models) > 0 && !route.Models[envelope.Model]) {
		http.Error(writer, "model request is missing a valid model", http.StatusBadRequest)
		return
	}
	if _, ok := envelope.Stream.(bool); !ok {
		http.Error(writer, "model request stream flag must be boolean", http.StatusBadRequest)
		return
	}
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, route.Endpoint, bytes.NewReader(body))
	if err != nil {
		http.Error(writer, "model egress request failed", http.StatusBadGateway)
		return
	}
	accept := request.Header.Get("Accept")
	if accept != "application/json" && accept != "text/event-stream, application/json" {
		accept = "text/event-stream, application/json"
	}
	upstream.Header.Set("Accept", accept)
	upstream.Header.Set("Content-Type", "application/json")
	switch route.API {
	case "drobotics-anthropic":
		upstream.Header.Set("Authorization", "Bearer "+route.Credential)
		upstream.Header.Set("Anthropic-Version", "2023-06-01")
	case "anthropic-messages":
		if route.AuthHeader {
			upstream.Header.Set("Authorization", "Bearer "+route.Credential)
		} else {
			upstream.Header.Set("X-Api-Key", route.Credential)
		}
		upstream.Header.Set("Anthropic-Version", "2023-06-01")
		if beta := request.Header.Get("Anthropic-Beta"); beta != "" && len(beta) <= 4096 && !strings.ContainsAny(beta, "\r\n") {
			upstream.Header.Set("Anthropic-Beta", beta)
		}
	case "openai-completions", "openai-responses":
		upstream.Header.Set("Authorization", "Bearer "+route.Credential)
	}
	upstream.Header.Set("User-Agent", "hobot-code-agentd/"+version)
	response, err := service.client.Do(upstream)
	if err != nil {
		http.Error(writer, "model gateway unavailable", http.StatusBadGateway)
		log.Printf("model egress provider=%s status=transport-error duration_ms=%d", route.ID, time.Since(started).Milliseconds())
		return
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if contentType != "" {
		writer.Header().Set("Content-Type", contentType)
	}
	if requestID := response.Header.Get("Request-Id"); requestID != "" && len(requestID) <= 256 {
		writer.Header().Set("Request-Id", requestID)
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(response.StatusCode)
	written, copyErr := copyBoundedModelResponse(writer, response.Body, maximumModelEgressResponse)
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
	status := fmt.Sprintf("http-%d", response.StatusCode)
	if copyErr != nil {
		status = "response-error"
	}
	log.Printf("model egress provider=%s status=%s response_bytes=%d duration_ms=%d", route.ID, status, written, time.Since(started).Milliseconds())
}

func copyBoundedModelResponse(destination io.Writer, source io.Reader, maximum int64) (int64, error) {
	buffer := make([]byte, 32*1024)
	written := int64(0)
	for written < maximum {
		limit := len(buffer)
		if remaining := maximum - written; int64(limit) > remaining {
			limit = int(remaining)
		}
		read, readErr := source.Read(buffer[:limit])
		if read > 0 {
			count, writeErr := destination.Write(buffer[:read])
			written += int64(count)
			if writeErr != nil {
				return written, writeErr
			}
			if count != read {
				return written, io.ErrShortWrite
			}
			if flusher, ok := destination.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
	one := []byte{0}
	if read, err := source.Read(one); read > 0 {
		return written, fmt.Errorf("model response exceeds %d bytes", maximum)
	} else if err != nil && !errors.Is(err, io.EOF) {
		return written, err
	}
	return written, nil
}
