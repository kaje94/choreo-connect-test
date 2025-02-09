/*
 * Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com/) All Rights Reserved.
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package handlers

import (
	"choreo-local-bridge/internal/utils"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// Handle preview URL request and forward it to client via websocket connection
func HandlePreviewUrl(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	subPath := vars["path"]

	userHandle := vars["user"]
	if userHandle == "" {
		http.Error(w, "user is required", http.StatusBadRequest)
		return
	}

	componentHandle := vars["component"]
	if componentHandle == "" {
		http.Error(w, "component is required", http.StatusBadRequest)
		return
	}

	clientKey := fmt.Sprintf("%s-%s", userHandle, componentHandle)

	log.Println("Received handle request request for key:", clientKey)

	utils.ClientsMu.Lock()
	conn, ok := utils.Clients[clientKey]
	utils.ClientsMu.Unlock()

	if !ok {
		http.Error(w, "component key not found", http.StatusNotFound)
		return
	}

	// Generate a unique request ID
	requestID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Create a response channel for this request
	responseChan := make(chan utils.ResponseDetails)
	utils.ResponsesMu.Lock()
	utils.ResponseChannels[requestID] = responseChan
	utils.ResponsesMu.Unlock()

	defer func() {
		utils.ResponsesMu.Lock()
		delete(utils.ResponseChannels, requestID)
		utils.ResponsesMu.Unlock()
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
	reqDetails := utils.RequestDetails{
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
