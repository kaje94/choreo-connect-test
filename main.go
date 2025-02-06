package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var (
	clients     = make(map[string]*websocket.Conn) // Map to store WebSocket connections
	clientsMu   sync.Mutex                         // Mutex to protect the clients map
	responsesMu sync.Mutex                         // Mutex to protect the clients map

	responseChannels = make(map[string]chan ResponseDetails)

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all connections
		},
	}
)

type RequestDetails struct {
	RequestID string            `json:"request_id"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Query     map[string]string `json:"query"`
	Body      string            `json:"body"`
	Port      string            `json:"port"`
	Domain    string            `json:"domain"`
}

type ResponseDetails struct {
	RequestID  string            `json:"request_id"`
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// Handle WebSocket connections
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["component-name"]
	if componentName == "" {
		http.Error(w, "component-name is required", http.StatusBadRequest)
		return
	}

	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade to WebSocket:", err)
		return
	}
	defer conn.Close()

	// Register the client
	clientsMu.Lock()
	clients[componentName] = conn
	clientsMu.Unlock()

	log.Printf("Client connected: %s\n", componentName)

	// Handle incoming messages from the client
	for {
		var response ResponseDetails
		err := conn.ReadJSON(&response)
		if err != nil {
			log.Printf("Client %s disconnected: %v\n", componentName, err)
			clientsMu.Lock()
			delete(clients, componentName)
			clientsMu.Unlock()
			return
		}

		log.Printf("Received response from client %s: %+v\n", componentName, response)

		// Forward the response to the appropriate HTTP request handler
		clientsMu.Lock()
		if responseChan, ok := responseChannels[response.RequestID]; ok {
			responseChan <- response
		}
		clientsMu.Unlock()
	}
}

// Handle incoming HTTP requests
func handleRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["component-name"]
	subPath := vars["path"]

	if componentName == "" {
		http.Error(w, "component-name is required", http.StatusBadRequest)
		return
	}

	clientsMu.Lock()
	conn, ok := clients[componentName]
	clientsMu.Unlock()

	if !ok {
		http.Error(w, "component not found", http.StatusNotFound)
		return
	}

	// Generate a unique request ID
	requestID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Create a response channel for this request
	responseChan := make(chan ResponseDetails)
	responsesMu.Lock()
	responseChannels[requestID] = responseChan
	responsesMu.Unlock()

	defer func() {
		responsesMu.Lock()
		delete(responseChannels, requestID)
		responsesMu.Unlock()
	}()

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	// Convert headers and query parameters to maps
	headers := make(map[string]string)
	for key, values := range r.Header {
		headers[key] = values[0]
	}

	query := make(map[string]string)
	for key, values := range r.URL.Query() {
		query[key] = values[0]
	}

	// Reconstruct the full path including sub-routes
	fullPath := "/" + subPath
	if r.URL.RawQuery != "" {
		fullPath += "?" + r.URL.RawQuery
	}

	// Create a RequestDetails struct
	reqDetails := RequestDetails{
		RequestID: requestID,
		Method:    r.Method,
		Path:      fullPath,
		Headers:   headers,
		Query:     query,
		Body:      string(body),
	}

	// Send the request details to the client via WebSocket
	err = conn.WriteJSON(reqDetails)
	if err != nil {
		log.Println("Failed to send request to client:", err)
		http.Error(w, "failed to forward request to client", http.StatusInternalServerError)
		return
	}

	// Wait for the client's response with a timeout
	select {
	case response := <-responseChan:

		// Set the headers from the response
		for key, value := range response.Headers {
			w.Header().Set(key, value)
		}

		// Set the HTTP status code from the response
		w.WriteHeader(response.StatusCode)

		// Write the response body
		w.Write([]byte(response.Body))
	case <-time.After(30 * time.Second):
		http.Error(w, "client did not respond in time", http.StatusGatewayTimeout)
	}
}

func handleRequestFromClient(w http.ResponseWriter, r *http.Request) {
	// Parse the request body into RequestDetails
	var reqDetails RequestDetails
	if err := json.NewDecoder(r.Body).Decode(&reqDetails); err != nil {
		// Respond with an error in ResponseDetails format
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	// Construct the URL with or without the port
	var urlStr string
	if reqDetails.Port != "" {
		// Include the port in the URL (e.g., http://{component-name}:{port}/path)
		urlStr = fmt.Sprintf("http://%s:%s%s", reqDetails.Domain, reqDetails.Port, reqDetails.Path)
	} else {
		// Default URL without the port (e.g., http://{component-name}/path)
		urlStr = fmt.Sprintf("http://%s%s", reqDetails.Domain, reqDetails.Path)
	}
	// todo: uncomment if you want to test
	// if componentName == "node-webapp" {
	// 	urlStr = fmt.Sprintf("http://localhost:%s%s", "3000", reqDetails.Path)
	// }

	// Add query parameters to the URL
	if len(reqDetails.Query) > 0 {
		query := url.Values{}
		for key, value := range reqDetails.Query {
			query.Add(key, value)
		}
		urlStr += "?" + query.Encode()
	}

	// Create the HTTP request
	req, err := http.NewRequest(reqDetails.Method, urlStr, bytes.NewBuffer([]byte(reqDetails.Body)))
	if err != nil {
		// Respond with an error in ResponseDetails format
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create request: %v", err))
		return
	}

	// Add headers to the request
	for key, value := range reqDetails.Headers {
		req.Header.Add(key, value)
	}

	// Make the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		// Respond with an error in ResponseDetails format
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to make request: %v", err))
		return
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		// Respond with an error in ResponseDetails format
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read response body: %v", err))
		return
	}

	// Prepare the response details
	responseDetails := ResponseDetails{
		StatusCode: resp.StatusCode,
		Headers:    make(map[string]string),
		Body:       string(respBody),
	}

	// Copy response headers
	for key, values := range resp.Header {
		responseDetails.Headers[key] = values[0] // Use the first value for each header
	}

	// Send the response back to the caller
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(responseDetails); err != nil {
		// Respond with an error in ResponseDetails format
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to encode response: %v", err))
	}
}

// Helper function to respond with an error in ResponseDetails format
func respondWithError(w http.ResponseWriter, statusCode int, message string) {
	responseDetails := ResponseDetails{
		StatusCode: statusCode,
		Headers:    make(map[string]string),
		Body:       message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(responseDetails); err != nil {
		log.Printf("Failed to encode error response: %v", err)
	}
}

func main() {
	r := mux.NewRouter()

	// WebSocket endpoint
	r.HandleFunc("/ws/{component-name}", handleWebSocket)

	// HTTP request endpoint
	r.HandleFunc("/health", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	r.HandleFunc("/request/{component-name}", handleRequest)
	r.HandleFunc("/request/{component-name}/{path:.*}", handleRequest)
	r.HandleFunc("/handle", handleRequestFromClient).Methods("POST")

	// Start the server
	server := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	log.Println("Server started on :8080")
	log.Fatal(server.ListenAndServe())
}
