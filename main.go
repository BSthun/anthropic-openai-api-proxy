package main

import (
	fiber2 "anthropic-openai-api-proxy/common/fiber"
	"anthropic-openai-api-proxy/procedure"
	"anthropic-openai-api-proxy/type/extern"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/bsthun/gut"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/ollama/ollama/api"
)

func main() {
	// * config
	ollamaEndpoint := "https://jtkkibqf2kntke-11434.proxy.runpod.net"
	ollamaModel := "qwen2.5-coder:32b"
	listen := ":3880"

	// * initialize ollama client
	ollamaEndpointUrl, err := url.Parse(ollamaEndpoint)
	if err != nil {
		gut.Fatal("failed to parse url", err)
	}

	client := api.NewClient(ollamaEndpointUrl, &http.Client{
		Timeout: 1 * time.Hour,
	})

	// * create fiber app
	app := fiber.New(fiber.Config{
		BodyLimit:    25 * 1024 * 1024,
		ErrorHandler: fiber2.HandleError,
	})

	// * add logging middleware
	app.Use(logger.New())

	// * health check endpoint
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
		})
	})

	// * anthropic chat completions endpoint
	app.Post("/v1/messages", func(c *fiber.Ctx) error {
		body := new(extern.Request)
		if err := c.BodyParser(&body); err != nil {
			return gut.Err(false, "unable to parse body", err)
		}

		// * validate basic requirements
		if body.Model == nil || *body.Model == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Model is required",
			})
		}

		// * extract system prompt if present
		var systemPrompt string
		if body.System != nil && len(body.System) > 0 && body.System[0].Text != nil {
			systemPrompt = *body.System[0].Text
		}

		// * convert anthropic messages to ollama format
		var ollamaMessages []api.Message

		// * add system message if present
		if systemPrompt != "" {
			ollamaMessages = append(ollamaMessages, api.Message{
				Role:    "system",
				Content: systemPrompt,
			})
		}

		// * process messages to extract text and images
		for _, message := range body.Messages {
			if message.Role == nil {
				continue
			}

			ollamaMessage := api.Message{
				Role:      strings.ToLower(*message.Role),
				Content:   "",
				Images:    nil,
				ToolCalls: nil,
			}

			if message.Content.Text != nil {
				ollamaMessage.Content = *message.Content.Text
			} else {
				for _, content := range message.Content.Content {
					if content.Type == nil {
						continue
					}

					switch *content.Type {
					case "text":
						ollamaMessage.Content = *content.Text
					case "image":
						// TODO: Add image handling
					case "tool_use":
						if content.Name != nil && content.Input != nil {
							// Add function call to the message
							ollamaMessage.ToolCalls = append(ollamaMessage.ToolCalls, api.ToolCall{
								Function: api.ToolCallFunction{
									Name:      *content.Name,
									Arguments: content.Input,
									Index:     0,
								},
							})
						}
					case "tool_result":
						ollamaMessage.Content = *content.Content
					}
				}
			}

			// * add message to list
			ollamaMessages = append(ollamaMessages, ollamaMessage)
		}

		// * prepare options
		options := map[string]any{
			"num_ctx":     32768,
			"num_predict": 32768,
		}

		// * set max tokens if provided
		if body.MaxTokens != nil {
			options["num_predict"] = *body.MaxTokens
		}

		// * set temperature if provided
		if body.Temperature != nil {
			options["temperature"] = *body.Temperature
		}

		// * prepare the ollama request
		request := &api.ChatRequest{
			Model:     ollamaModel,
			Messages:  ollamaMessages,
			Stream:    gut.Ptr(false),
			Format:    nil,
			KeepAlive: nil,
			Tools:     procedure.ConvertAnthropicToolsToOllama(body.Tools),
			Options:   options,
		}

		// * if streaming is requested
		isStreaming := false
		if body.Stream != nil && *body.Stream {
			request.Stream = gut.Ptr(true)
			isStreaming = true
		}

		if isStreaming {
			// * setup streaming response
			c.Set("Content-Type", "text/event-stream")
			c.Set("Cache-Control", "no-cache")
			c.Set("Connection", "keep-alive")
			c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
				// * generate message id
				messageId := "msg_" + *gut.Random(gut.RandomSet.MixedAlphaNum, 24)

				// * send message_start event
				messageStart := map[string]any{
					"type": "message_start",
					"message": map[string]any{
						"id":            messageId,
						"type":          "message",
						"role":          "assistant",
						"model":         *body.Model,
						"content":       []map[string]any{},
						"stop_reason":   nil,
						"stop_sequence": nil,
						"usage": map[string]any{
							"input_tokens":                15,
							"cache_creation_input_tokens": 0,
							"cache_read_input_tokens":     0,
							"output_tokens":               1,
						},
					},
				}
				if messageStartData, err := json.Marshal(messageStart); err == nil {
					_, _ = fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", messageStartData)
				}

				// Initial flag to track if we need to handle tool calls for streaming
				hasToolCalls := false
				var toolCallId string
				var toolCallName string
				var toolCallArgs string

				// * call ollama with streaming to check for tool calls
				err = client.Chat(context.Background(), request, func(resp api.ChatResponse) error {
					if len(resp.Message.ToolCalls) > 0 && !hasToolCalls {
						hasToolCalls = true
						toolCallId = fmt.Sprintf("tc_%s", *gut.Random(gut.RandomSet.MixedAlphaNum, 24))
						if len(resp.Message.ToolCalls) > 0 {
							toolCallName = resp.Message.ToolCalls[0].Function.Name
							toolCallArgs = resp.Message.ToolCalls[0].Function.Arguments.String()
						}
						return nil
					}
					return nil
				})

				// * send content_block_start event based on whether we have tool calls
				contentBlockType := "text"
				contentBlock := map[string]any{
					"type": "text",
					"text": "",
				}
				_ = contentBlockType

				if hasToolCalls {
					contentBlockType = "tool_use"

					// Parse arguments from string to map if possible
					var inputMap map[string]any
					var toolInput any = map[string]any{}

					if toolCallArgs != "" {
						if err := json.Unmarshal([]byte(toolCallArgs), &inputMap); err == nil {
							toolInput = inputMap
						} else {
							toolInput = toolCallArgs
						}
					}

					contentBlock = map[string]any{
						"type":  "tool_use",
						"id":    toolCallId,
						"name":  toolCallName,
						"input": toolInput,
					}
				}

				contentBlockStart := map[string]any{
					"type":          "content_block_start",
					"index":         0,
					"content_block": contentBlock,
				}

				if contentBlockStartData, err := json.Marshal(contentBlockStart); err == nil {
					_, _ = fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", contentBlockStartData)
				}

				// * send initial ping event
				pingEvent := map[string]any{
					"type": "ping",
				}
				if pingData, err := json.Marshal(pingEvent); err == nil {
					_, _ = fmt.Fprintf(w, "event: ping\ndata: %s\n\n", pingData)
				}

				// * call ollama with streaming
				var accumulatedResponse string
				var toolCallData map[string]any
				outputTokens := 0

				err = client.Chat(context.Background(), request, func(resp api.ChatResponse) error {
					// * accumulate response
					accumulatedResponse += resp.Message.Content
					outputTokens += 1

					// Check for tool calls
					if len(resp.Message.ToolCalls) > 0 {
						// Handle tool call streaming
						for _, tc := range resp.Message.ToolCalls {
							// Only process if we have function data
							if tc.Function.Name != "" {
								// Create or update tool call data
								toolCallData = map[string]any{
									"name":  tc.Function.Name,
									"input": tc.Function.Arguments,
								}

								// Create Anthropic delta format
								chunk := map[string]any{
									"type":  "content_block_delta",
									"index": 0,
									"delta": map[string]any{
										"type":     "tool_use_delta",
										"tool_use": toolCallData,
									},
								}

								// Serialize and send chunk
								if chunkData, err := json.Marshal(chunk); err == nil {
									_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", chunkData)
								}
							}
						}
					} else if resp.Message.Content != "" {
						// Regular text content streaming
						chunk := map[string]any{
							"type":  "content_block_delta",
							"index": 0,
							"delta": map[string]any{
								"type": "text_delta",
								"text": resp.Message.Content,
							},
						}

						// * serialize and send chunk
						if chunkData, err := json.Marshal(chunk); err == nil {
							_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", chunkData)
						}
					}

					return nil
				})

				if err != nil {
					// * handle error in stream
					errorChunk := map[string]any{
						"type": "error",
						"error": map[string]any{
							"message": "ollama response error",
							"type":    "server_error",
						},
					}

					if errorData, err := json.Marshal(errorChunk); err == nil {
						_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", errorData)
					}
				}

				// * send content_block_stop
				contentBlockStop := map[string]any{
					"type":  "content_block_stop",
					"index": 0,
				}
				if contentBlockStopData, err := json.Marshal(contentBlockStop); err == nil {
					_, _ = fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", contentBlockStopData)
				}

				// * send message_delta
				messageDelta := map[string]any{
					"type": "message_delta",
					"delta": map[string]any{
						"stop_reason":   "end_turn",
						"stop_sequence": nil,
					},
					"usage": map[string]any{
						"output_tokens": outputTokens,
					},
				}
				if messageDeltaData, err := json.Marshal(messageDelta); err == nil {
					_, _ = fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", messageDeltaData)
				}

				// * send message_stop
				messageStop := map[string]any{
					"type": "message_stop",
				}
				if messageStopData, err := json.Marshal(messageStop); err == nil {
					_, _ = fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", messageStopData)
				}
			})

			return nil
		}

		// * call ollama for non-streaming response
		var output string
		var toolCalls []api.ToolCall

		err = client.Chat(context.Background(), request, func(resp api.ChatResponse) error {
			output += resp.Message.Content
			if len(resp.Message.ToolCalls) > 0 {
				toolCalls = resp.Message.ToolCalls
			}
			return nil
		})

		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "ollama response error",
				"details": err.Error(),
			})
		}

		// * create content blocks based on response type
		var contentBlocks []anthropic.ContentBlock

		if len(toolCalls) > 0 {
			// * process tool calls into Anthropic format
			contentBlocks = procedure.ProcessToolCalls(toolCalls)
		} else {
			// * regular text response
			contentBlocks = []anthropic.ContentBlock{{Type: anthropic.ContentBlockTypeText, Text: output}}
		}

		// * create anthropic response format
		response := anthropic.Message{
			ID:         "msg_" + *gut.Random(gut.RandomSet.MixedAlphaNum, 24),
			Type:       "message",
			Role:       anthropic.MessageRoleAssistant,
			Content:    contentBlocks,
			Model:      *body.Model,
			StopReason: "end_turn",
			Usage: anthropic.Usage{
				InputTokens:  100,
				OutputTokens: 100,
			},
		}

		return c.JSON(response)
	})

	// * start the server
	log.Fatal(app.Listen(listen))
}
