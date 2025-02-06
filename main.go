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
)

var (
	clients          = make(map[string]chan string)          // Map to store client channels
	responseChannels = make(map[string]chan ResponseDetails) // Map to store response channels
	clientsMu        sync.Mutex                              // Mutex to protect the clients map
	responsesMu      sync.Mutex                              // Mutex to protect the responseChannels map
)

type RequestDetails struct {
	RequestID string            `json:"request_id"`
	Method    string            `json:"method"`
	Domain    string            `json:"domain"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Query     map[string]string `json:"query"`
	Body      string            `json:"body"`
	Port      string            `json:"port"`
}

type ResponseDetails struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

func registerClientForSSE(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["component-name"]
	if componentName == "" {
		http.Error(w, "component-name is required", http.StatusBadRequest)
		return
	}

	clientsMu.Lock()
	if _, exists := clients[componentName]; exists {
		clientsMu.Unlock()
		http.Error(w, "component-name already registered", http.StatusConflict)
		return
	}

	clientChan := make(chan string)
	clients[componentName] = clientChan
	clientsMu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	fmt.Fprintf(w, "data: %s\n\n", `{"status":"registered"}`)
	w.(http.Flusher).Flush()

	for {
		select {
		case msg := <-clientChan:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			clientsMu.Lock()
			delete(clients, componentName)
			clientsMu.Unlock()
			return
		}
	}
}

func forwardRequestToClient(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["component-name"]
	subPath := vars["path"]

	if componentName == "" {
		http.Error(w, "component-name is required", http.StatusBadRequest)
		return
	}

	clientsMu.Lock()
	clientChan, ok := clients[componentName]
	clientsMu.Unlock()

	if !ok {
		http.Error(w, "component not found", http.StatusNotFound)
		return
	}

	requestID := fmt.Sprintf("%d", time.Now().UnixNano())
	responseChan := make(chan ResponseDetails)
	// responsesMu.Lock()
	responseChannels[requestID] = responseChan
	// responsesMu.Unlock()

	defer func() {
		// responsesMu.Lock()
		delete(responseChannels, requestID)
		// responsesMu.Unlock()
	}()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	headers := make(map[string]string)
	for key, values := range r.Header {
		headers[key] = values[0]
	}

	query := make(map[string]string)
	for key, values := range r.URL.Query() {
		query[key] = values[0]
	}

	fullPath := "/" + subPath
	if r.URL.RawQuery != "" {
		fullPath += "?" + r.URL.RawQuery
	}

	reqDetails := RequestDetails{
		RequestID: requestID,
		Method:    r.Method,
		Path:      fullPath,
		Headers:   headers,
		Query:     query,
		Body:      string(body),
	}

	reqDetailsJSON, err := json.Marshal(reqDetails)
	if err != nil {
		http.Error(w, "failed to marshal request details", http.StatusInternalServerError)
		return
	}

	clientChan <- string(reqDetailsJSON)

	select {
	case response := <-responseChan:
		w.WriteHeader(response.StatusCode)
		for key, value := range response.Headers {
			w.Header().Set(key, value)
		}
		w.Write([]byte(response.Body))
	case <-time.After(30 * time.Second):
		http.Error(w, "client did not respond in time", http.StatusGatewayTimeout)
	}
}

func handleResponseFromClient(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	componentName := vars["component-name"]
	if componentName == "" {
		http.Error(w, "component-name is required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read response body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var response struct {
		RequestID string          `json:"request_id"`
		Response  ResponseDetails `json:"response"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		http.Error(w, "failed to parse response", http.StatusBadRequest)
		return
	}

	log.Printf("Received response for request ID: %s\n", response.RequestID)
	log.Printf("Response details: %+v\n", response.Response)

	// responsesMu.Lock()// todo: temporarily commented out mutex locks
	if responseChan, ok := responseChannels[response.RequestID]; ok {
		responseChan <- response.Response
	}
	// responsesMu.Unlock()

	w.WriteHeader(http.StatusOK)
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

	r.HandleFunc("/health", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	r.HandleFunc("/register/{component-name}", registerClientForSSE).Methods("GET")
	r.HandleFunc("/request/{component-name}/{path:.*}", forwardRequestToClient)
	r.HandleFunc("/response/{component-name}", handleResponseFromClient).Methods("POST")
	r.HandleFunc("/handle", handleRequestFromClient).Methods("POST")

	server := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	log.Println("Server started on :8080")
	log.Fatal(server.ListenAndServe())
}
