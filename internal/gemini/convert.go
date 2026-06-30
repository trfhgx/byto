package gemini

import (
	"strings"

	"github.com/example/go-llm-gateway/internal/openai"
)

func FromOpenAI(req openai.ChatCompletionRequest) (GenerateRequest, error) {
	out := GenerateRequest{
		GenerationConfig: &GenerationConfig{
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			MaxOutputTokens: req.MaxTokens,
		},
	}
	if req.ExtraBody.Google.CachedContent != "" {
		out.CachedContent = req.ExtraBody.Google.CachedContent
	}
	for _, m := range req.Messages {
		text, err := openai.MessageText(m)
		if err != nil {
			return out, err
		}
		role := strings.TrimSpace(m.Role)
		if role == "system" {
			if out.SystemInstruction == nil {
				out.SystemInstruction = &Content{Parts: []Part{{Text: text}}}
			} else {
				out.SystemInstruction.Parts = append(out.SystemInstruction.Parts, Part{Text: text})
			}
			continue
		}
		vertexRole := "user"
		if role == "assistant" {
			vertexRole = "model"
		}
		out.Contents = append(out.Contents, Content{Role: vertexRole, Parts: []Part{{Text: text}}})
	}
	return out, nil
}
