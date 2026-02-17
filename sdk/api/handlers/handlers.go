// Package handlers provides core API handler functionality for the CLI Proxy API server.
// It includes common types, client management, load balancing, and error handling
// shared across all API endpoint handlers (OpenAI, Claude, Gemini).
package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/net/context"
)

// ErrorResponse represents a standard error response format for the API.
// It contains a single ErrorDetail field.
type ErrorResponse struct {
	// Error contains detailed information about the error that occurred.
	Error ErrorDetail `json:"error"`
}

// ErrorDetail provides specific information about an error that occurred.
// It includes a human-readable message, an error type, and an optional error code.
type ErrorDetail struct {
	// Message is a human-readable message providing more details about the error.
	Message string `json:"message"`

	// Type is the category of error that occurred (e.g., "invalid_request_error").
	Type string `json:"type"`

	// Code is a short code identifying the error, if applicable.
	Code string `json:"code,omitempty"`
}

const idempotencyKeyMetadataKey = "idempotency_key"

const (
	defaultStreamingKeepAliveSeconds = 0
	defaultStreamingBootstrapRetries = 0
)

// BuildErrorResponseBody builds an OpenAI-compatible JSON error response body.
// If errText is already valid JSON, it is returned as-is to preserve upstream error payloads.
func BuildErrorResponseBody(status int, errText string) []byte {
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if strings.TrimSpace(errText) == "" {
		errText = http.StatusText(status)
	}

	trimmed := strings.TrimSpace(errText)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		return []byte(trimmed)
	}

	errType := "invalid_request_error"
	var code string
	switch status {
	case http.StatusUnauthorized:
		errType = "authentication_error"
		code = "invalid_api_key"
	case http.StatusForbidden:
		errType = "permission_error"
		code = "insufficient_quota"
	case http.StatusTooManyRequests:
		errType = "rate_limit_error"
		code = "rate_limit_exceeded"
	case http.StatusNotFound:
		errType = "invalid_request_error"
		code = "model_not_found"
	default:
		if status >= http.StatusInternalServerError {
			errType = "server_error"
			code = "internal_server_error"
		}
	}

	payload, err := json.Marshal(ErrorResponse{
		Error: ErrorDetail{
			Message: errText,
			Type:    errType,
			Code:    code,
		},
	})
	if err != nil {
		return []byte(fmt.Sprintf(`{"error":{"message":%q,"type":"server_error","code":"internal_server_error"}}`, errText))
	}
	return payload
}

// StreamingKeepAliveInterval returns the SSE keep-alive interval for this server.
// Returning 0 disables keep-alives (default when unset).
func StreamingKeepAliveInterval(cfg *config.SDKConfig) time.Duration {
	seconds := defaultStreamingKeepAliveSeconds
	if cfg != nil {
		seconds = cfg.Streaming.KeepAliveSeconds
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// NonStreamingKeepAliveInterval returns the keep-alive interval for non-streaming responses.
// Returning 0 disables keep-alives (default when unset).
func NonStreamingKeepAliveInterval(cfg *config.SDKConfig) time.Duration {
	seconds := 0
	if cfg != nil {
		seconds = cfg.NonStreamKeepAliveInterval
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// StreamingBootstrapRetries returns how many times a streaming request may be retried before any bytes are sent.
func StreamingBootstrapRetries(cfg *config.SDKConfig) int {
	retries := defaultStreamingBootstrapRetries
	if cfg != nil {
		retries = cfg.Streaming.BootstrapRetries
	}
	if retries < 0 {
		retries = 0
	}
	return retries
}

func requestExecutionMetadata(ctx context.Context) map[string]any {
	// Idempotency-Key is an optional client-supplied header used to correlate retries.
	// It is forwarded as execution metadata; when absent we generate a UUID.
	key := ""
	if ctx != nil {
		if ginCtx, ok := ctx.Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
			key = strings.TrimSpace(ginCtx.GetHeader("Idempotency-Key"))
		}
	}
	if key == "" {
		key = uuid.NewString()
	}
	return map[string]any{idempotencyKeyMetadataKey: key}
}

func apiKeyPolicyFromContext(ctx context.Context) *internalconfig.APIKeyPolicy {
	if ctx == nil {
		return nil
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return nil
	}
	value, exists := ginCtx.Get("apiKeyPolicy")
	if !exists || value == nil {
		return nil
	}
	policy, ok := value.(*internalconfig.APIKeyPolicy)
	if !ok || policy == nil {
		return nil
	}
	return policy
}

func clientAPIKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return ""
	}
	return strings.TrimSpace(ginCtx.GetString("apiKey"))
}

type errorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
	Message string `json:"message"`
}

type statusHeadersError struct {
	err   error
	code  int
	addon http.Header
}

func (e *statusHeadersError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *statusHeadersError) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.code
}

func (e *statusHeadersError) Headers() http.Header {
	if e == nil || e.addon == nil {
		return nil
	}
	return e.addon
}

func extractErrorMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !json.Valid([]byte(raw)) {
		return raw
	}
	var env errorEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return raw
	}
	if msg := strings.TrimSpace(env.Error.Message); msg != "" {
		return msg
	}
	if msg := strings.TrimSpace(env.Message); msg != "" {
		return msg
	}
	return raw
}

func isClaudeFailoverEligible(status int, err error) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden:
		return true
	case http.StatusInternalServerError:
		msg := strings.ToLower(extractErrorMessage(errString(err)))
		if msg == "" {
			return false
		}
		// When no Claude auth is currently selectable (all cooled down / unavailable),
		// the core auth manager can return an internal error like:
		//   "auth_unavailable: no auth available"
		// Treat this as failover eligible so clients can transparently route to Codex.
		if strings.Contains(msg, "auth_unavailable") || strings.Contains(msg, "auth_not_found") || strings.Contains(msg, "no auth available") {
			return true
		}
		return false
	case http.StatusBadGateway:
		msg := strings.ToLower(extractErrorMessage(errString(err)))
		if msg == "" {
			return false
		}
		// When a provider isn't configured/registered, requests may fail before any upstream call.
		// Only treat this as failover eligible when the error explicitly indicates an unknown provider.
		if strings.Contains(msg, "unknown provider") && strings.Contains(msg, "model") {
			return true
		}
		return false
	case http.StatusBadRequest:
		msg := strings.ToLower(extractErrorMessage(errString(err)))
		if msg == "" {
			return false
		}
		// Only treat 400 as failover eligible when it looks like an auth/account scoped issue.
		if strings.Contains(msg, "account") {
			if strings.Contains(msg, "disabled") || strings.Contains(msg, "suspended") || strings.Contains(msg, "banned") || strings.Contains(msg, "blocked") {
				return true
			}
			return true
		}
		if strings.Contains(msg, "token") ||
			strings.Contains(msg, "oauth") ||
			strings.Contains(msg, "credential") ||
			strings.Contains(msg, "session") ||
			strings.Contains(msg, "login") {
			return true
		}
		return false
	default:
		return false
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func seemsClaudeModel(modelName string) bool {
	resolved := util.ResolveAutoModel(modelName)
	parsed := thinking.ParseSuffix(resolved)
	base := strings.ToLower(strings.TrimSpace(parsed.ModelName))
	return strings.HasPrefix(base, "claude-")
}

func containsProvider(providers []string, provider string) bool {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" || len(providers) == 0 {
		return false
	}
	for _, p := range providers {
		if strings.EqualFold(strings.TrimSpace(p), provider) {
			return true
		}
	}
	return false
}

func rewriteModelField(body []byte, model string) []byte {
	model = strings.TrimSpace(model)
	if len(body) == 0 || model == "" {
		return body
	}
	if !gjson.GetBytes(body, "model").Exists() {
		return body
	}
	out, err := sjson.SetBytes(body, "model", model)
	if err != nil {
		return body
	}
	return out
}

// BaseAPIHandler contains the handlers for API endpoints.
// It holds a pool of clients to interact with the backend service and manages
// load balancing, client selection, and configuration.
type BaseAPIHandler struct {
	// AuthManager manages auth lifecycle and execution in the new architecture.
	AuthManager *coreauth.Manager

	// Cfg holds the current application configuration.
	Cfg *config.SDKConfig
}

// NewBaseAPIHandlers creates a new API handlers instance.
// It takes a slice of clients and configuration as input.
//
// Parameters:
//   - cliClients: A slice of AI service clients
//   - cfg: The application configuration
//
// Returns:
//   - *BaseAPIHandler: A new API handlers instance
func NewBaseAPIHandlers(cfg *config.SDKConfig, authManager *coreauth.Manager) *BaseAPIHandler {
	return &BaseAPIHandler{
		Cfg:         cfg,
		AuthManager: authManager,
	}
}

// UpdateClients updates the handlers' client list and configuration.
// This method is called when the configuration or authentication tokens change.
//
// Parameters:
//   - clients: The new slice of AI service clients
//   - cfg: The new application configuration
func (h *BaseAPIHandler) UpdateClients(cfg *config.SDKConfig) { h.Cfg = cfg }

// GetAlt extracts the 'alt' parameter from the request query string.
// It checks both 'alt' and '$alt' parameters and returns the appropriate value.
//
// Parameters:
//   - c: The Gin context containing the HTTP request
//
// Returns:
//   - string: The alt parameter value, or empty string if it's "sse"
func (h *BaseAPIHandler) GetAlt(c *gin.Context) string {
	var alt string
	var hasAlt bool
	alt, hasAlt = c.GetQuery("alt")
	if !hasAlt {
		alt, _ = c.GetQuery("$alt")
	}
	if alt == "sse" {
		return ""
	}
	return alt
}

// GetContextWithCancel creates a new context with cancellation capabilities.
// It embeds the Gin context and the API handler into the new context for later use.
// The returned cancel function also handles logging the API response if request logging is enabled.
//
// Parameters:
//   - handler: The API handler associated with the request.
//   - c: The Gin context of the current request.
//   - ctx: The parent context (caller values/deadlines are preserved; request context adds cancellation and request ID).
//
// Returns:
//   - context.Context: The new context with cancellation and embedded values.
//   - APIHandlerCancelFunc: A function to cancel the context and log the response.
func (h *BaseAPIHandler) GetContextWithCancel(handler interfaces.APIHandler, c *gin.Context, ctx context.Context) (context.Context, APIHandlerCancelFunc) {
	parentCtx := ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	var requestCtx context.Context
	if c != nil && c.Request != nil {
		requestCtx = c.Request.Context()
	}

	if requestCtx != nil && logging.GetRequestID(parentCtx) == "" {
		if requestID := logging.GetRequestID(requestCtx); requestID != "" {
			parentCtx = logging.WithRequestID(parentCtx, requestID)
		} else if requestID := logging.GetGinRequestID(c); requestID != "" {
			parentCtx = logging.WithRequestID(parentCtx, requestID)
		}
	}
	newCtx, cancel := context.WithCancel(parentCtx)
	if requestCtx != nil && requestCtx != parentCtx {
		go func() {
			select {
			case <-requestCtx.Done():
				cancel()
			case <-newCtx.Done():
			}
		}()
	}
	newCtx = context.WithValue(newCtx, "gin", c)
	newCtx = context.WithValue(newCtx, "handler", handler)
	return newCtx, func(params ...interface{}) {
		if h.Cfg.RequestLog && len(params) == 1 {
			if existing, exists := c.Get("API_RESPONSE"); exists {
				if existingBytes, ok := existing.([]byte); ok && len(bytes.TrimSpace(existingBytes)) > 0 {
					switch params[0].(type) {
					case error, string:
						cancel()
						return
					}
				}
			}

			var payload []byte
			switch data := params[0].(type) {
			case []byte:
				payload = data
			case error:
				if data != nil {
					payload = []byte(data.Error())
				}
			case string:
				payload = []byte(data)
			}
			if len(payload) > 0 {
				if existing, exists := c.Get("API_RESPONSE"); exists {
					if existingBytes, ok := existing.([]byte); ok && len(existingBytes) > 0 {
						trimmedPayload := bytes.TrimSpace(payload)
						if len(trimmedPayload) > 0 && bytes.Contains(existingBytes, trimmedPayload) {
							cancel()
							return
						}
					}
				}
				appendAPIResponse(c, payload)
			}
		}

		cancel()
	}
}

// StartNonStreamingKeepAlive emits blank lines every 5 seconds while waiting for a non-streaming response.
// It returns a stop function that must be called before writing the final response.
func (h *BaseAPIHandler) StartNonStreamingKeepAlive(c *gin.Context, ctx context.Context) func() {
	if h == nil || c == nil {
		return func() {}
	}
	interval := NonStreamingKeepAliveInterval(h.Cfg)
	if interval <= 0 {
		return func() {}
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	stopChan := make(chan struct{})
	var stopOnce sync.Once
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = c.Writer.Write([]byte("\n"))
				flusher.Flush()
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			close(stopChan)
		})
		wg.Wait()
	}
}

// appendAPIResponse preserves any previously captured API response and appends new data.
func appendAPIResponse(c *gin.Context, data []byte) {
	if c == nil || len(data) == 0 {
		return
	}

	// Capture timestamp on first API response
	if _, exists := c.Get("API_RESPONSE_TIMESTAMP"); !exists {
		c.Set("API_RESPONSE_TIMESTAMP", time.Now())
	}

	if existing, exists := c.Get("API_RESPONSE"); exists {
		if existingBytes, ok := existing.([]byte); ok && len(existingBytes) > 0 {
			combined := make([]byte, 0, len(existingBytes)+len(data)+1)
			combined = append(combined, existingBytes...)
			if existingBytes[len(existingBytes)-1] != '\n' {
				combined = append(combined, '\n')
			}
			combined = append(combined, data...)
			c.Set("API_RESPONSE", combined)
			return
		}
	}

	c.Set("API_RESPONSE", bytes.Clone(data))
}

// ExecuteWithAuthManager executes a non-streaming request via the core auth manager.
// This path is the only supported execution route.
func (h *BaseAPIHandler) ExecuteWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, *interfaces.ErrorMessage) {
	reqMeta := requestExecutionMetadata(ctx)
	providers, normalizedModel, errMsg := h.getRequestDetails(modelName)
	if errMsg != nil {
		if policy := apiKeyPolicyFromContext(ctx); policy != nil {
			targetModel, enabled := policy.ClaudeFailoverTargetModel()
			if enabled && strings.TrimSpace(targetModel) != "" && targetModel != modelName && seemsClaudeModel(modelName) && isClaudeFailoverEligible(errMsg.StatusCode, errMsg.Error) {
				failoverPayload := rewriteModelField(rawJSON, targetModel)
				failoverProviders, failoverModel, detailErr := h.getRequestDetails(targetModel)
				if detailErr == nil {
					clientKey := util.HideAPIKey(clientAPIKeyFromContext(ctx))
					log.WithFields(log.Fields{
						"component":       "failover",
						"client_api_key":  clientKey,
						"from_provider":   "claude",
						"from_model":      modelName,
						"to_model":        failoverModel,
						"status_code":     errMsg.StatusCode,
						"error_message":   extractErrorMessage(errString(errMsg.Error)),
						"handler_format":  handlerType,
						"idempotency_key": reqMeta[idempotencyKeyMetadataKey],
						"reason":          "unknown_provider",
					}).Warn("triggering automatic failover for Claude request (unknown provider)")

					rawJSON = failoverPayload
					providers = failoverProviders
					normalizedModel = failoverModel
				} else {
					return nil, detailErr
				}
			} else {
				return nil, errMsg
			}
		} else {
			return nil, errMsg
		}
	}
	reqMeta[coreexecutor.RequestedModelMetadataKey] = normalizedModel
	payload := rawJSON
	if len(payload) == 0 {
		payload = nil
	}
	req := coreexecutor.Request{
		Model:   normalizedModel,
		Payload: payload,
	}
	opts := coreexecutor.Options{
		Stream:          false,
		Alt:             alt,
		OriginalRequest: rawJSON,
		SourceFormat:    sdktranslator.FromString(handlerType),
	}
	opts.Metadata = reqMeta

	execOnce := func(execProviders []string, execReq coreexecutor.Request, execOpts coreexecutor.Options) ([]byte, *interfaces.ErrorMessage) {
		resp, err := h.AuthManager.Execute(ctx, execProviders, execReq, execOpts)
		if err != nil {
			status := http.StatusInternalServerError
			if se, ok := err.(interface{ StatusCode() int }); ok && se != nil {
				if code := se.StatusCode(); code > 0 {
					status = code
				}
			}
			var addon http.Header
			if he, ok := err.(interface{ Headers() http.Header }); ok && he != nil {
				if hdr := he.Headers(); hdr != nil {
					addon = hdr.Clone()
				}
			}
			return nil, &interfaces.ErrorMessage{StatusCode: status, Error: err, Addon: addon}
		}
		return resp.Payload, nil
	}

	out, execErr := execOnce(providers, req, opts)
	if execErr == nil {
		return out, nil
	}

	// Optional per-client API key failover: Claude -> configured target model.
	policy := apiKeyPolicyFromContext(ctx)
	targetModel := ""
	enabled := false
	if policy != nil {
		targetModel, enabled = policy.ClaudeFailoverTargetModel()
	}
	if enabled && containsProvider(providers, "claude") && strings.TrimSpace(targetModel) != "" && targetModel != normalizedModel {
		status := execErr.StatusCode
		if status <= 0 {
			status = statusFromError(execErr.Error)
		}
		if isClaudeFailoverEligible(status, execErr.Error) {
			failoverPayload := rewriteModelField(rawJSON, targetModel)
			failoverProviders, failoverModel, detailErr := h.getRequestDetails(targetModel)
			if detailErr == nil {
				failoverReqMeta := make(map[string]any, len(reqMeta)+1)
				for k, v := range reqMeta {
					failoverReqMeta[k] = v
				}
				failoverReqMeta[coreexecutor.RequestedModelMetadataKey] = failoverModel
				failoverReq := coreexecutor.Request{Model: failoverModel, Payload: failoverPayload}
				failoverOpts := opts
				failoverOpts.OriginalRequest = failoverPayload
				failoverOpts.Metadata = failoverReqMeta

				clientKey := util.HideAPIKey(clientAPIKeyFromContext(ctx))
				log.WithFields(log.Fields{
					"component":       "failover",
					"client_api_key":  clientKey,
					"from_provider":   "claude",
					"from_model":      normalizedModel,
					"to_model":        failoverModel,
					"status_code":     status,
					"error_message":   extractErrorMessage(errString(execErr.Error)),
					"handler_format":  handlerType,
					"idempotency_key": reqMeta[idempotencyKeyMetadataKey],
				}).Warn("triggering automatic failover for Claude request")

				failoverOut, failoverErr := execOnce(failoverProviders, failoverReq, failoverOpts)
				if failoverErr == nil {
					return failoverOut, nil
				}
				return nil, failoverErr
			}
			_ = detailErr
		}
	}

	return nil, execErr
}

// ExecuteCountWithAuthManager executes a non-streaming request via the core auth manager.
// This path is the only supported execution route.
func (h *BaseAPIHandler) ExecuteCountWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) ([]byte, *interfaces.ErrorMessage) {
	reqMeta := requestExecutionMetadata(ctx)
	providers, normalizedModel, errMsg := h.getRequestDetails(modelName)
	if errMsg != nil {
		if policy := apiKeyPolicyFromContext(ctx); policy != nil {
			targetModel, enabled := policy.ClaudeFailoverTargetModel()
			if enabled && strings.TrimSpace(targetModel) != "" && targetModel != modelName && seemsClaudeModel(modelName) && isClaudeFailoverEligible(errMsg.StatusCode, errMsg.Error) {
				failoverPayload := rewriteModelField(rawJSON, targetModel)
				failoverProviders, failoverModel, detailErr := h.getRequestDetails(targetModel)
				if detailErr == nil {
					clientKey := util.HideAPIKey(clientAPIKeyFromContext(ctx))
					log.WithFields(log.Fields{
						"component":       "failover",
						"client_api_key":  clientKey,
						"from_provider":   "claude",
						"from_model":      modelName,
						"to_model":        failoverModel,
						"status_code":     errMsg.StatusCode,
						"error_message":   extractErrorMessage(errString(errMsg.Error)),
						"handler_format":  handlerType,
						"idempotency_key": reqMeta[idempotencyKeyMetadataKey],
						"reason":          "unknown_provider",
					}).Warn("triggering automatic failover for Claude count request (unknown provider)")

					rawJSON = failoverPayload
					providers = failoverProviders
					normalizedModel = failoverModel
				} else {
					return nil, detailErr
				}
			} else {
				return nil, errMsg
			}
		} else {
			return nil, errMsg
		}
	}
	reqMeta[coreexecutor.RequestedModelMetadataKey] = normalizedModel
	payload := rawJSON
	if len(payload) == 0 {
		payload = nil
	}
	req := coreexecutor.Request{
		Model:   normalizedModel,
		Payload: payload,
	}
	opts := coreexecutor.Options{
		Stream:          false,
		Alt:             alt,
		OriginalRequest: rawJSON,
		SourceFormat:    sdktranslator.FromString(handlerType),
	}
	opts.Metadata = reqMeta

	execOnce := func(execProviders []string, execReq coreexecutor.Request, execOpts coreexecutor.Options) ([]byte, *interfaces.ErrorMessage) {
		resp, err := h.AuthManager.ExecuteCount(ctx, execProviders, execReq, execOpts)
		if err != nil {
			status := http.StatusInternalServerError
			if se, ok := err.(interface{ StatusCode() int }); ok && se != nil {
				if code := se.StatusCode(); code > 0 {
					status = code
				}
			}
			var addon http.Header
			if he, ok := err.(interface{ Headers() http.Header }); ok && he != nil {
				if hdr := he.Headers(); hdr != nil {
					addon = hdr.Clone()
				}
			}
			return nil, &interfaces.ErrorMessage{StatusCode: status, Error: err, Addon: addon}
		}
		return resp.Payload, nil
	}

	out, execErr := execOnce(providers, req, opts)
	if execErr == nil {
		return out, nil
	}

	policy := apiKeyPolicyFromContext(ctx)
	targetModel := ""
	enabled := false
	if policy != nil {
		targetModel, enabled = policy.ClaudeFailoverTargetModel()
	}
	if enabled && containsProvider(providers, "claude") && strings.TrimSpace(targetModel) != "" && targetModel != normalizedModel {
		status := execErr.StatusCode
		if status <= 0 {
			status = statusFromError(execErr.Error)
		}
		if isClaudeFailoverEligible(status, execErr.Error) {
			failoverPayload := rewriteModelField(rawJSON, targetModel)
			failoverProviders, failoverModel, detailErr := h.getRequestDetails(targetModel)
			if detailErr == nil {
				failoverReqMeta := make(map[string]any, len(reqMeta)+1)
				for k, v := range reqMeta {
					failoverReqMeta[k] = v
				}
				failoverReqMeta[coreexecutor.RequestedModelMetadataKey] = failoverModel
				failoverReq := coreexecutor.Request{Model: failoverModel, Payload: failoverPayload}
				failoverOpts := opts
				failoverOpts.OriginalRequest = failoverPayload
				failoverOpts.Metadata = failoverReqMeta
				failoverOut, failoverErr := execOnce(failoverProviders, failoverReq, failoverOpts)
				if failoverErr == nil {
					return failoverOut, nil
				}
				return nil, failoverErr
			}
			_ = detailErr
		}
	}

	return nil, execErr
}

// ExecuteStreamWithAuthManager executes a streaming request via the core auth manager.
// This path is the only supported execution route.
func (h *BaseAPIHandler) ExecuteStreamWithAuthManager(ctx context.Context, handlerType, modelName string, rawJSON []byte, alt string) (<-chan []byte, <-chan *interfaces.ErrorMessage) {
	reqMeta := requestExecutionMetadata(ctx)
	providers, normalizedModel, errMsg := h.getRequestDetails(modelName)
	if errMsg != nil {
		if policy := apiKeyPolicyFromContext(ctx); policy != nil {
			targetModel, enabled := policy.ClaudeFailoverTargetModel()
			if enabled && strings.TrimSpace(targetModel) != "" && targetModel != modelName && seemsClaudeModel(modelName) && isClaudeFailoverEligible(errMsg.StatusCode, errMsg.Error) {
				failoverPayload := rewriteModelField(rawJSON, targetModel)
				failoverProviders, failoverModel, detailErr := h.getRequestDetails(targetModel)
				if detailErr == nil {
					clientKey := util.HideAPIKey(clientAPIKeyFromContext(ctx))
					log.WithFields(log.Fields{
						"component":       "failover",
						"client_api_key":  clientKey,
						"from_provider":   "claude",
						"from_model":      modelName,
						"to_model":        failoverModel,
						"status_code":     errMsg.StatusCode,
						"error_message":   extractErrorMessage(errString(errMsg.Error)),
						"handler_format":  handlerType,
						"idempotency_key": reqMeta[idempotencyKeyMetadataKey],
						"reason":          "unknown_provider",
					}).Warn("triggering automatic failover for Claude streaming request (unknown provider)")

					rawJSON = failoverPayload
					providers = failoverProviders
					normalizedModel = failoverModel
				} else {
					errChan := make(chan *interfaces.ErrorMessage, 1)
					errChan <- detailErr
					close(errChan)
					return nil, errChan
				}
			} else {
				errChan := make(chan *interfaces.ErrorMessage, 1)
				errChan <- errMsg
				close(errChan)
				return nil, errChan
			}
		} else {
			errChan := make(chan *interfaces.ErrorMessage, 1)
			errChan <- errMsg
			close(errChan)
			return nil, errChan
		}
	}
	reqMeta[coreexecutor.RequestedModelMetadataKey] = normalizedModel
	payload := rawJSON
	if len(payload) == 0 {
		payload = nil
	}
	req := coreexecutor.Request{
		Model:   normalizedModel,
		Payload: payload,
	}
	opts := coreexecutor.Options{
		Stream:          true,
		Alt:             alt,
		OriginalRequest: rawJSON,
		SourceFormat:    sdktranslator.FromString(handlerType),
	}
	opts.Metadata = reqMeta

	var (
		failoverTargetModel string
		failoverEnabled     bool
		failoverAttempted   bool
	)
	if policy := apiKeyPolicyFromContext(ctx); policy != nil {
		failoverTargetModel, failoverEnabled = policy.ClaudeFailoverTargetModel()
	}

	execStream := func(execProviders []string, execReq coreexecutor.Request, execOpts coreexecutor.Options) (<-chan coreexecutor.StreamChunk, *interfaces.ErrorMessage) {
		stream, err := h.AuthManager.ExecuteStream(ctx, execProviders, execReq, execOpts)
		if err == nil {
			return stream, nil
		}
		status := http.StatusInternalServerError
		if se, ok := err.(interface{ StatusCode() int }); ok && se != nil {
			if code := se.StatusCode(); code > 0 {
				status = code
			}
		}
		var addon http.Header
		if he, ok := err.(interface{ Headers() http.Header }); ok && he != nil {
			if hdr := he.Headers(); hdr != nil {
				addon = hdr.Clone()
			}
		}
		return nil, &interfaces.ErrorMessage{StatusCode: status, Error: err, Addon: addon}
	}

	chunks, execErr := execStream(providers, req, opts)
	if execErr != nil {
		// Immediate failure before any chunks are available - consider failover.
		status := execErr.StatusCode
		if status <= 0 {
			status = statusFromError(execErr.Error)
		}
		if failoverEnabled && containsProvider(providers, "claude") && failoverTargetModel != "" && failoverTargetModel != normalizedModel && isClaudeFailoverEligible(status, execErr.Error) {
			failoverAttempted = true
			failoverPayload := rewriteModelField(rawJSON, failoverTargetModel)
			failoverProviders, failoverModel, detailErr := h.getRequestDetails(failoverTargetModel)
			if detailErr == nil {
				failoverReqMeta := make(map[string]any, len(reqMeta)+1)
				for k, v := range reqMeta {
					failoverReqMeta[k] = v
				}
				failoverReqMeta[coreexecutor.RequestedModelMetadataKey] = failoverModel
				failoverReq := coreexecutor.Request{Model: failoverModel, Payload: failoverPayload}
				failoverOpts := opts
				failoverOpts.OriginalRequest = failoverPayload
				failoverOpts.Metadata = failoverReqMeta

				clientKey := util.HideAPIKey(clientAPIKeyFromContext(ctx))
				log.WithFields(log.Fields{
					"component":       "failover",
					"client_api_key":  clientKey,
					"from_provider":   "claude",
					"from_model":      normalizedModel,
					"to_model":        failoverModel,
					"status_code":     status,
					"error_message":   extractErrorMessage(errString(execErr.Error)),
					"handler_format":  handlerType,
					"idempotency_key": reqMeta[idempotencyKeyMetadataKey],
				}).Warn("triggering automatic failover for Claude streaming request")

				chunks, execErr = execStream(failoverProviders, failoverReq, failoverOpts)
				if execErr != nil {
					errChan := make(chan *interfaces.ErrorMessage, 1)
					errChan <- execErr
					close(errChan)
					return nil, errChan
				}
				// Update live variables for below goroutine.
				providers = failoverProviders
				normalizedModel = failoverModel
				req = failoverReq
				opts = failoverOpts
			}
			_ = detailErr
		}
		if execErr != nil {
			errChan := make(chan *interfaces.ErrorMessage, 1)
			errChan <- execErr
			close(errChan)
			return nil, errChan
		}
	}

	dataChan := make(chan []byte)
	errChan := make(chan *interfaces.ErrorMessage, 1)
	go func() {
		defer close(dataChan)
		defer close(errChan)
		sentPayload := false
		bootstrapRetries := 0
		maxBootstrapRetries := StreamingBootstrapRetries(h.Cfg)

		sendErr := func(msg *interfaces.ErrorMessage) bool {
			if ctx == nil {
				errChan <- msg
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case errChan <- msg:
				return true
			}
		}

		sendData := func(chunk []byte) bool {
			if ctx == nil {
				dataChan <- chunk
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case dataChan <- chunk:
				return true
			}
		}

		bootstrapEligible := func(err error) bool {
			status := statusFromError(err)
			if status == 0 {
				return true
			}
			switch status {
			case http.StatusUnauthorized, http.StatusForbidden, http.StatusPaymentRequired,
				http.StatusRequestTimeout, http.StatusTooManyRequests:
				return true
			default:
				return status >= http.StatusInternalServerError
			}
		}

	outer:
		for {
			for {
				var chunk coreexecutor.StreamChunk
				var ok bool
				if ctx != nil {
					select {
					case <-ctx.Done():
						return
					case chunk, ok = <-chunks:
					}
				} else {
					chunk, ok = <-chunks
				}
				if !ok {
					return
				}
				if chunk.Err != nil {
					streamErr := chunk.Err
					// Safe bootstrap recovery: if the upstream fails before any payload bytes are sent,
					// retry a few times (to allow auth rotation / transient recovery) and then attempt model fallback.
					if !sentPayload {
						if bootstrapRetries < maxBootstrapRetries && bootstrapEligible(streamErr) {
							bootstrapRetries++
							retryChunks, retryExecErr := execStream(providers, req, opts)
							if retryExecErr == nil {
								chunks = retryChunks
								continue outer
							}
							streamErr = &statusHeadersError{err: retryExecErr.Error, code: retryExecErr.StatusCode, addon: retryExecErr.Addon}
						}
					}

					// Optional failover: only before any payload bytes are sent.
					if !sentPayload && !failoverAttempted && failoverEnabled && containsProvider(providers, "claude") && failoverTargetModel != "" && failoverTargetModel != normalizedModel {
						status := statusFromError(streamErr)
						if isClaudeFailoverEligible(status, streamErr) {
							failoverAttempted = true
							failoverPayload := rewriteModelField(rawJSON, failoverTargetModel)
							failoverProviders, failoverModel, detailErr := h.getRequestDetails(failoverTargetModel)
							if detailErr == nil {
								failoverReqMeta := make(map[string]any, len(reqMeta)+1)
								for k, v := range reqMeta {
									failoverReqMeta[k] = v
								}
								failoverReqMeta[coreexecutor.RequestedModelMetadataKey] = failoverModel
								failoverReq := coreexecutor.Request{Model: failoverModel, Payload: failoverPayload}
								failoverOpts := opts
								failoverOpts.OriginalRequest = failoverPayload
								failoverOpts.Metadata = failoverReqMeta

								clientKey := util.HideAPIKey(clientAPIKeyFromContext(ctx))
								log.WithFields(log.Fields{
									"component":       "failover",
									"client_api_key":  clientKey,
									"from_provider":   "claude",
									"from_model":      normalizedModel,
									"to_model":        failoverModel,
									"status_code":     status,
									"error_message":   extractErrorMessage(errString(streamErr)),
									"handler_format":  handlerType,
									"idempotency_key": reqMeta[idempotencyKeyMetadataKey],
								}).Warn("triggering automatic failover for Claude streaming request (pre-first-byte)")

								retryChunks, retryExecErr := execStream(failoverProviders, failoverReq, failoverOpts)
								if retryExecErr == nil {
									// Swap state and restart outer loop on new chunks.
									providers = failoverProviders
									normalizedModel = failoverModel
									req = failoverReq
									opts = failoverOpts
									chunks = retryChunks
									bootstrapRetries = 0
									continue outer
								}
								streamErr = &statusHeadersError{err: retryExecErr.Error, code: retryExecErr.StatusCode, addon: retryExecErr.Addon}
							}
							_ = detailErr
						}
					}

					status := http.StatusInternalServerError
					if se, ok := streamErr.(interface{ StatusCode() int }); ok && se != nil {
						if code := se.StatusCode(); code > 0 {
							status = code
						}
					}
					var addon http.Header
					if he, ok := streamErr.(interface{ Headers() http.Header }); ok && he != nil {
						if hdr := he.Headers(); hdr != nil {
							addon = hdr.Clone()
						}
					}
					_ = sendErr(&interfaces.ErrorMessage{StatusCode: status, Error: streamErr, Addon: addon})
					return
				}
				if len(chunk.Payload) > 0 {
					sentPayload = true
					if okSendData := sendData(cloneBytes(chunk.Payload)); !okSendData {
						return
					}
				}
			}
		}
	}()
	return dataChan, errChan
}

func statusFromError(err error) int {
	if err == nil {
		return 0
	}
	if se, ok := err.(interface{ StatusCode() int }); ok && se != nil {
		if code := se.StatusCode(); code > 0 {
			return code
		}
	}
	return 0
}

func (h *BaseAPIHandler) getRequestDetails(modelName string) (providers []string, normalizedModel string, err *interfaces.ErrorMessage) {
	resolvedModelName := modelName
	initialSuffix := thinking.ParseSuffix(modelName)
	if initialSuffix.ModelName == "auto" {
		resolvedBase := util.ResolveAutoModel(initialSuffix.ModelName)
		if initialSuffix.HasSuffix {
			resolvedModelName = fmt.Sprintf("%s(%s)", resolvedBase, initialSuffix.RawSuffix)
		} else {
			resolvedModelName = resolvedBase
		}
	} else {
		resolvedModelName = util.ResolveAutoModel(modelName)
	}

	parsed := thinking.ParseSuffix(resolvedModelName)
	baseModel := strings.TrimSpace(parsed.ModelName)

	providers = util.GetProviderName(baseModel)
	// Fallback: if baseModel has no provider but differs from resolvedModelName,
	// try using the full model name. This handles edge cases where custom models
	// may be registered with their full suffixed name (e.g., "my-model(8192)").
	// Evaluated in Story 11.8: This fallback is intentionally preserved to support
	// custom model registrations that include thinking suffixes.
	if len(providers) == 0 && baseModel != resolvedModelName {
		providers = util.GetProviderName(resolvedModelName)
	}

	// Heuristic fallback when the registry isn't warmed up yet (or a new model is used).
	// This path is intentionally scoped to request routing (not generic provider lookup),
	// so callers that depend on "registry only" semantics (e.g. AMP mapping) remain unchanged.
	if len(providers) == 0 {
		lower := strings.ToLower(strings.TrimSpace(baseModel))
		switch {
		case strings.HasPrefix(lower, "claude-"):
			providers = []string{"claude"}
		case strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4") || strings.HasPrefix(lower, "chatgpt-"):
			providers = []string{"codex"}
		case strings.HasPrefix(lower, "gemini") || strings.HasPrefix(lower, "models/gemini") || strings.HasPrefix(lower, "vertex") || strings.HasPrefix(lower, "aistudio"):
			providers = []string{"gemini"}
		case strings.HasPrefix(lower, "qwen"):
			providers = []string{"qwen"}
		case strings.HasPrefix(lower, "kimi"):
			providers = []string{"kimi"}
		case strings.HasPrefix(lower, "iflow"):
			providers = []string{"iflow"}
		}
	}

	if len(providers) == 0 {
		return nil, "", &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: fmt.Errorf("unknown provider for model %s", modelName)}
	}

	// The thinking suffix is preserved in the model name itself, so no
	// metadata-based configuration passing is needed.
	return providers, resolvedModelName, nil
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// WriteErrorResponse writes an error message to the response writer using the HTTP status embedded in the message.
func (h *BaseAPIHandler) WriteErrorResponse(c *gin.Context, msg *interfaces.ErrorMessage) {
	status := http.StatusInternalServerError
	if msg != nil && msg.StatusCode > 0 {
		status = msg.StatusCode
	}
	if msg != nil && msg.Addon != nil {
		for key, values := range msg.Addon {
			if len(values) == 0 {
				continue
			}
			c.Writer.Header().Del(key)
			for _, value := range values {
				c.Writer.Header().Add(key, value)
			}
		}
	}

	errText := http.StatusText(status)
	if msg != nil && msg.Error != nil {
		if v := strings.TrimSpace(msg.Error.Error()); v != "" {
			errText = v
		}
	}

	body := BuildErrorResponseBody(status, errText)
	// Append first to preserve upstream response logs, then drop duplicate payloads if already recorded.
	var previous []byte
	if existing, exists := c.Get("API_RESPONSE"); exists {
		if existingBytes, ok := existing.([]byte); ok && len(existingBytes) > 0 {
			previous = existingBytes
		}
	}
	appendAPIResponse(c, body)
	trimmedErrText := strings.TrimSpace(errText)
	trimmedBody := bytes.TrimSpace(body)
	if len(previous) > 0 {
		if (trimmedErrText != "" && bytes.Contains(previous, []byte(trimmedErrText))) ||
			(len(trimmedBody) > 0 && bytes.Contains(previous, trimmedBody)) {
			c.Set("API_RESPONSE", previous)
		}
	}

	if !c.Writer.Written() {
		c.Writer.Header().Set("Content-Type", "application/json")
	}
	c.Status(status)
	_, _ = c.Writer.Write(body)
}

func (h *BaseAPIHandler) LoggingAPIResponseError(ctx context.Context, err *interfaces.ErrorMessage) {
	if h.Cfg.RequestLog {
		if ginContext, ok := ctx.Value("gin").(*gin.Context); ok {
			if apiResponseErrors, isExist := ginContext.Get("API_RESPONSE_ERROR"); isExist {
				if slicesAPIResponseError, isOk := apiResponseErrors.([]*interfaces.ErrorMessage); isOk {
					slicesAPIResponseError = append(slicesAPIResponseError, err)
					ginContext.Set("API_RESPONSE_ERROR", slicesAPIResponseError)
				}
			} else {
				// Create new response data entry
				ginContext.Set("API_RESPONSE_ERROR", []*interfaces.ErrorMessage{err})
			}
		}
	}
}

// APIHandlerCancelFunc is a function type for canceling an API handler's context.
// It can optionally accept parameters, which are used for logging the response.
type APIHandlerCancelFunc func(params ...interface{})
