package query

import (
	"fmt"
	"testing"

	lpg "github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/service"
)

func TestDebugCypher(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddNode(g, graph.LabelFunction, map[string]any{
		graph.PropID:       "func:alice",
		graph.PropName:     "alice",
		"score":            1.0,
		graph.PropFilePath: "alice.go",
	})
	b := &Backend{Graph: g}
	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (f:Function) RETURN f.name, f.score",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for i, row := range result.Rows {
		for k, v := range row {
			fmt.Printf("row[%d][%q] = %v (type %T)\n", i, k, v, v)
		}
	}
	t.Log("columns:", result.Columns)
}
