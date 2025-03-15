package procedure

import (
	"anthropic-openai-api-proxy/type/extern"
	"encoding/json"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/ollama/ollama/api"
)

func ConvertAnthropicToolsToOllama(tools []*extern.Tool) []api.Tool {
	// * return nil if no tools provided
	if len(tools) == 0 {
		return nil
	}

	ollamaTools := make([]api.Tool, 0, len(tools))

	for _, tool := range tools {
		// * skip invalid tools
		if tool.Name == nil || tool.Description == nil || tool.InputSchema == nil {
			continue
		}

		// * create base ollama tool
		ollamaTool := api.Tool{
			Type: "function",
			Function: api.ToolFunction{
				Name:        *tool.Name,
				Description: *tool.Description,
			},
		}

		// * initialize parameters structure based on the actual Ollama API type
		ollamaTool.Function.Parameters.Type = "object"
		ollamaTool.Function.Parameters.Required = []string{}
		ollamaTool.Function.Parameters.Properties = map[string]struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Enum        []string `json:"enum,omitempty"`
		}{}

		// * process required fields
		if tool.InputSchema.Required != nil {
			for _, req := range tool.InputSchema.Required {
				if req != nil {
					ollamaTool.Function.Parameters.Required = append(ollamaTool.Function.Parameters.Required, *req)
				}
			}
		}

		// * process all properties
		if tool.InputSchema.Properties != nil {
			for propName, propDetails := range tool.InputSchema.Properties {
				if propDetails == nil {
					continue
				}

				propType := "string"
				if propDetails.Type != nil {
					propType = *propDetails.Type
				}

				propDesc := ""
				if propDetails.Description != nil {
					propDesc = *propDetails.Description
				}

				var enumValues []string
				if propDetails.Enum != nil && len(propDetails.Enum) > 0 {
					for _, e := range propDetails.Enum {
						if e != nil {
							enumValues = append(enumValues, *e)
						}
					}
				}

				ollamaTool.Function.Parameters.Properties[propName] = struct {
					Type        string   `json:"type"`
					Description string   `json:"description"`
					Enum        []string `json:"enum,omitempty"`
				}{
					Type:        propType,
					Description: propDesc,
					Enum:        enumValues,
				}
			}
		}

		ollamaTools = append(ollamaTools, ollamaTool)
	}

	return ollamaTools
}

func ProcessToolCalls(toolCalls []api.ToolCall) []anthropic.ContentBlock {
	if len(toolCalls) == 0 {
		return nil
	}

	contentBlocks := make([]anthropic.ContentBlock, 0, len(toolCalls))

	for _, tc := range toolCalls {
		// Parse arguments from string to map
		raw, err := json.Marshal(tc.Function.Arguments)
		if err != nil {
			continue
		}

		contentBlock := anthropic.ContentBlock{
			Type:  "tool_use",
			Input: raw,
		}

		contentBlocks = append(contentBlocks, contentBlock)
	}

	return contentBlocks
}
