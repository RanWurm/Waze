package sim

import (
	"encoding/json"
	"math/rand"
	"os"
	"waze/internal/graph"
)

type Route struct {
	Steps []int `json:"steps"`
}

type RouteFile struct {
	Routes []Route `json:"routes"`
}

type ArrRoutes struct {
	Routes [][]int `json:"steps"`
}

func GenerateFixedRouteJSON() ([]byte, error) {
	route := Route{
		Steps: []int{12, 7, 19, 4, 9},
	}

	return json.MarshalIndent(route, "", "  ")
}
func RandomRequest(g *graph.Graph, rng *rand.Rand) (int, int) {
	// not enough nodes in the graph
	if len(g.NodesArr) < 2 {
		return -1, -1
	}

	// sample two nodes, src and dst
	idx1 := rng.Intn(len(g.NodesArr))
	idx2 := rng.Intn(len(g.NodesArr))

	if idx1 == idx2 {
		idx2 = rng.Intn(len(g.NodesArr))
	}

	return g.NodesArr[idx1], g.NodesArr[idx2]
}

func GenarateRandomRoute(g *graph.Graph, steps int) []int {
	var currentMsgNode int
	for nodeId := range g.Nodes {
		if len(g.GetNeighbors(nodeId)) > 0 {
			currentMsgNode = nodeId
			break
		}
	}
	route := make([]int, 0)
	for i := 0; i < steps; i++ {
		neighbors := g.GetNeighbors(currentMsgNode)
		if len(neighbors) == 0 {
			break
		}

		randIdx := rand.Intn(len(neighbors))
		chosenEdge := neighbors[randIdx]

		route = append(route, chosenEdge.Id)
		currentMsgNode = chosenEdge.Id
	}

	return route
}

func GenerateRandomRoutes(g *graph.Graph, steps int, count int) RouteFile {
	routes := make([]Route, 0, count)

	for i := 0; i < count; i++ {
		r := GenarateRandomRoute(g, steps)
		routes = append(routes, Route{Steps: r})
	}

	return RouteFile{Routes: routes}
}

func SaveRoutesToFile(routes [][]int) error {
	data, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("Route.json", data, 0644)
}

func LoadRoutesFromFile() ([][]int, error) {
	data, err := os.ReadFile("Route.json")
	if err != nil {
		return nil, err
	}

	var routes [][]int
	err = json.Unmarshal(data, &routes)
	if err != nil {
		return nil, err
	}

	return routes, nil
}
