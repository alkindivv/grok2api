package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

type hermesTerminalEnvelope struct {
	Output   string `json:"output"`
	ExitCode *int   `json:"exit_code"`
	Error    any    `json:"error"`
}

func normalizeHermesTerminalToolOutputs(body []byte) ([]byte, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, false, nil
	}
	items, ok := payload["input"].([]any)
	if !ok || len(items) == 0 {
		return body, false, nil
	}
	callTools := make(map[string]string)
	changed := false
	for index, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch strings.TrimSpace(stringField(item, "type")) {
		case "function_call":
			callID := strings.TrimSpace(stringField(item, "call_id"))
			name := strings.TrimSpace(stringField(item, "name"))
			if callID != "" && name != "" {
				callTools[callID] = name
			}
		case "function_call_output":
			callID := strings.TrimSpace(stringField(item, "call_id"))
			if callTools[callID] != "terminal" {
				continue
			}
			rawOutput, ok := item["output"].(string)
			if !ok || strings.TrimSpace(rawOutput) == "" {
				continue
			}
			var envelope hermesTerminalEnvelope
			if err := json.Unmarshal([]byte(rawOutput), &envelope); err != nil || envelope.ExitCode == nil {
				continue
			}
			var decoded map[string]any
			if err := json.Unmarshal([]byte(rawOutput), &decoded); err != nil {
				continue
			}
			if _, ok := decoded["output"]; !ok {
				continue
			}
			if _, ok := decoded["exit_code"]; !ok {
				continue
			}
			prompt := fmt.Sprintf("exit: %d\n%s", *envelope.ExitCode, envelope.Output)
			if message := hermesTerminalErrorText(envelope.Error); message != "" && !strings.Contains(envelope.Output, message) {
				if envelope.Output != "" {
					prompt += "\n"
				}
				prompt += message
			}
			item["output"] = prompt
			items[index] = item
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	payload["input"] = items
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("encode Hermes terminal compatibility payload: %w", err)
	}
	return encoded, true, nil
}

func hermesTerminalErrorText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		text := strings.TrimSpace(string(encoded))
		if text == "null" || text == "{}" || text == "[]" || text == `""` {
			return ""
		}
		return text
	}
}
