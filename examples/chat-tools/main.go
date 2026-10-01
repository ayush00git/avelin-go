// Command chat-tools runs a tool-calling round trip on the chat completions
// endpoint: the model asks for get_weather, the program runs it and sends the
// result back, and the model answers.
//
// It reads AVELIN_API_KEY, and AVELIN_BASE_URL if set.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/ayush00git/avelin-go"
)

func getWeather(city string) string {
	return fmt.Sprintf(`{"city": %q, "temp_c": 31, "sky": "sunny"}`, city)
}

func main() {
	client := avelin.NewClient()
	req := avelin.ChatCompletionRequest{
		Model:    avelin.ModelAgenticPro,
		Messages: []avelin.ChatMessage{{Role: "user", Content: "What's the weather in Abu Dhabi?"}},
		Tools: []avelin.Tool{{Type: "function", Function: avelin.FunctionDefinition{
			Name: "get_weather", Description: "Get current weather for a city",
			Parameters: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		}}},
	}
	for range 5 {
		resp, err := client.CreateChatCompletion(context.Background(), req)
		if err != nil {
			log.Fatal(err)
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			fmt.Println(msg.Content)
			return
		}
		req.Messages = append(req.Messages, msg)
		for _, call := range msg.ToolCalls {
			var args struct{ City string }
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("model called %s(%s)\n", call.Function.Name, call.Function.Arguments)
			req.Messages = append(req.Messages, avelin.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: getWeather(args.City)})
		}
	}
}
