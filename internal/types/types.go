package types

// format of sending a traffic report
type TrafficReport struct {
	CarID        int     `json:"car_id"`
	EdgeID       int     `json:"edge_id"`
	Speed        float64 `json:"speed"`
	Timestamp    int64   `json:"timestamp"`
	EdgeProgress float64 `json:"edge_progress"`            // 0-1 fraction along current edge
	RouteEdges   []int   `json:"route_edges,omitempty"`    // full route for sampled cars
}

// format of asking a navigation request
type NavigationRequest struct {
	FromNodeId int `json:"from_node"`
	ToNodeId   int `json:"toNode"`
}

// format of recieving a navigation request answer
type NavigationResponse struct {
	RouteNodes []int       `json:"route"`
	RouteEdges []EdgeInfo  `json:"route_edges"` // Full edge data for frontend
	ETA        float64     `json:"eta"`
	Distance   float64     `json:"distance"`
	Err        error       `json:"error"`
}

// Edge information for frontend rendering
type EdgeInfo struct {
	ID         int     `json:"id"`
	From       int     `json:"from"`
	To         int     `json:"to"`
	FromX      float64 `json:"from_x"`
	FromY      float64 `json:"from_y"`
	ToX        float64 `json:"to_x"`
	ToY        float64 `json:"to_y"`
	Length     float64 `json:"length"`
	SpeedLimit float64 `json:"speed_limit"`
}
