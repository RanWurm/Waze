package sim

import (
	"fmt"
	"sync"
	"waze/internal/config"
	"waze/internal/graph"
)

type CarState int

const (
	Idle CarState = iota
	Waiting
	Driving
	Arrived
)

const CONVERT_TO_HOURS float64 = 3600.0

type TravelRoute struct {
	RouteEdges       []int
	CurrentEdgeIndex int
	CurrentEdgeLen   float64
	EdgeProgress     float64
}

type Car struct {
	Id           int
	UserId       int // optional
	State        CarState
	CurrentSpeed float64
	ActiveRoute  *TravelRoute

	Mu sync.RWMutex

	LastReportedEdgeID int
	LastReportedSpeed  float64
	LastReportTime     float64

	LastRouteReq float64    // time of the last route request
	NewRouteChan chan []int // channel for a new route
}

func NewCar(id, userId int, currentTime float64) *Car {
	return &Car{
		Id:           id,
		UserId:       userId,
		State:        Idle,
		CurrentSpeed: 0,
		LastRouteReq: currentTime,
		NewRouteChan: make(chan []int, 1),
	}
}

func (car *Car) InitRoute(routeEdges []int, g *graph.Graph) {
	// route is empty
	if len(routeEdges) == 0 {
		return
	}

	firstEdgeId := routeEdges[0]
	firstEdgeLen := 0.0
	initialSpeed := 0.0
	// check if the edge exists
	if edge, exists := g.Edges[firstEdgeId]; exists {
		firstEdgeLen = edge.Length
		// set initial speed for the car
		if edge.GetCurrentSpeed() > 0 {
			initialSpeed = edge.GetCurrentSpeed()
		} else {
			initialSpeed = float64(edge.SpeedLimit)
		}
	}

	// init active route
	car.ActiveRoute = &TravelRoute{
		RouteEdges:       routeEdges,
		CurrentEdgeIndex: 0,
		EdgeProgress:     0,
		CurrentEdgeLen:   firstEdgeLen,
	}

	car.CurrentSpeed = initialSpeed
	// set state to driving
	car.State = Driving
}

func (car *Car) Move(deltaTime float64, g *graph.Graph, densityMap map[int]int) {

	car.Mu.Lock()
	defer car.Mu.Unlock()

	// the car is not in driving state. return from the function
	if car.State != Driving || car.ActiveRoute == nil {
		return
	}

	car.calculatePhysics(g, densityMap)

	// convert to hour because of calculation
	hoursNum := deltaTime / CONVERT_TO_HOURS

	// calculate distanceCovered covered in delta time
	distanceCovered := car.CurrentSpeed * hoursNum

	// move current progress by distance
	car.ActiveRoute.EdgeProgress += distanceCovered

	if car.ActiveRoute.EdgeProgress >= car.ActiveRoute.CurrentEdgeLen {
		car.switchToNextEdge(g)
	}
	car.LastRouteReq += deltaTime

}

func (car *Car) calculatePhysics(g *graph.Graph, densityMap map[int]int) {
	// get current length id and check for existance
	currentEdgeId := car.ActiveRoute.RouteEdges[car.ActiveRoute.CurrentEdgeIndex]
	edge, exists := g.Edges[currentEdgeId]
	if !exists {
		return
	}

	lengthKm := edge.Length
	carLen := config.Global.Physics.CarLengthKm

	// calculate capacity of cars on the edge/road
	carCapacity := lengthKm / carLen
	if carCapacity < 1 {
		carCapacity = 1
	}

	// calculate the current edgeDensity on the edge
	edgeDensity := float64(densityMap[currentEdgeId]) / carCapacity
	if edgeDensity > 1.0 {
		edgeDensity = 1.0
	}

	// lower the speed when there is a lot of density
	speedFactor := 1.0 - (edgeDensity * edgeDensity)

	// check the progress on the current edge - (more progress means lower speed because of bottleneck)
	progressPrecent := car.ActiveRoute.EdgeProgress / lengthKm
	if progressPrecent > config.Global.Physics.DensityThreshold && edgeDensity > config.Global.Physics.EdgeDensityThreshold {
		speedFactor *= config.Global.Physics.SpeedFactor
	}

	// update the current speed to be the speed limit in the edge multipkied by speed factor
	finalSpeed := edge.SpeedLimit * speedFactor
	if finalSpeed < 5 {
		finalSpeed = 5
	}
	car.CurrentSpeed = finalSpeed
}

func (car *Car) switchToNextEdge(g *graph.Graph) {
	// get how much long til the end of the edge.road
	reminder := car.ActiveRoute.CurrentEdgeLen - car.ActiveRoute.EdgeProgress

	// check if we finished the current edge/road
	if reminder <= 0 {
		// get to the next edge
		car.ActiveRoute.CurrentEdgeIndex++

		// check if we reached end of the route
		if car.ActiveRoute.CurrentEdgeIndex >= len(car.ActiveRoute.RouteEdges) {
			fmt.Printf("Car %d arrived (route had %d edges)\n", car.Id, len(car.ActiveRoute.RouteEdges))
			car.State = Arrived
			car.CurrentSpeed = 0
			car.ActiveRoute = nil
			return
		}

		// move to the next edge
		nextEdgeId := car.ActiveRoute.RouteEdges[car.ActiveRoute.CurrentEdgeIndex]

		// check if edge exists
		if nextEdge, exists := g.Edges[nextEdgeId]; exists {
			car.ActiveRoute.CurrentEdgeLen = nextEdge.Length
			// turn reminder to a positive number and add to the current progress in the node
			car.ActiveRoute.EdgeProgress = (-1) * reminder

			// update current speed
			speed := nextEdge.GetCurrentSpeed()
			if speed <= 0 {
				speed = float64(nextEdge.SpeedLimit)
			}
			car.CurrentSpeed = speed
		} else {
			fmt.Printf("Error: Route contained invalid edge ID %d\nCar id: %d\n", nextEdgeId, car.Id)
			car.State = Waiting
		}
	}
}
