package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"time"
	"waze/internal/config"
	"waze/internal/types"
)

type Client struct {
	BaseURL string
	Http    *http.Client
}

func NewClient(url string) *Client {
	return &Client{
		BaseURL: url,
		Http: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

const minReportsPerRequest = 500

// send all traffic report from al cars to server
func (c *Client) SendTrafficBatch(reports []types.TrafficReport) error {
	jsonData, _ := json.Marshal(reports)
	resp, err := c.Http.Post(c.BaseURL+"/api/traffic", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	// close the connetion after the function ends
	defer resp.Body.Close()

	// check connection status
	if resp.StatusCode != 200 {
		return fmt.Errorf("Server returned status:  %d", resp.StatusCode)
	}
	return nil
}

// Request and return route from server
func (c *Client) RequestRoute(startNode, endNode int) ([]int, error) {

	url := fmt.Sprintf("%s/api/navigate?from=%d&to=%d", c.BaseURL, startNode, endNode)
	// fmt.Println(url)
	// time.Sleep(time.Second * 5)

	// send route request
	resp, err := c.Http.Get(url)
	if err != nil {
		return nil, err
	}
	// close the connection after function ends
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("navigation failed, status: %d", resp.StatusCode)
	}

	var result types.NavigationResponse

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.RouteNodes, nil
}

func simulateCarDataFetch(id int) types.TrafficReport {
	return types.TrafficReport{
		CarID:  id,
		EdgeID: id % 1000,
		Speed:  float64(id%100) + 5.5,
	}
}

func (c *Client) MeasureTotalCycle(numCars int, mode string) time.Duration {
	start := time.Now()

	cpuLimit := config.Global.MaxCPUs
	if cpuLimit <= 0 {
		cpuLimit = runtime.NumCPU()
	}

	if mode == "single" {
		hugeReport := make([]types.TrafficReport, numCars)

		var wgGen sync.WaitGroup
		chunkSize := (numCars + cpuLimit - 1) / cpuLimit

		for i := 0; i < cpuLimit; i++ {
			startIdx := i * chunkSize
			endIdx := min(startIdx+chunkSize, numCars)
			if startIdx >= endIdx {
				continue
			}

			wgGen.Add(1)
			go func(s, e int) {
				defer wgGen.Done()
				for j := s; j < e; j++ {
					hugeReport[j] = simulateCarDataFetch(j)
				}
			}(startIdx, endIdx)
		}
		wgGen.Wait()

		if err := c.SendTrafficBatch(hugeReport); err != nil {
			fmt.Println("Error single:", err)
		}

	} else if mode == "parallel" {

		neededWorkers := numCars / minReportsPerRequest
		numWorkers := min(cpuLimit, neededWorkers)
		if numWorkers < 1 {
			numWorkers = 1
		}

		chunkSize := (numCars + numWorkers - 1) / numWorkers
		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			startIdx := i * chunkSize
			endIdx := min(startIdx+chunkSize, numCars)
			if startIdx >= endIdx {
				break
			}

			wg.Add(1)
			go func(s, e int) {
				defer wg.Done()

				localData := make([]types.TrafficReport, e-s)

				for j := 0; j < len(localData); j++ {
					localData[j] = simulateCarDataFetch(s + j)
				}

				c.SendTrafficBatch(localData)
			}(startIdx, endIdx)
		}
		wg.Wait()
	}

	return time.Since(start)
}

func (c *Client) MeasurePerformance(reports []types.TrafficReport, mode string) time.Duration {
	start := time.Now()

	if mode == "single" {
		err := c.SendTrafficBatch(reports)
		if err != nil {
			fmt.Println("Error in single batch: ", err)
		}
	} else if mode == "parallel" {
		neededWorkers := len(reports) / minReportsPerRequest
		numWorkers := max(min(config.Global.MaxCPUs, neededWorkers), 1)

		chunkSize := (len(reports) + numWorkers - 1) / numWorkers

		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			startIdx := i * chunkSize
			endIdx := min(startIdx+chunkSize, len(reports))
			if startIdx >= endIdx {
				break
			}

			chunk := reports[startIdx:endIdx]

			wg.Add(1)
			go func(data []types.TrafficReport) {
				defer wg.Done()
				c.SendTrafficBatch(data)
			}(chunk)
		}
		wg.Wait()
	}
	return time.Since(start)
}
