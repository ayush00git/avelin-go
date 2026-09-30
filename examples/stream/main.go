// Command stream streams a chat completion, printing reasoning to stderr and
// the answer to stdout as they arrive.
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
	stream, err := client.CreateChatCompletionStream(context.Background(), avelin.ChatCompletionRequest{
		Model:    "avelin-pro",
		Messages: []avelin.ChatMessage{{Role: "user", Content: "Write a haiku about sovereignty."}},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	for stream.Next() {
		for _, choice := range stream.Current().Choices {
			fmt.Fprint(os.Stderr, choice.Delta.ReasoningContent)
			fmt.Print(choice.Delta.Content)
		}
	}
	fmt.Println()
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
}
