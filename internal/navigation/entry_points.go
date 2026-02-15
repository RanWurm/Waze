package navigation

import (
	"math"
	"waze/internal/graph"
)

// City represents a geographic region with entry points
type City struct {
	Name        string
	CenterX     float64
	CenterY     float64
	Radius      float64        // km
	EntryPoints []int          // node IDs
	Nodes       map[int]bool   // all node IDs in this city

	ForwardVirtualNodeID  int // edges FROM each entry TO this node (weight 0)
	ReversedVirtualNodeID int // edges FROM this node TO each entry (weight 0)
}

// EntryPointManager handles city entry points
type EntryPointManager struct {
	Cities map[string]*City
	// Map from node ID to city name
	NodeToCity map[int]string
}

func NewEntryPointManager() *EntryPointManager {
	return &EntryPointManager{
		Cities:     make(map[string]*City),
		NodeToCity: make(map[int]string),
	}
}

// IdentifyEntryPoints finds entry points for a city based on major highways
func (epm *EntryPointManager) IdentifyEntryPoints(g *graph.Graph, cityName string, centerX, centerY, radius float64, numEntries int) {
	city := &City{
		Name:        cityName,
		CenterX:     centerX,
		CenterY:     centerY,
		Radius:      radius,
		EntryPoints: make([]int, 0, numEntries),
	}

	// Find all nodes in the city
	cityNodeSet := make(map[int]bool)
	cityNodes := make([]int, 0)
	for id, node := range g.Nodes {
		dist := haversine(centerX, centerY, node.X, node.Y)
		if dist <= radius {
			cityNodes = append(cityNodes, id)
			cityNodeSet[id] = true
			epm.NodeToCity[id] = cityName
		}
	}
	city.Nodes = cityNodeSet

	// Find boundary nodes (nodes at the edge with connections to outside)
	type BoundaryNode struct {
		NodeID           int
		ExternalDegree   int // number of edges going outside
		DistanceToCenter float64
	}

	boundaryNodes := make([]BoundaryNode, 0)

	for _, nodeID := range cityNodes {
		node := g.Nodes[nodeID]
		externalDegree := 0

		// Check outgoing edges
		for _, edge := range g.GetNeighbors(nodeID) {
			targetCity, exists := epm.NodeToCity[edge.To]
			if !exists || targetCity != cityName {
				externalDegree++
			}
		}

		// Check incoming edges
		if reverseEdges, exists := g.ReverseAdjList[nodeID]; exists {
			for _, edge := range reverseEdges {
				sourceCity, exists := epm.NodeToCity[edge.From]
				if !exists || sourceCity != cityName {
					externalDegree++
				}
			}
		}

		if externalDegree > 0 {
			distToCenter := haversine(centerX, centerY, node.X, node.Y)
			boundaryNodes = append(boundaryNodes, BoundaryNode{
				NodeID:           nodeID,
				ExternalDegree:   externalDegree,
				DistanceToCenter: distToCenter,
			})
		}
	}

	// Sort by external degree (descending) - nodes with more external connections are better entry points
	// Break ties by distance to center (closer is better for central entries, farther for distributed coverage)
	for i := 0; i < len(boundaryNodes); i++ {
		for j := i + 1; j < len(boundaryNodes); j++ {
			if boundaryNodes[j].ExternalDegree > boundaryNodes[i].ExternalDegree {
				boundaryNodes[i], boundaryNodes[j] = boundaryNodes[j], boundaryNodes[i]
			}
		}
	}

	// Select top numEntries with geographic distribution
	selected := make([]int, 0, numEntries)
	for _, bn := range boundaryNodes {
		if len(selected) >= numEntries {
			break
		}

		// Check if this node is far enough from already selected entries
		tooClose := false
		for _, selectedID := range selected {
			selectedNode := g.Nodes[selectedID]
			currentNode := g.Nodes[bn.NodeID]
			dist := haversine(selectedNode.X, selectedNode.Y, currentNode.X, currentNode.Y)
			if dist < radius*0.3 { // at least 30% of city radius apart
				tooClose = true
				break
			}
		}

		if !tooClose {
			selected = append(selected, bn.NodeID)
		}
	}

	city.EntryPoints = selected
	epm.Cities[cityName] = city
}

// GetCity returns the city a node belongs to
func (epm *EntryPointManager) GetCity(nodeID int) (string, bool) {
	city, exists := epm.NodeToCity[nodeID]
	return city, exists
}

// GetEntryPoints returns entry points for a city
func (epm *EntryPointManager) GetEntryPoints(cityName string) ([]int, bool) {
	city, exists := epm.Cities[cityName]
	if !exists {
		return nil, false
	}
	return city.EntryPoints, true
}

// haversine calculates distance between two lat/lon points in km
func haversine(lon1, lat1, lon2, lat2 float64) float64 {
	const R = 6371 // Earth radius in km

	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0

	lat1Rad := lat1 * math.Pi / 180.0
	lat2Rad := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(lat1Rad)*math.Cos(lat2Rad)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}
