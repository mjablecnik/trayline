package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openRouterBaseURL = "https://openrouter.ai/api/v1/chat/completions"
	llmTimeout        = 60 * time.Second
	systemPrompt      = `You are a pipeline condition evaluator. You will receive content and a condition to evaluate.
Analyze the content based on the condition and respond with exactly one word: true or false.
Do not include any other text, explanation, or formatting.`
)

// ConditionEvaluator abstracts LLM condition evaluation for testability.
type ConditionEvaluator interface {
	Evaluate(content string, conditionPrompt string) (bool, error)
}

// LLMClient sends requests to the OpenRouter API.
type LLMClient struct {
	APIKey  string
	Model   string
	BaseURL string
	client  *http.Client
}

// LLMRequest is the request body for the OpenRouter chat completions API.
type LLMRequest struct {
	Model    string       `json:"model"`
	Messages []LLMMessage `json:"messages"`
}

// LLMMessage is a single message in the chat completion request.
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMResponse is the response from the OpenRouter API.
type LLMResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// NewLLMClient creates a new LLMClient.
func NewLLMClient(apiKey, model string) *LLMClient {
	return &LLMClient{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: openRouterBaseURL,
		client:  &http.Client{Timeout: llmTimeout},
	}
}

// Evaluate sends content + prompt to the LLM and returns a boolean decision.
// Retries once on failure.
func (c *LLMClient) Evaluate(content string, conditionPrompt string) (bool, error) {
	result, err := c.doEvaluate(content, conditionPrompt)
	if err != nil {
		// Retry once
		result, err = c.doEvaluate(content, conditionPrompt)
		if err != nil {
			return false, fmt.Errorf("LLM request failed after retry: %w", err)
		}
	}
	return result, nil
}

func (c *LLMClient) doEvaluate(content string, conditionPrompt string) (bool, error) {
	userMsg := fmt.Sprintf("Condition: %s\n\nContent:\n%s", conditionPrompt, content)

	reqBody := LLMRequest{
		Model: c.Model,
		Messages: []LLMMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return false, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var llmResp LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return false, fmt.Errorf("decoding response: %w", err)
	}

	if len(llmResp.Choices) == 0 {
		return false, fmt.Errorf("LLM returned empty response")
	}

	return parseLLMDecision(llmResp.Choices[0].Message.Content)
}

// parseLLMDecision extracts a boolean from an LLM response string.
func parseLLMDecision(response string) (bool, error) {
	trimmed := strings.TrimSpace(strings.ToLower(response))
	if trimmed == "true" {
		return true, nil
	}
	if trimmed == "false" {
		return false, nil
	}
	// Check if it contains true or false (sometimes LLM adds punctuation)
	if strings.Contains(trimmed, "true") && !strings.Contains(trimmed, "false") {
		return true, nil
	}
	if strings.Contains(trimmed, "false") && !strings.Contains(trimmed, "true") {
		return false, nil
	}
	return false, fmt.Errorf("LLM returned unparseable response after retry")
}
