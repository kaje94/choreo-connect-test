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
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all connections
		},
	}
)

// Allow register from client and handle WS communication
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

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

	log.Println("Received handle websocket request for key:", clientKey)

	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade to WebSocket:", err)
		return
	}
	defer conn.Close()

	// Register the client
	utils.ClientsMu.Lock()
	utils.Clients[clientKey] = conn
	utils.ClientsMu.Unlock()

	log.Printf("Client connected: %s\n", clientKey)

	// Handle incoming messages from the client
	for {
		var response utils.ResponseDetails
		err := conn.ReadJSON(&response)
		if err != nil {
			log.Printf("Client %s disconnected: %v\n", clientKey, err)
			utils.ClientsMu.Lock()
			delete(utils.Clients, clientKey)
			utils.ClientsMu.Unlock()
			return
		}

		log.Printf("Received response from client %s: %+v\n", clientKey, response)

		// Forward the response to the appropriate HTTP request handler
		utils.ClientsMu.Lock()
		if responseChan, ok := utils.ResponseChannels[response.RequestID]; ok {
			responseChan <- response
		}
		utils.ClientsMu.Unlock()
	}
}
