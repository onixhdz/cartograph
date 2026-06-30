package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/onixhdz/cartograph"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	target := "."
	if len(os.Args) > 1 {
		target = os.Args[1]
	}

	client, err := cartograph.Open(cartograph.Config{})
	if err != nil {
		if errors.Is(err, cartograph.ErrDataDirInUse) {
			return fmt.Errorf("cartograph service already owns the data directory: %w", err)
		}
		return fmt.Errorf("open cartograph: %w", err)
	}
	defer client.Close()

	analyzed, err := client.Analyze(ctx, target, cartograph.AnalyzeOptions{})
	if err != nil {
		return fmt.Errorf("analyze %s: %w", target, err)
	}
	fmt.Printf("indexed %s (%s): %d nodes, %d edges\n", analyzed.RepoName, analyzed.RepoHash, analyzed.NodeCount, analyzed.EdgeCount)

	schema, err := client.Schema(ctx, analyzed.RepoHash)
	if err != nil {
		return fmt.Errorf("schema %s: %w", analyzed.RepoHash, err)
	}
	fmt.Printf("schema: %d labels, %d relationship types\n", len(schema.NodeLabels), len(schema.RelTypes))

	matches, err := client.Search(ctx, analyzed.RepoHash, "func", cartograph.SearchOptions{
		FixedStrings: true,
		Limit:        5,
	})
	if err != nil {
		return fmt.Errorf("search %s: %w", analyzed.RepoHash, err)
	}
	fmt.Printf("search: %d matches in %d files\n", matches.MatchCount, matches.FileCount)

	for _, match := range matches.Matches {
		fmt.Printf("%s:%d: %s\n", match.FilePath, match.Line, match.LineText)
	}
	return nil
}
