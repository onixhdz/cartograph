package cartograph_test

import (
	"context"

	"github.com/onixhdz/cartograph"
)

func ExampleOpen() {
	client, err := cartograph.Open(cartograph.Config{})
	if err != nil {
		return
	}
	defer client.Close()

	_, _ = client.List(context.Background())
}

func ExampleClient_Analyze() {
	ctx := context.Background()
	client, err := cartograph.Open(cartograph.Config{})
	if err != nil {
		return
	}
	defer client.Close()

	result, err := client.Analyze(ctx, ".", cartograph.AnalyzeOptions{})
	if err != nil {
		return
	}

	_, _ = client.Schema(ctx, result.RepoHash)
}
