package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Client עם mutex לכתיבה בטוחה
type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
	send chan []byte
}

func (c *Client) WriteMessage(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// מנהל חיבורי WebSocket
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

var GlobalHub *Hub

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("Client connected. Total: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.conn.Close()
			}
			h.mu.Unlock()
			log.Printf("Client disconnected. Total: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				if err := client.WriteMessage(message); err != nil {
					client.conn.Close()
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// הודעה שנשלחת ל-GUI
type GUIUpdate struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type CarPosition struct {
	CarID    int         `json:"car_id"`
	EdgeID   int         `json:"edge_id"`
	Progress float64     `json:"progress"`
	Speed    float64     `json:"speed"`
	X        float64     `json:"x"`
	Y        float64     `json:"y"`
	Route    [][]float64 `json:"route,omitempty"` // [[lng,lat], ...] for GUI
}

// שליחת עדכון לכל הלקוחות
func (h *Hub) BroadcastUpdate(updateType string, data interface{}) {
	update := GUIUpdate{
		Type: updateType,
		Data: data,
	}
	jsonData, err := json.Marshal(update)
	if err != nil {
		log.Printf("Error marshaling update: %v", err)
		return
	}
	h.broadcast <- jsonData
}

// WebSocket endpoint handler
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}

	GlobalHub.register <- client

	// שליחת הודעת אתחול ללא הגרף (למנוע lag)
	// הלקוח יקבל רק את המסלולים שנבקש
	initMsg := GUIUpdate{
		Type: "init",
		Data: map[string]interface{}{
			"nodes": []NodeData{}, // Empty - no need to send entire graph
			"edges": []EdgeData{}, // Empty - no need to send entire graph
			"message": "Connected - request a route to begin",
		},
	}
	jsonData, _ := json.Marshal(initMsg)
	client.WriteMessage(jsonData)

	// האזנה להודעות מהלקוח (לסגירה)
	go func() {
		defer func() {
			GlobalHub.unregister <- client
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

// מבנה הגרף לשליחה ל-GUI
type GraphData struct {
	Nodes []NodeData `json:"nodes"`
	Edges []EdgeData `json:"edges"`
}

type NodeData struct {
	ID int     `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

type EdgeData struct {
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

func (s *Server) GetGraphData() GraphData {
	nodes := make([]NodeData, 0, len(s.Graph.Nodes))
	edges := make([]EdgeData, 0, len(s.Graph.Edges))

	for _, node := range s.Graph.Nodes {
		nodes = append(nodes, NodeData{
			ID: node.Id,
			X:  node.X,
			Y:  node.Y,
		})
	}

	for _, edge := range s.Graph.Edges {
		fromNode := s.Graph.Nodes[edge.From]
		toNode := s.Graph.Nodes[edge.To]
		edges = append(edges, EdgeData{
			ID:         edge.Id,
			From:       edge.From,
			To:         edge.To,
			FromX:      fromNode.X,
			FromY:      fromNode.Y,
			ToX:        toNode.X,
			ToY:        toNode.Y,
			Length:     edge.Length,
			SpeedLimit: edge.SpeedLimit,
		})
	}

	return GraphData{Nodes: nodes, Edges: edges}
}