// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/viper"
	api "skywalking.apache.org/repo/goapi/query"
)

// AddEventTools registers event-related tools with the MCP server
func AddEventTools(srv *server.MCPServer) {
	EventQueryTool.Register(srv)
}

// EventQueryRequest defines the parameters for the event query tool
type EventQueryRequest struct {
	Source      string `json:"source,omitempty"`
	Level       string `json:"level,omitempty"`
	Type        string `json:"type,omitempty"`
	Duration    string `json:"duration,omitempty"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	PageSize    int    `json:"page_size,omitempty"`
	PageNum     int    `json:"page_num,omitempty"`
}

// validateEventQueryRequest validates event query request parameters
func validateEventQueryRequest(req *EventQueryRequest) error {
	// All parameters are optional, but at least one filter should be provided
	if req.Source == "" && req.Level == "" && req.Type == "" {
		// Return warning but allow the query
		return nil
	}
	return nil
}

// queryEvents queries events from SkyWalking OAP
func queryEvents(ctx context.Context, req *EventQueryRequest) (*mcp.CallToolResult, error) {
	if err := validateEventQueryRequest(req); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Build duration
	var duration api.Duration
	if req.Duration != "" {
		duration = ParseDuration(req.Duration, false)
	} else {
		duration = BuildDuration(req.Start, req.End, "", false, DefaultDuration)
	}

	// Build pagination
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 20
	}
	if pageSize < 0 {
		return mcp.NewToolResultError("page_size cannot be negative"), nil
	}
	paging := BuildPagination(req.PageNum, pageSize)

	// GraphQL query for events
	query := `
		query queryEvents($source: String, $level: EventLevel, $type: String, $duration: Duration!, $paging: Pagination!) {
			events: queryEvents(source: $source, level: $level, type: $type, duration: $duration, paging: $paging) {
				uuid
				event
				message
				level
				startTime
				endTime
				type
				source
				parameters {
					key
					value
				}
			}
		}
	`

	// Build variables
	variables := map[string]interface{}{
		"duration": map[string]interface{}{
			"start": duration.Start,
			"end":   duration.End,
			"step":  string(duration.Step),
		},
		"paging": map[string]interface{}{
			"pageNum":  paging.PageNum,
			"pageSize": paging.PageSize,
		},
	}

	// Add optional parameters
	if req.Source != "" {
		variables["source"] = req.Source
	}
	if req.Level != "" {
		// Validate level
		level := req.Level
		validLevels := []string{"Normal", "Warning", "Critical"}
		isValid := false
		for _, v := range validLevels {
			if level == v {
				isValid = true
				break
			}
		}
		if isValid {
			variables["level"] = level
		}
	}
	if req.Type != "" {
		variables["type"] = req.Type
	}

	result, err := executeGraphQLForEvent(ctx, viper.GetString("url"), query, variables)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to query events: %v", err)), nil
	}

	jsonBytes, err := json.Marshal(result.Data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(jsonBytes)), nil
}

// executeGraphQLForEvent executes a GraphQL query for event data
func executeGraphQLForEvent(ctx context.Context, url, query string, variables map[string]interface{}) (*GraphQLResponse, error) {
	url = FinalizeURL(url)

	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP request failed with status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var graphqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&graphqlResp); err != nil {
		return nil, fmt.Errorf("failed to decode GraphQL response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		var errorMsgs []string
		for _, err := range graphqlResp.Errors {
			errorMsgs = append(errorMsgs, err.Message)
		}
		return nil, fmt.Errorf("GraphQL errors: %s", strings.Join(errorMsgs, ", "))
	}

	return &graphqlResp, nil
}

// EventQueryTool is a tool for querying events
var EventQueryTool = NewTool[EventQueryRequest, *mcp.CallToolResult](
	"query_events",
	`Query events from SkyWalking OAP with flexible filters.

This tool retrieves event information from the SkyWalking backend, allowing you to
track important system occurrences, deployments, and state changes.

Workflow:
1. Use this tool when you need to query event information
2. Specify source to filter by service, instance, or endpoint
3. Use level to filter by severity (Normal, Warning, Critical)
4. Set time range with duration or start/end time
5. Paginate through results if needed

Event Types:
- Deployment: Application deployment events
- Scaly: Auto-scaling events
- Routing: Service routing changes
- Modulation: Traffic modulation events
- CRUD: Database operations
- Exception: Application exceptions

Event Levels:
- Normal: Regular informational events
- Warning: Warning-level events
- Critical: Critical events requiring attention

Usage Tips:
- Use duration for quick queries (e.g., "-1h" for last hour)
- Combine source and level for targeted searches
- Use type to filter for specific event categories
- Adjust page_size for larger result sets

Examples:
- {"source": "service:your-service-name", "duration": "-1h"}: Events from a specific service in the past hour
- {"level": "Critical", "duration": "-24h"}: All critical events in the last 24 hours
- {"type": "Deployment", "duration": "-7d"}: All deployment events in the past week
- {"source": "endpoint:/api/users", "level": "Warning", "start": "2025-01-01 00:00:00", "end": "2025-01-01 23:59:59"}: Warning events from a specific endpoint on a specific day
- {"duration": "-30m", "page_size": 50}: Recent events with larger page size`,
	queryEvents,
	mcp.WithTitleAnnotation("Query events from SkyWalking"),
	mcp.WithString("source",
		mcp.Description("Source of the events (e.g., 'service:name', 'instance:name', 'endpoint:name'). Use this to filter events from specific entities."),
	),
	mcp.WithString("level",
		mcp.Enum("Normal", "Warning", "Critical"),
		mcp.Description(`Event severity level:
- 'Normal': Regular informational events
- 'Warning': Warning-level events
- 'Critical': Critical events requiring attention`),
	),
	mcp.WithString("type",
		mcp.Description("Event type to filter. Examples: 'Deployment', 'Scaly', 'Routing', 'Modulation', 'CRUD', 'Exception'."),
	),
	mcp.WithString("duration",
		mcp.Description("Time duration for the query relative to current time. "+
			"Negative values query the past: \"-1h\" (past 1 hour), \"-30m\" (past 30 minutes), \"-7d\" (past 7 days). "+
			"Use this OR specify both start+end"),
	),
	mcp.WithString("start",
		mcp.Description("Start time for the query. Examples: \"2025-01-01 12:00:00\", \"-1h\" (1 hour ago), \"-30m\" (30 minutes ago)"),
	),
	mcp.WithString("end",
		mcp.Description("End time for the query. Examples: \"2025-01-01 13:00:00\", \"now\", \"-10m\" (10 minutes ago)"),
	),
	mcp.WithNumber("page_size",
		mcp.Description("Number of results per page. Default is 20. Use larger values for comprehensive queries."),
	),
	mcp.WithNumber("page_num",
		mcp.Description("Page number for pagination. Default is 0 (first page)."),
	),
)
