package gemini

import (
	"encoding/json"
	"testing"

	"github.com/example/go-llm-gateway/internal/openai"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestFromOpenAIKeepsSystemSeparate(t *testing.T) {
	req := openai.ChatCompletionRequest{Messages: []openai.Message{
		{Role: "system", Content: raw(`"You are useful."`)},
		{Role: "user", Content: raw(`"Hello"`)},
	}}
	out, err := FromOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.SystemInstruction == nil {
		t.Fatal("missing systemInstruction")
	}
	if len(out.Contents) != 1 {
		t.Fatalf("expected one content, got %d", len(out.Contents))
	}
	if out.Contents[0].Role != "user" {
		t.Fatalf("role %s", out.Contents[0].Role)
	}
}

func TestCachedContentPassthrough(t *testing.T) {
	req := openai.ChatCompletionRequest{Messages: []openai.Message{{Role: "user", Content: raw(`"Hello"`)}}, ExtraBody: openai.ExtraBody{Google: openai.GoogleExtra{CachedContent: "cachedContents/123"}}}
	out, err := FromOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.CachedContent != "cachedContents/123" {
		t.Fatalf("got %s", out.CachedContent)
	}
}

func TestGenerationConfigPassthrough(t *testing.T) {
	frequencyPenalty := 0.4
	presencePenalty := 0.2
	seed := 7
	req := openai.ChatCompletionRequest{
		Messages:         []openai.Message{{Role: "user", Content: raw(`"Hello"`)}},
		FrequencyPenalty: &frequencyPenalty,
		PresencePenalty:  &presencePenalty,
		Stop:             raw(`["END"]`),
		Seed:             &seed,
	}
	out, err := FromOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.GenerationConfig.FrequencyPenalty == nil || *out.GenerationConfig.FrequencyPenalty != frequencyPenalty {
		t.Fatalf("frequency penalty not passed through: %#v", out.GenerationConfig.FrequencyPenalty)
	}
	if out.GenerationConfig.PresencePenalty == nil || *out.GenerationConfig.PresencePenalty != presencePenalty {
		t.Fatalf("presence penalty not passed through: %#v", out.GenerationConfig.PresencePenalty)
	}
	if got := out.GenerationConfig.StopSequences; len(got) != 1 || got[0] != "END" {
		t.Fatalf("stop sequences %#v", got)
	}
	if out.GenerationConfig.Seed == nil || *out.GenerationConfig.Seed != seed {
		t.Fatalf("seed not passed through: %#v", out.GenerationConfig.Seed)
	}
}
