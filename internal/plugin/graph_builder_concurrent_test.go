package plugin

import (
	"fmt"
	"sync"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

func TestLPGGraphBuilderConcurrentAddNode(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	const workers = 8
	const perWorker = 25
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := range perWorker {
				b.AddNode("Node", fmt.Sprintf("n:%d:%d", worker, i), nil)
			}
		}(w)
	}
	wg.Wait()

	if got, want := graph.NodeCount(g), workers*perWorker; got != want {
		t.Fatalf("nodes = %d, want %d", got, want)
	}
}
