// Command stream streams a chat completion, printing the answer as it arrives
// and collecting the whole completion with Accumulate.
//
// It reads AVELIN_API_KEY, and AVELIN_BASE_URL if set.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ayush00git/avelin-go"
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
	var completion avelin.ChatCompletion
	for stream.Next() {
		chunk := stream.Current()
		if err := completion.Accumulate(chunk); err != nil {
			log.Fatal(err)
		}
		for _, choice := range chunk.Choices {
			fmt.Print(choice.Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
	choice := completion.Choices[0]
	fmt.Printf("\n(finish: %s, %d characters of reasoning)\n", choice.FinishReason, len(choice.Message.ReasoningContent))
}
