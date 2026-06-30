package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	cartograph "github.com/onixhdz/cartograph"
)

const envTrue = "true"

func main() {
	ctx := context.Background()
	client, err := cartograph.Open(cartograph.Config{DataDir: os.Getenv("CARTOGRAPH_DATA_DIR")})
	if err != nil {
		fatal(err)
	}
	defer func() { _ = client.Close() }()

	cfg := map[string]string{}
	if v := strings.TrimSpace(os.Getenv("CAPEC_STIX_URL")); v != "" {
		cfg["stix_url"] = v
	}
	if os.Getenv("CAPEC_INCLUDE_DEPRECATED") == envTrue {
		cfg["include_deprecated"] = "true"
	}

	status, err := client.RegisterPlugin(ctx, &capecPlugin{}, cartograph.RegisterPluginOptions{
		Config:  cfg,
		Timeout: 10 * time.Minute,
	})
	if err != nil {
		fatal(err)
	}
	fmt.Printf("registered %s (%s): repo=%s hash=%s nodes=%d edges=%d resources=%d\n",
		status.PluginName, status.PluginVersion, status.Repo, status.RepoHash, status.NodeCount, status.EdgeCount, status.ResourceCount)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
