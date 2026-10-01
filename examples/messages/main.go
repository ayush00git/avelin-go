// Command messages calls the Anthropic-compatible messages endpoint and
// prints the model's thinking to stderr and its answer to stdout.
//
// It reads AVELIN_API_KEY, and AVELIN_BASE_URL if set.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ayush00git/avelin-go"
)

func main() {
	client := avelin.NewClient()
	msg, err := client.CreateMessage(context.Background(), avelin.MessageRequest{
		Model:     avelin.ModelCodingFast,
		MaxTokens: 1024,
		System:    "You are a senior software engineer.",
		Messages:  []avelin.MessageParam{{Role: "user", Content: "Write a Go function to debounce calls."}},
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, block := range msg.Content {
		if block.Type == "thinking" {
			fmt.Fprintln(os.Stderr, "thinking:", block.Thinking)
		}
	}
	fmt.Println(msg.Text())
	fmt.Printf("(%s, stop: %s, %d tokens)\n", msg.Model, msg.StopReason, msg.Usage.TotalTokens)
}
