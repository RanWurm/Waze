package config

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

type Config struct {
	Server struct {
		Port     string `json:"server_port"`
		MapFile  string `json:"map_file"`
		CacheTtl int    `json:"cache_ttl"`
	} `json:"server"`

	Simulation struct {
		ServerURL         string  `json:"server_url"`
		NumCars           int     `json:"num_cars"`
		SpawnRate         float64 `json:"spawn_rate"`
		ReportInterval    float64 `json:"report_interval"`
		EndSpawn          float64 `json:"end_spawn"`
		DeltaTime         float64 `json:"delta_time"`
		SpeedThreshold    float64 `json:"speed_threshold"`
		MaxTime           float64 `json:"max_time"`
		ReRouteInterval   float64 `json:"reroute_interval"`
		LookAheadDistance float64 `json:"look_ahead_distance"`
		MaxRouteRequest   float64 `json:"max_route_request"`
	} `json:"simulation"`

	Physics struct {
		CarLengthKm          float64 `json:"car_length_km"`
		DensityThreshold     float64 `json:"density_threshold"`
		EdgeDensityThreshold float64 `json:"edge_density"`
		SpeedFactor          float64 `json:"speed_factor"`
		Alpha                float64 `json:"alpha"`
	} `json:"physics"`

	MaxCPUs int `json:"max_cpus"`
}

var (
	Global Config
	once   sync.Once
)

func Load(filename string) error {
	var err error
	once.Do(func() {
		data, e := os.ReadFile(filename)
		if e != nil {
			err = e
			return
		}
		err = json.Unmarshal(data, &Global)
	})
	return err
}

func TimeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf("%s took %s", name, elapsed)
}
