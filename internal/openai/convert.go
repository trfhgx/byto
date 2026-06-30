package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func MessageText(m Message) (string, error) {
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" || p.Type == "" {
				b.WriteString(p.Text)
			}
		}
		if b.Len() > 0 {
			return b.String(), nil
		}
	}
	return "", errors.New("only string content or text content parts are supported in v1")
}

func ValidateRequest(req ChatCompletionRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return errors.New("model is required")
	}
	if len(req.Messages) == 0 {
		return errors.New("messages is required")
	}
	for i, m := range req.Messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			return fmt.Errorf("messages[%d].role is required", i)
		}
		if role != "system" && role != "user" && role != "assistant" {
			return fmt.Errorf("messages[%d].role %q is unsupported", i, role)
		}
		if _, err := MessageText(m); err != nil {
			return fmt.Errorf("messages[%d].content: %w", i, err)
		}
	}
	if _, err := StopSequences(req.Stop); err != nil {
		return err
	}
	return nil
}

func StopSequences(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if one == "" {
			return nil, nil
		}
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		out := make([]string, 0, len(many))
		for _, s := range many {
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	}
	return nil, errors.New("stop must be a string or array of strings")
}
