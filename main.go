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

package main

import (
	"choreo-local-bridge/internal/handlers"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

func main() {
	// Router for REST API
	apiRouter := mux.NewRouter()
	apiRouter.HandleFunc("/health", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	// Forward requests from this service back to the local env
	apiRouter.HandleFunc("/preview/{user}/{component}", handlers.HandlePreviewUrl)
	// Forward requests from this service back to the local env
	apiRouter.HandleFunc("/preview/{user}/{component}/{path:.*}", handlers.HandlePreviewUrl)
	// Handle outgoing requests from the local env
	apiRouter.HandleFunc("/handle-client-req", handlers.HandleRequestFromClient).Methods("POST")

	// Router for WebSocket
	wsRouter := mux.NewRouter()
	// Register local env and listen for events from /preview endpoint
	wsRouter.HandleFunc("/ws/{user}/{component}", handlers.HandleWebSocket)

	// Start REST API server on port 8080
	go func() {
		apiServer := &http.Server{
			Addr:    ":8080",
			Handler: apiRouter,
		}
		log.Println("REST API server started on :8080")
		log.Fatal(apiServer.ListenAndServe())
	}()

	// Start WebSocket server on port 8081
	wsServer := &http.Server{
		Addr:    ":8081",
		Handler: wsRouter,
	}
	log.Println("WebSocket server started on :8081")
	log.Fatal(wsServer.ListenAndServe())
}
