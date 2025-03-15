package extern

import "encoding/json"

type Request struct {
	MaxTokens   *int              `json:"max_tokens"`
	Messages    []*RequestMessage `json:"messages"`
	UserId      *string           `json:"user_id"`
	Model       *string           `json:"model"`
	Stream      *bool             `json:"stream"`
	System      []*SystemMessage  `json:"system"`
	Temperature *float64          `json:"temperature"`
	Tools       []*Tool           `json:"tools"`
}

type RequestMessage struct {
	Content *RequestContent `json:"content,omitempty"`
	Role    *string         `json:"role"`
}

type RequestContent struct {
	Content []*ContentItem `json:"content"`
	Text    *string        `json:"text"`
}

func (r *RequestContent) UnmarshalJSON(data []byte) error {
	var content []*ContentItem
	if err := json.Unmarshal(data, &content); err == nil {
		r.Content = content
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}

	r.Text = &text
	return nil
}

type ContentItem struct {
	CacheControlType *CacheControl `json:"cache_control,omitempty"`
	Type             *string       `json:"type"`
	Text             *string       `json:"text"`
}

type SystemMessage struct {
	CacheControlType *CacheControl `json:"cache_control,omitempty"`
	Text             *string       `json:"text"`
	Type             *string       `json:"type"`
}

type CacheControl struct {
	Type string `json:"type"`
}

type Tool struct {
	Description *string      `json:"description"`
	InputSchema *InputSchema `json:"input_schema"`
	Name        *string      `json:"name"`
}

type InputSchema struct {
	Schema               *string           `json:"$schema"`
	AdditionalProperties *bool             `json:"additionalProperties"`
	Properties           *SchemaProperties `json:"properties"`
	Required             []*string         `json:"required"`
	Type                 *string           `json:"type"`
}

type SchemaProperties struct {
	Prompt *PromptProperty `json:"prompt"`
}

type PromptProperty struct {
	Description *string `json:"description"`
	Type        *string `json:"type"`
}
