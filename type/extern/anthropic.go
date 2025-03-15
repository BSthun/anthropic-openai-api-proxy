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
	Type             *string        `json:"type"`
	Text             *string        `json:"text"`
	Id               *string        `json:"id"`
	Name             *string        `json:"name,omitempty"`
	Input            map[string]any `json:"input,omitempty"`
	Content          *string        `json:"content,omitempty"`
	ToolUseId        *string        `json:"tool_use_id,omitempty"`
	CacheControlType *CacheControl  `json:"cache_control,omitempty"`
}

// ToolResult represents the result of a tool call
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Type       string `json:"type"`
	Content    any    `json:"content"`
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
	Name        *string      `json:"name"`
	Description *string      `json:"description"`
	InputSchema *InputSchema `json:"input_schema"`
}

type InputSchema struct {
	Type                 *string                         `json:"type"`
	AdditionalProperties *bool                           `json:"additionalProperties"`
	Properties           map[string]*InputSchemaProperty `json:"properties"`
	Required             []*string                       `json:"required"`
}

type InputSchemaProperty struct {
	Description *string   `json:"description"`
	Type        *string   `json:"type"`
	Enum        []*string `json:"enum,omitempty"`
}
