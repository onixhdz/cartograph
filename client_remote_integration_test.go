package cartograph

import (
	"context"
	"testing"
	"time"
)

func TestEmbeddedClientAnalyzeRemoteIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("remote clone integration test")
	}
	client, err := Open(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := client.Analyze(ctx, "go-git/go-billy@v5.6.2", AnalyzeOptions{})
	if err != nil {
		t.Fatalf("Analyze remote shorthand: %v", err)
	}
	if result.RepoHash == "" || result.NodeCount == 0 || result.Commit == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	status, err := client.Status(ctx, result.RepoHash)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Indexed || status.NodeCount == 0 {
		t.Fatalf("unexpected status: %+v", status)
	}
}
