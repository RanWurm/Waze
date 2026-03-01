package graph

import (
	"fmt"
	"sort"
	"strings"
)

type Graph struct {
	Nodes    map[int]*Node
	Edges    map[int]*Edge
	NodesArr []int
	AdjList        map[int][]*Edge
	ReverseAdjList map[int][]*Edge

	// Dense index mapping for array-based algorithms (Delta-Stepping)
	NodeIndex map[int]int // node ID → dense index 0..N-1
	IndexNode []int       // dense index → node ID
	DefaultDelta float64  // auto-computed delta for Delta-Stepping
}

func NewGraph() *Graph {
	return &Graph{
		Nodes:          make(map[int]*Node),
		Edges:          make(map[int]*Edge),
		NodesArr:       make([]int, 0),
		AdjList:        make(map[int][]*Edge),
		ReverseAdjList: make(map[int][]*Edge),
	}
}

func (g *Graph) String() string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "=== Graph Summary ===\n")
	sb.WriteString(fmt.Sprintf("Nodes: %d | Edges: %d\n", len(g.Nodes), len(g.Edges)))
	sb.WriteString("-------------------\n")

	var keys []int
	for k := range g.Nodes {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	for _, id := range keys {
		node := g.Nodes[id]
		fmt.Fprintf(&sb, "[Node %d] %s (Lat: %.4f, Lon: %.4f)\n", node.Id, node.Name, node.X, node.Y)

		neighbors := g.AdjList[id]
		if len(neighbors) == 0 {
			sb.WriteString("    (No outgoing roads)\n")
		} else {
			for _, edge := range neighbors {
				fmt.Fprintf(&sb, "    --> To Node %d | Speed: %.0f km/h | Len: %.2f km\n",
					edge.To, edge.SpeedLimit, edge.Length)
			}
		}
	}

	return sb.String()
}

func (g *Graph) AddNode(n *Node) {
	g.Nodes[n.Id] = n
	g.NodesArr = append(g.NodesArr, n.Id)
}

func (g *Graph) AddEdge(e *Edge) error {
	if _, ok := g.Nodes[e.From]; !ok {
		return fmt.Errorf("Source node %d not found", e.From)
	}
	if _, ok := g.Nodes[e.To]; !ok {
		return fmt.Errorf("Destination node %d not found", e.To)
	}
	g.Edges[e.Id] = e
	g.AdjList[e.From] = append(g.AdjList[e.From], e)
	g.ReverseAdjList[e.To] = append(g.ReverseAdjList[e.To], e)

	return nil
}

func (g *Graph) GetNeighbors(nodeId int) []*Edge {
	return g.AdjList[nodeId]
}

func (g *Graph) AddVirtualNode(id int, name string, x, y float64) {
	g.Nodes[id] = &Node{Id: id, Name: name, X: x, Y: y}
}

func (g *Graph) AddVirtualEdge(id, from, to int) error {
	if _, ok := g.Nodes[from]; !ok {
		return fmt.Errorf("source node %d not found", from)
	}
	if _, ok := g.Nodes[to]; !ok {
		return fmt.Errorf("destination node %d not found", to)
	}
	e := &Edge{
		Id:         id,
		From:       from,
		To:         to,
		Length:     0,
		SpeedLimit: 1.0,
	}
	e.SetCurrentSpeed(1.0)
	g.Edges[id] = e
	g.AdjList[from] = append(g.AdjList[from], e)
	g.ReverseAdjList[to] = append(g.ReverseAdjList[to], e)
	return nil
}