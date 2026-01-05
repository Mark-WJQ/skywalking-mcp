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
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/viper"
	api "skywalking.apache.org/repo/goapi/query"
)

// AddTopologyTools registers topology-related tools with the MCP server
func AddTopologyTools(srv *server.MCPServer) {
	ServiceTopologyTool.Register(srv)
	InstanceTopologyTool.Register(srv)
	EndpointTopologyTool.Register(srv)
}

// TopologyRequest defines the parameters for the topology query tool
type TopologyRequest struct {
	ServiceID         string `json:"service_id,omitempty"`
	ServiceName       string `json:"service_name,omitempty"`
	Duration          string `json:"duration,omitempty"`
	Depth             int    `json:"depth,omitempty"`
	MetricNames       string `json:"metric_names,omitempty"`
}

// ServiceTopologyRequest defines the parameters for service topology
type ServiceTopologyRequest struct {
	ServiceID   string `json:"service_id,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	Duration    string `json:"duration,omitempty"`
	Depth       int    `json:"depth,omitempty"`
}

// InstanceTopologyRequest defines the parameters for instance topology
type InstanceTopologyRequest struct {
	ServiceID   string `json:"service_id,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	Duration    string `json:"duration,omitempty"`
}

// EndpointTopologyRequest defines the parameters for endpoint topology
type EndpointTopologyRequest struct {
	ServiceID   string `json:"service_id,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	Duration    string `json:"duration,omitempty"`
}

// validateTopologyRequest validates topology request parameters
func validateTopologyRequest(req *TopologyRequest) error {
	// Either service_id or service_name should be provided
	if req.ServiceID == "" && req.ServiceName == "" {
		return errors.New("either service_id or service_name must be provided")
	}
	return nil
}

// queryServiceTopology queries service topology from SkyWalking OAP
func queryServiceTopology(ctx context.Context, req *ServiceTopologyRequest) (*mcp.CallToolResult, error) {
	topologyReq := &TopologyRequest{
		ServiceID:   req.ServiceID,
		ServiceName: req.ServiceName,
		Duration:    req.Duration,
		Depth:       req.Depth,
	}
	return queryTopology(ctx, topologyReq, "Service")
}

// queryInstanceTopology queries instance topology from SkyWalking OAP
func queryInstanceTopology(ctx context.Context, req *InstanceTopologyRequest) (*mcp.CallToolResult, error) {
	topologyReq := &TopologyRequest{
		ServiceID:   req.ServiceID,
		ServiceName: req.ServiceName,
		Duration:    req.Duration,
	}
	return queryTopology(ctx, topologyReq, "Instance")
}

// queryEndpointTopology queries endpoint topology from SkyWalking OAP
func queryEndpointTopology(ctx context.Context, req *EndpointTopologyRequest) (*mcp.CallToolResult, error) {
	topologyReq := &TopologyRequest{
		ServiceID:   req.ServiceID,
		ServiceName: req.ServiceName,
		Duration:    req.Duration,
	}
	return queryTopology(ctx, topologyReq, "Endpoint")
}

// queryTopology is the generic topology query function
func queryTopology(ctx context.Context, req *TopologyRequest, topologyType string) (*mcp.CallToolResult, error) {
	if err := validateTopologyRequest(req); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Build duration
	var duration api.Duration
	if req.Duration != "" {
		duration = ParseDuration(req.Duration, false)
	} else {
		duration = BuildDuration("", "", "", false, DefaultDuration)
	}

	// Determine service ID
	serviceID := req.ServiceID
	if serviceID == "" && req.ServiceName != "" {
		// We'll use the service name in the query
		serviceID = req.ServiceName
	}

	// Build GraphQL query based on topology type
	var query string
	var variables map[string]interface{}

	switch topologyType {
	case "Service":
		query = `
			query getServiceTopology($serviceId: ID!, $duration: Duration!) {
				serviceTopology: getServiceTopology(serviceId: $serviceId, duration: $duration) {
					nodes {
						id
						name
						type
						isReal
					}
					calls {
						id
						source
						target
						isDetectPoint
						type
						component
					}
				}
			}
		`
		variables = map[string]interface{}{
			"serviceId": serviceID,
			"duration": map[string]interface{}{
				"start": duration.Start,
				"end":   duration.End,
				"step":  string(duration.Step),
			},
		}
	case "Instance":
		query = `
			query getServiceInstanceTopology($serviceId: ID!, $duration: Duration!) {
				instanceTopology: getServiceInstanceTopology(serviceId: $serviceId, duration: $duration) {
					nodes {
						id
						name
						serviceId
						serviceName
					}
					calls {
						id
						source
						target
						type
						component
					}
				}
			}
		`
		variables = map[string]interface{}{
			"serviceId": serviceID,
			"duration": map[string]interface{}{
				"start": duration.Start,
				"end":   duration.End,
				"step":  string(duration.Step),
			},
		}
	case "Endpoint":
		query = `
			query getEndpointTopology($serviceId: ID!, $duration: Duration!) {
				endpointTopology: getEndpointTopology(serviceId: $serviceId, duration: $duration) {
					nodes {
						id
						name
						serviceId
						serviceName
					}
					calls {
						id
						source
						target
						type
						component
					}
				}
			}
		`
		variables = map[string]interface{}{
			"serviceId": serviceID,
			"duration": map[string]interface{}{
				"start": duration.Start,
				"end":   duration.End,
				"step":  string(duration.Step),
			},
		}
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown topology type: %s", topologyType)), nil
	}

	result, err := executeGraphQL(ctx, viper.GetString("url"), query, variables)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to query %s topology: %v", topologyType, err)), nil
	}

	jsonBytes, err := json.Marshal(result.Data)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(jsonBytes)), nil
}

// ServiceTopologyTool is a tool for querying service topology
var ServiceTopologyTool = NewTool[ServiceTopologyRequest, *mcp.CallToolResult](
	"get_service_topology",
	`Get service topology showing service relationships and call patterns.

This tool retrieves the service topology graph from SkyWalking, showing how services
interact with each other through calls and dependencies.

Workflow:
1. Use this tool to visualize service relationships
2. Identify service dependencies and call patterns
3. Detect potential performance bottlenecks
4. Understand microservice architecture

Topology Information:
- Nodes: Services with their types (e.g., HTTP, RPC, Database)
- Calls: Relationships between services (source -> target)
- Metrics: Optional metrics on calls (response time, throughput)

Examples:
- {"service_id": "your-service-id", "duration": "-1h"}: Service topology for the past hour
- {"service_name": "user-service", "duration": "-24h"}: Topology by service name for last 24 hours
- {"service_id": "order-service", "depth": 2}: Topology with depth 2 (2-hop relationships)`,
	queryServiceTopology,
	mcp.WithTitleAnnotation("Query service topology"),
	mcp.WithString("service_id",
		mcp.Description("Service ID to query topology for. Use service_id if you have the exact ID."),
	),
	mcp.WithString("service_name",
		mcp.Description("Service name to query topology for. Alternative to service_id."),
	),
	mcp.WithString("duration",
		mcp.Description("Time duration for the query. Examples: \"-1h\" (past hour), \"-24h\" (past 24 hours), \"-7d\" (past week). Default is last 30 minutes."),
	),
	mcp.WithNumber("depth",
		mcp.Description("Depth of topology to fetch (number of hops). Default is 1."),
	),
)

// InstanceTopologyTool is a tool for querying service instance topology
var InstanceTopologyTool = NewTool[InstanceTopologyRequest, *mcp.CallToolResult](
	"get_instance_topology",
	`Get service instance topology showing instance-level relationships.

This tool retrieves the instance topology graph, showing how instances of services
interact with each other. Useful for understanding service mesh and load balancing.

Workflow:
1. Use this tool to visualize instance relationships
2. Identify instance-to-instance communication patterns
3. Debug load balancing issues
4. Understand instance distribution

Examples:
- {"service_id": "your-service-id", "duration": "-1h"}: Instance topology for the past hour
- {"service_name": "user-service", "duration": "-30m"}: Instance topology by service name`,
	queryInstanceTopology,
	mcp.WithTitleAnnotation("Query service instance topology"),
	mcp.WithString("service_id",
		mcp.Description("Service ID to query instance topology for."),
	),
	mcp.WithString("service_name",
		mcp.Description("Service name to query instance topology for."),
	),
	mcp.WithString("duration",
		mcp.Description("Time duration for the query. Examples: \"-1h\", \"-30m\". Default is last 30 minutes."),
	),
)

// EndpointTopologyTool is a tool for querying endpoint topology
var EndpointTopologyTool = NewTool[EndpointTopologyRequest, *mcp.CallToolResult](
	"get_endpoint_topology",
	`Get endpoint topology showing endpoint-level relationships.

This tool retrieves the endpoint topology graph, showing how endpoints interact
with each other across service boundaries.

Workflow:
1. Use this tool to visualize endpoint relationships
2. Identify API call patterns
3. Understand endpoint dependencies
4. Analyze inter-service API usage

Examples:
- {"service_id": "your-service-id", "duration": "-1h"}: Endpoint topology for the past hour
- {"service_name": "api-gateway", "duration": "-24h"}: Endpoint topology by service name`,
	queryEndpointTopology,
	mcp.WithTitleAnnotation("Query endpoint topology"),
	mcp.WithString("service_id",
		mcp.Description("Service ID to query endpoint topology for."),
	),
	mcp.WithString("service_name",
		mcp.Description("Service name to query endpoint topology for."),
	),
	mcp.WithString("duration",
		mcp.Description("Time duration for the query. Examples: \"-1h\", \"-30m\". Default is last 30 minutes."),
	),
)
