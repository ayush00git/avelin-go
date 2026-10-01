// Command chat sends one chat completion request and prints the reply.
//
// It reads AVELIN_API_KEY, and AVELIN_BASE_URL if set (for example to point
// it at cmd/mockserver).
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/tncworks/avelin-go"
)

func main() {
	client := avelin.NewClient()
	resp, err := client.CreateChatCompletion(context.Background(), avelin.ChatCompletionRequest{
		Model: avelin.ModelPro,
		Messages: []avelin.ChatMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Give me three productivity tips."},
		},
		MaxTokens: 1024,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Choices[0].Message.Content)
	fmt.Printf("(%s, %d tokens)\n", resp.Model, resp.Usage.TotalTokens)
}
