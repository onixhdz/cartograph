package ingestion

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"
)

func benchPlantedPartition(b *testing.B, nNodes, nGroups int) {
	adj := make(AdjacencyList, nNodes)
	groupSize := nNodes / nGroups
	for i := range nNodes {
		adj[strconv.Itoa(i)] = make(map[string]float64)
	}
	for g := range nGroups {
		start := g * groupSize
		end := start + groupSize
		for i := start; i < end; i++ {
			for j := i + 1; j < end; j++ {
				a := strconv.Itoa(i)
				bStr := strconv.Itoa(j)
				adj[a][bStr] = 1.0
				adj[bStr][a] = 1.0
			}
		}
	}
	for g := range nGroups - 1 {
		a := strconv.Itoa(g * groupSize)
		bStr := strconv.Itoa((g + 1) * groupSize)
		adj[a][bStr] = 0.1
		adj[bStr][a] = 0.1
	}
	b.ResetTimer()
	for b.Loop() {
		Leiden(adj)
	}
}

// Sparse: each node connects to intraEdgesPerNode random group peers
func benchSparsePartition(b *testing.B, nNodes, nGroups, intraEdgesPerNode int) {
	rng := rand.New(rand.NewSource(42))
	adj := make(AdjacencyList, nNodes)
	groupSize := nNodes / nGroups
	for i := range nNodes {
		adj[strconv.Itoa(i)] = make(map[string]float64)
	}
	for g := range nGroups {
		start := g * groupSize
		for i := start; i < start+groupSize; i++ {
			a := strconv.Itoa(i)
			for range intraEdgesPerNode {
				j := start + rng.Intn(groupSize)
				if j == i {
					continue
				}
				bStr := strconv.Itoa(j)
				adj[a][bStr] = 1.0
				adj[bStr][a] = 1.0
			}
		}
	}
	for g := range nGroups - 1 {
		a := strconv.Itoa(g * groupSize)
		bStr := strconv.Itoa((g + 1) * groupSize)
		adj[a][bStr] = 0.1
		adj[bStr][a] = 0.1
	}
	b.ResetTimer()
	for b.Loop() {
		Leiden(adj)
	}
}

func BenchmarkLeiden_1000_dense(b *testing.B)  { benchPlantedPartition(b, 1000, 20) }
func BenchmarkLeiden_2000_dense(b *testing.B)  { benchPlantedPartition(b, 2000, 20) }
func BenchmarkLeiden_5000_dense(b *testing.B)  { benchPlantedPartition(b, 5000, 50) }
func BenchmarkLeiden_10000_dense(b *testing.B) { benchPlantedPartition(b, 10000, 50) }

func BenchmarkLeiden_1000_sparse10(b *testing.B)  { benchSparsePartition(b, 1000, 20, 10) }
func BenchmarkLeiden_2000_sparse10(b *testing.B)  { benchSparsePartition(b, 2000, 20, 10) }
func BenchmarkLeiden_5000_sparse10(b *testing.B)  { benchSparsePartition(b, 5000, 50, 10) }
func BenchmarkLeiden_10000_sparse10(b *testing.B) { benchSparsePartition(b, 10000, 50, 10) }
func BenchmarkLeiden_10000_sparse20(b *testing.B) { benchSparsePartition(b, 10000, 50, 20) }

func init() { _ = fmt.Sprintf }
