package navigation

// import "time"

type PathResult struct {
	Route      []int   // Edge IDs (primary format)
	RouteNodes []int   // Node IDs (alternative format for entry-point routing)
	Distance   float64 // in KM
	ETA        float64 // in minutes
}

// Route    []int   `json:"route"`
// Distance float64 `json:"total_distance"`
// ETA      float64 `json:"computation_cost"`
// TotalTime time.Duration `json:"total_time"`
