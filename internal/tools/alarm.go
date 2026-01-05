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
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/viper"
	api "skywalking.apache.org/repo/goapi/query"
)

// AddAlarmTools registers alarm-related tools with the MCP server
func AddAlarmTools(srv *server.MCPServer) {
	AlarmQueryTool.Register(srv)
}

// AlarmQueryRequest defines the parameters for the alarm query tool
type AlarmQueryRequest struct {
	Scope    string `json:"scope,omitempty"`
	Keyword  string `json:"keyword,omitempty"`
	Duration string `json:"duration,omitempty"`
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
	PageNum  int    `json:"page_num,omitempty"`
}

// validateAlarmQueryRequest validates alarm query request parameters
func validateAlarmQueryRequest(req *AlarmQueryRequest) error {
	// Scope and Duration are optional, but at least one should be provided for meaningful results
	// If neither scope nor keyword is provided, we'll still allow the query but warn
	return nil
}

// queryAlarms queries alarms from SkyWalking OAP
func queryAlarms(ctx context.Context, req *AlarmQueryRequest) (*mcp.CallToolResult, error) {
	if err := validateAlarmQueryRequest(req); err != nil {
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

	// GraphQL query for alarms
	query := `
		query queryAlarms($scope: Scope, $keyword: String, $duration: Duration!, $paging: Pagination!) {
			alarms: queryAlarms(scope: $scope, keyword: $keyword, duration: $duration, paging: $paging) {
				id
				keyword
				scope
				startTime
				endTime
				alarmMessage
				tags {
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

	// Add optional scope parameter
	if req.Scope != "" {
		var scope api.Scope
		if req.Scope != "" {
			scope = api.Scope(req.Scope)
			if scope.IsValid() {
				variables["scope"] = scope
			}
		}
	}

	// Add optional keyword parameter
	if req.Keyword != "" {
		variables["keyword"] = req.Keyword
	}

	result, err := executeGraphQL(ctx, viper.GetString("url"), query, variables)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to query alarms: %v", err)), nil
	}

	jsonBytes, err := json.Marshal(result.Data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(jsonBytes)), nil
}

// AlarmQueryTool is a tool for querying alarms
var AlarmQueryTool = NewTool[AlarmQueryRequest, *mcp.CallToolResult](
	"query_alarms",
	`Query alarms from SkyWalking OAP with flexible filters.

This tool retrieves alarm information from the SkyWalking backend, allowing you to
monitor system health and identify potential issues.

Workflow:
1. Use this tool when you need to query alarm information
2. Specify scope to filter by service, instance, or endpoint
3. Use keyword to search for specific alarm types
4. Set time range with duration or start/end time
5. Paginate through results if needed

Alarm Scopes:
- Service: Service-level alarms
- ServiceInstance: Service instance-level alarms
- Endpoint: Endpoint-level alarms
- All: All alarms (default)

Usage Tips:
- Use duration for quick queries (e.g., "-1h" for last hour)
- Use start/end for precise time ranges
- Combine scope and keyword for targeted searches
- Adjust page_size for larger result sets

Examples:
- {"scope": "Service", "duration": "-1h"}: Alarms in the past hour
- {"keyword": "high_latency", "duration": "-24h"}: Search for latency alarms in last 24 hours
- {"scope": "ServiceInstance", "service_name": "your-service", "duration": "-30m"}: Instance alarms in last 30 minutes
- {"start": "2025-01-01 00:00:00", "end": "2025-01-01 23:59:59", "page_size": 50}: Alarms for a specific day with larger page size`,
	queryAlarms,
	mcp.WithTitleAnnotation("Query alarms from SkyWalking"),
	mcp.WithString("scope",
		mcp.Enum(string(api.ScopeAll), string(api.ScopeService), string(api.ScopeServiceInstance), string(api.ScopeEndpoint)),
		mcp.Description(`The scope of the alarms:
- 'Service': Service-level alarms
- 'ServiceInstance': Service instance-level alarms
- 'Endpoint': Endpoint-level alarms
- 'All': All alarms (default)`),
	),
	mcp.WithString("keyword",
		mcp.Description("Keyword to search for in alarm messages. Use this to filter for specific types of alarms."),
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
