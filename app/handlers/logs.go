package handlers

import (
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// LogBuffer is a thread-safe circular buffer for log messages
type LogBuffer struct {
	mu       sync.RWMutex
	messages []string
	maxSize  int
}

// Global log buffer
var logBuffer = &LogBuffer{
	messages: make([]string, 0, 1000),
	maxSize:  1000,
}

// LogWriter captures stdout and writes to both stdout and buffer
type LogWriter struct {
	output io.Writer
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	// Write to original output
	n, err = w.output.Write(p)
	
	// Add to buffer
	message := string(p)
	logBuffer.Add(message)
	
	// Broadcast to all connected WebSocket clients
	broadcastLog(message)
	
	return n, err
}

// Add adds a message to the buffer
func (lb *LogBuffer) Add(message string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	
	// Split by newlines to handle multi-line logs
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")
	for _, line := range lines {
		if line != "" {
			if len(lb.messages) >= lb.maxSize {
				// Remove oldest message
				lb.messages = lb.messages[1:]
			}
			lb.messages = append(lb.messages, line)
		}
	}
}

// GetAll returns all messages in the buffer
func (lb *LogBuffer) GetAll() []string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	
	result := make([]string, len(lb.messages))
	copy(result, lb.messages)
	return result
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for simplicity
	},
}

// WebSocket clients
var (
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.RWMutex
)

// broadcastLog sends a log message to all connected WebSocket clients
func broadcastLog(message string) {
	clientsMu.RLock()
	clientsCopy := make([]*websocket.Conn, 0, len(clients))
	for client := range clients {
		clientsCopy = append(clientsCopy, client)
	}
	clientsMu.RUnlock()
	
	var toRemove []*websocket.Conn
	for _, client := range clientsCopy {
		err := client.WriteMessage(websocket.TextMessage, []byte(message))
		if err != nil {
			client.Close()
			toRemove = append(toRemove, client)
		}
	}
	
	// Remove disconnected clients
	if len(toRemove) > 0 {
		clientsMu.Lock()
		for _, client := range toRemove {
			delete(clients, client)
		}
		clientsMu.Unlock()
	}
}

// LogsHandler serves the logs page
func LogsHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/layout.html", "templates/logs.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// LogsWebSocketHandler handles WebSocket connections for live logs
func LogsWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Register client
	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	// Send all existing logs
	logs := logBuffer.GetAll()
	for _, logMsg := range logs {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(logMsg+"\n")); err != nil {
			break
		}
	}

	// Keep connection alive and handle cleanup
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			clientsMu.Lock()
			delete(clients, conn)
			clientsMu.Unlock()
			break
		}
	}
}

// InitLogCapture sets up log capturing
func InitLogCapture() {
	// Create a multi-writer that writes to both stdout and our buffer
	multiWriter := &LogWriter{output: log.Writer()}
	
	// Create a new logger that uses our multi-writer
	logger := log.New(multiWriter, "", log.LstdFlags)
	
	// Set as default logger
	log.SetOutput(logger.Writer())
	log.SetFlags(log.LstdFlags)
	
	// Note: We keep the original stdout as is, but logs via log package will be captured
}