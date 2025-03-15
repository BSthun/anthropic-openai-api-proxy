package main

import (
	fiber2 "anthropic-openai-api-proxy/common/fiber"
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
	ollamaEndpoint := "http://10.2.1.134:11434"
	ollamaModel := "qwen2.5-coder:14b"
	listen := ":3880"

	// * initialize ollama client
	ollamaEndpointUrl, err := url.Parse(ollamaEndpoint)
	if err != nil {
		gut.Fatal("failed to parse url", err)
	}

	client := api.NewClient(ollamaEndpointUrl, &http.Client{
		Timeout: 60 * time.Second,
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
			println(err.Error())
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

			ollamaMsg := api.Message{
				Role: strings.ToLower(*message.Role), // convert Anthropic role to Ollama role
			}

			var textParts []string

			if message.Content.Text != nil {
				// * add text content
				textParts = append(textParts, *message.Content.Text)
			} else {
				for _, content := range message.Content.Content {
					if content.Type == nil {
						continue
					}

					switch *content.Type {
					case "text":
						// * add text content
						if content.Text != nil {
							textParts = append(textParts, *content.Text)
						}
					case "image":
						// TODO: handle image content
					}
				}
			}

			// * combine text parts
			ollamaMsg.Content = strings.Join(textParts, "\n")

			// * add message to list
			ollamaMessages = append(ollamaMessages, ollamaMsg)
		}

		// * prepare options
		options := map[string]any{
			"num_predict": 256, // default token limit
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
			Tools:     nil,
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
				messageStart := map[string]interface{}{
					"type": "message_start",
					"message": map[string]interface{}{
						"id":            messageId,
						"type":          "message",
						"role":          "assistant",
						"model":         *body.Model,
						"content":       []map[string]interface{}{},
						"stop_reason":   nil,
						"stop_sequence": nil,
						"usage": map[string]interface{}{
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

				// * send content_block_start event
				contentBlockStart := map[string]interface{}{
					"type":  "content_block_start",
					"index": 0,
					"content_block": map[string]interface{}{
						"type": "text",
						"text": "",
					},
				}
				if contentBlockStartData, err := json.Marshal(contentBlockStart); err == nil {
					_, _ = fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", contentBlockStartData)
				}

				// * send initial ping event
				pingEvent := map[string]interface{}{
					"type": "ping",
				}
				if pingData, err := json.Marshal(pingEvent); err == nil {
					_, _ = fmt.Fprintf(w, "event: ping\ndata: %s\n\n", pingData)
				}

				// * call ollama with streaming
				var accumulatedResponse string
				outputTokens := 0
				err = client.Chat(context.Background(), request, func(resp api.ChatResponse) error {
					// * accumulate response
					accumulatedResponse += resp.Message.Content
					outputTokens += 1 // rough estimation

					// * create anthropic streaming response chunk
					chunk := map[string]interface{}{
						"type":  "content_block_delta",
						"index": 0,
						"delta": map[string]interface{}{
							"type": "text_delta",
							"text": resp.Message.Content,
						},
					}

					// * serialize and send chunk
					if chunkData, err := json.Marshal(chunk); err == nil {
						_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", chunkData)
					}

					return nil
				})

				if err != nil {
					// * handle error in stream
					errorChunk := map[string]interface{}{
						"type": "error",
						"error": map[string]interface{}{
							"message": "Failed to generate response from Ollama",
							"type":    "server_error",
						},
					}

					if errorData, err := json.Marshal(errorChunk); err == nil {
						_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", errorData)
					}
				}

				// * send content_block_stop
				contentBlockStop := map[string]interface{}{
					"type":  "content_block_stop",
					"index": 0,
				}
				if contentBlockStopData, err := json.Marshal(contentBlockStop); err == nil {
					_, _ = fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", contentBlockStopData)
				}

				// * send message_delta
				messageDelta := map[string]interface{}{
					"type": "message_delta",
					"delta": map[string]interface{}{
						"stop_reason":   "end_turn",
						"stop_sequence": nil,
					},
					"usage": map[string]interface{}{
						"output_tokens": outputTokens,
					},
				}
				if messageDeltaData, err := json.Marshal(messageDelta); err == nil {
					_, _ = fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", messageDeltaData)
				}

				// * send message_stop
				messageStop := map[string]interface{}{
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
		err = client.Chat(context.Background(), request, func(resp api.ChatResponse) error {
			output += resp.Message.Content
			return nil
		})

		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "Failed to generate response from Ollama",
				"details": err.Error(),
			})
		}

		// * create anthropic response format
		response := anthropic.Message{
			ID:         "msg_" + *gut.Random(gut.RandomSet.MixedAlphaNum, 24),
			Type:       "message",
			Role:       anthropic.MessageRoleAssistant,
			Content:    []anthropic.ContentBlock{{Type: anthropic.ContentBlockTypeText, Text: output}},
			Model:      *body.Model,
			StopReason: "end_turn",
			Usage: anthropic.Usage{
				InputTokens:  100, // estimate
				OutputTokens: 100, // estimate
			},
		}

		return c.JSON(response)
	})

	// * start the server
	log.Fatal(app.Listen(listen))
}
