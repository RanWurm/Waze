package graph

import (
	"encoding/json"
	"fmt"
	"os"
)

type container struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

func LoadGraph(fileName string) (*Graph, error) {
	fileData, err := os.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("Failed to read file %w", err)
	}
	var container container

	if err := json.Unmarshal(fileData, &container); err != nil {
		return nil, fmt.Errorf("Failed to parse JSON: %w", err)
	}

	g := NewGraph()

	// add all nodes to the graph
	for i := range container.Nodes {
		g.AddNode(&container.Nodes[i])
	}

	// add all edges to the graph
	for i := range container.Edges {
		e := &container.Edges[i]

		// init current speed to the speed limit
		e.SetCurrentSpeed(e.SpeedLimit)

		// add the edge to the graph
		if err := g.AddEdge(e); err != nil {
			fmt.Printf("Warning: Skipping edge %d: %v\n", e.Id, err)
		}
	}

	// Build dense node index for array-based algorithms
	g.NodeIndex = make(map[int]int, len(g.Nodes))
	g.IndexNode = make([]int, len(g.Nodes))
	idx := 0
	for _, nodeID := range g.NodesArr {
		g.NodeIndex[nodeID] = idx
		g.IndexNode[idx] = nodeID
		idx++
	}

	// Auto-compute default delta: average edge weight (time in hours)
	totalWeight := 0.0
	edgeCount := 0
	for _, e := range g.Edges {
		speed := e.SpeedLimit
		if speed <= 0 {
			speed = 1.0
		}
		totalWeight += e.Length / speed
		edgeCount++
	}
	if edgeCount > 0 {
		g.DefaultDelta = totalWeight / float64(edgeCount)
	} else {
		g.DefaultDelta = 0.003
	}
	fmt.Printf("Graph loaded: %d nodes, %d edges, delta=%.6f hours\n", len(g.Nodes), len(g.Edges), g.DefaultDelta)

	return g, nil
}
