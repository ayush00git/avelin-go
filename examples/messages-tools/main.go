// Command messages-tools runs a tool-use round trip on the Anthropic-style
// messages endpoint: the model asks for get_weather, the program runs it and
// sends a tool_result back, and the model answers.
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
	req := avelin.MessageRequest{
		Model:     avelin.ModelAgenticPro,
		MaxTokens: 1024,
		Messages:  []avelin.MessageParam{{Role: "user", Content: "What's the weather in Abu Dhabi?"}},
		Tools: []avelin.MessageTool{{Name: "get_weather", Description: "Get current weather for a city",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)}},
	}
	for range 5 {
		msg, err := client.CreateMessage(context.Background(), req)
		if err != nil {
			log.Fatal(err)
		}
		if msg.StopReason != "tool_use" {
			fmt.Println(msg.Text())
			return
		}
		req.Messages = append(req.Messages, avelin.MessageParam{Role: "assistant", Blocks: msg.Content})
		var results []avelin.ContentBlock
		for _, block := range msg.Content {
			if block.Type != "tool_use" {
				continue
			}
			var args struct{ City string }
			if err := json.Unmarshal(block.Input, &args); err != nil {
				log.Fatal(err)
			}
			fmt.Printf("model called %s(%s)\n", block.Name, block.Input)
			results = append(results, avelin.ContentBlock{Type: "tool_result", ToolUseID: block.ID, Content: getWeather(args.City)})
		}
		req.Messages = append(req.Messages, avelin.MessageParam{Role: "user", Blocks: results})
	}
}
