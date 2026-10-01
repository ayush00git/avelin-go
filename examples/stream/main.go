// Command stream streams a chat completion and prints the answer as it
// arrives.
//
// It reads AVELIN_API_KEY, and AVELIN_BASE_URL if set.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/tncworks/avelin-go"
)

func main() {
	client := avelin.NewClient()
	stream, err := client.CreateChatCompletionStream(context.Background(), avelin.ChatCompletionRequest{
		Model:    avelin.ModelPro,
		Messages: []avelin.ChatMessage{{Role: "user", Content: "Write a haiku about sovereignty."}},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	for stream.Next() {
		for _, choice := range stream.Current().Choices {
			fmt.Print(choice.Delta.Content) // Delta.ReasoningContent holds the reasoning.
		}
	}
	fmt.Println()
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
}
