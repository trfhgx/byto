package openai

import "encoding/json"

type ChatCompletionRequest struct {
	Model            string          `json:"model"`
	Messages         []Message       `json:"messages"`
	Stream           bool            `json:"stream,omitempty"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	Stop             json.RawMessage `json:"stop,omitempty"`
	Seed             *int            `json:"seed,omitempty"`
	ExtraBody        ExtraBody       `json:"extra_body,omitempty"`
}

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ExtraBody struct {
	Google GoogleExtra `json:"google,omitempty"`
}

type GoogleExtra struct {
	CachedContent string `json:"cached_content,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason,omitempty"`
}

type ResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

type ModelListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

type ModelInfo struct {
	ID                  string             `json:"id"`
	Object              string             `json:"object"`
	OwnedBy             string             `json:"owned_by"`
	DisplayName         string             `json:"display_name,omitempty"`
	Family              string             `json:"family,omitempty"`
	Enabled             *bool              `json:"enabled,omitempty"`
	Available           *bool              `json:"available,omitempty"`
	LaunchStage         string             `json:"launch_stage,omitempty"`
	VersionState        string             `json:"version_state,omitempty"`
	SupportedActions    []string           `json:"supported_actions,omitempty"`
	SupportedParameters []string           `json:"supported_parameters,omitempty"`
	Capabilities        *ModelCapabilities `json:"capabilities,omitempty"`
	Notes               string             `json:"notes,omitempty"`
	LastSeenAt          string             `json:"last_seen_at,omitempty"`
}

type ModelCapabilities struct {
	ReasoningEffort []string `json:"reasoning_effort,omitempty"`
	Input           []string `json:"input,omitempty"`
	Output          []string `json:"output,omitempty"`
	Streaming       *bool    `json:"streaming,omitempty"`
	Tools           *bool    `json:"tools,omitempty"`
	JSONMode        *bool    `json:"json_mode,omitempty"`
}

type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type StreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}
