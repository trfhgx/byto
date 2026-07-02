package gemini

type GenerateRequest struct {
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	Contents          []Content         `json:"contents"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	CachedContent     string            `json:"cachedContent,omitempty"`
}

type RequestOptions struct {
	LLMRequestType       string
	LLMSharedRequestType string
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text,omitempty"`
}

type GenerationConfig struct {
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"topP,omitempty"`
	MaxOutputTokens  *int            `json:"maxOutputTokens,omitempty"`
	PresencePenalty  *float64        `json:"presencePenalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequencyPenalty,omitempty"`
	StopSequences    []string        `json:"stopSequences,omitempty"`
	Seed             *int            `json:"seed,omitempty"`
	ThinkingConfig   *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

type ThinkingConfig struct {
	ThinkingBudget  *int  `json:"thinkingBudget,omitempty"`
	IncludeThoughts *bool `json:"includeThoughts,omitempty"`
}

type GenerateResponse struct {
	Candidates     []Candidate    `json:"candidates"`
	UsageMetadata  UsageMetadata  `json:"usageMetadata"`
	PromptFeedback PromptFeedback `json:"promptFeedback,omitempty"`
}

type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
}

type UsageMetadata struct {
	PromptTokenCount        int    `json:"promptTokenCount"`
	CandidatesTokenCount    int    `json:"candidatesTokenCount"`
	TotalTokenCount         int    `json:"totalTokenCount"`
	CachedContentTokenCount int    `json:"cachedContentTokenCount"`
	ThoughtsTokenCount      int    `json:"thoughtsTokenCount,omitempty"`
	TrafficType             string `json:"trafficType,omitempty"`
}

type PromptFeedback struct {
	BlockReason string `json:"blockReason,omitempty"`
}
