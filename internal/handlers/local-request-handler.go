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
	"bytes"
	"choreo-local-bridge/internal/utils"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

// Accept request details from client and make the request within the dataplane
func HandleRequestFromClient(w http.ResponseWriter, r *http.Request) {
	// Parse the request body into RequestDetails
	var reqDetails utils.RequestDetails
	if err := json.NewDecoder(r.Body).Decode(&reqDetails); err != nil {
		// Respond with an error in ResponseDetails format
		utils.RespondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	// Construct the URL with or without the port
	var urlStr string
	if reqDetails.Port != "" {
		urlStr = fmt.Sprintf("http://%s:%s%s", reqDetails.Domain, reqDetails.Port, reqDetails.Path)
	} else {
		urlStr = fmt.Sprintf("http://%s%s", reqDetails.Domain, reqDetails.Path)
	}

	log.Println("Received request for proxy call for:", urlStr)

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
		utils.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create request: %v", err))
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
		utils.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to make request: %v", err))
		return
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		// Respond with an error in ResponseDetails format
		utils.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read response body: %v", err))
		return
	}

	// Prepare the response details
	responseDetails := utils.ResponseDetails{
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
		utils.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to encode response: %v", err))
	}
}
