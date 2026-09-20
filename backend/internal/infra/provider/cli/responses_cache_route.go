package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildPromptCacheRoute records internal tools added to route this request through the cache-capable path.
// injectedToolTypes restores the client's original visible tool list during response processing.
type buildPromptCacheRoute struct {
	filterXSearch       bool
	injectedToolTypes   map[string]struct{}
	clientDeclaredTools map[string]struct{}
}

func prepareBuildPromptCacheRoute(body []byte, operation, model, promptCacheKey string, allowClientTools bool) ([]byte, buildPromptCacheRoute, error) {
	route := buildPromptCacheRoute{
		injectedToolTypes:   make(map[string]struct{}),
		clientDeclaredTools: make(map[string]struct{}),
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, route, fmt.Errorf("解析 Build prompt cache 请求: %w", err)
	}
	if payload == nil {
		payload = make(map[string]json.RawMessage)
	}
	tools, err := buildCacheRouteTools(payload)
	if err != nil {
		return nil, route, err
	}
	for _, rawTool := range tools {
		kind, name := buildCacheToolIdentity(rawTool)
		if kind == "function" || kind == "custom" {
			if name != "" {
				route.clientDeclaredTools[name] = struct{}{}
			}
		}
		if kind == "x_search" {
			route.filterXSearch = true
		}
	}

	// Prompt caching is keyed by prompt_cache_key / x-grok-conv-id upstream.
	// Never invent hosted tools or broaden tool_choice to obtain cache affinity:
	// the client owns the capability surface. Explicit x_search remains visible
	// in route metadata so already-executed upstream search subcalls can be
	// filtered from the downstream response.
	_ = promptCacheKey
	_ = operation
	_ = model
	_ = allowClientTools
	return body, route, nil
}

func buildCacheRouteTools(payload map[string]json.RawMessage) ([]json.RawMessage, error) {
	raw, exists := payload["tools"]
	if !exists || isEmptyJSON(raw) {
		return nil, nil
	}
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return nil, &responsesRequestError{Message: "tools 必须是数组", Param: "tools", Code: "invalid_parameter"}
	}
	return tools, nil
}

func buildCacheToolIdentity(raw json.RawMessage) (kind, name string) {
	var tool struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &tool) != nil {
		return "", ""
	}
	return strings.TrimSpace(tool.Type), strings.TrimSpace(tool.Name)
}

func hasBuildCacheToolType(tools []json.RawMessage, kind string) bool {
	for _, rawTool := range tools {
		toolType, _ := buildCacheToolIdentity(rawTool)
		if toolType == kind {
			return true
		}
	}
	return false
}

func isBuildCacheConversationOperation(operation string) bool {
	switch strings.TrimSpace(operation) {
	case "", "responses", "chat", "messages":
		return true
	default:
		return false
	}
}

func isBuildCacheMediaModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "image") || strings.Contains(model, "imagine") || strings.Contains(model, "video")
}
