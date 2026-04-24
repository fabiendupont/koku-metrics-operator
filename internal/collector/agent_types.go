//
// Copyright 2021 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0
//

package collector

import (
	"strconv"
	"strings"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// AgentBillingRecord represents a single agent invocation's cost data
type AgentBillingRecord struct {
	Namespace       string
	AgentName       string
	AgentID         string
	ModelName       string
	Organization    string
	InputTokens     int64
	OutputTokens    int64
	CacheReadTokens int64
	LLMCallCount    int
	ToolCallCount   int
	DurationSeconds float64
	TraceID         string
}

type agentBillingRow struct {
	*dateTimes
	Namespace       string
	AgentName       string
	AgentID         string
	ModelName       string
	Organization    string
	InputTokens     string
	OutputTokens    string
	CacheReadTokens string
	LLMCallCount    string
	ToolCallCount   string
	DurationSeconds string
	InvocationCount string
}

func newAgentBillingRow(ts *promv1.Range) agentBillingRow {
	return agentBillingRow{dateTimes: newDates(ts)}
}

func (agentBillingRow) csvHeader() []string {
	return []string{
		"report_period_start",
		"report_period_end",
		"interval_start",
		"interval_end",
		"namespace",
		"agent_name",
		"agent_id",
		"model_name",
		"organization",
		"input_tokens",
		"output_tokens",
		"cache_read_tokens",
		"llm_call_count",
		"tool_call_count",
		"duration_seconds",
		"invocation_count",
	}
}

func (row agentBillingRow) csvRow() []string {
	return []string{
		row.ReportPeriodStart,
		row.ReportPeriodEnd,
		row.IntervalStart,
		row.IntervalEnd,
		row.Namespace,
		row.AgentName,
		row.AgentID,
		row.ModelName,
		row.Organization,
		row.InputTokens,
		row.OutputTokens,
		row.CacheReadTokens,
		row.LLMCallCount,
		row.ToolCallCount,
		row.DurationSeconds,
		row.InvocationCount,
	}
}

func (row agentBillingRow) string() string { return strings.Join(row.csvRow(), ",") }

// extractAgentBillingRecord extracts billing data from a Tempo trace
func extractAgentBillingRecord(trace *TempoTrace) *AgentBillingRecord {
	record := &AgentBillingRecord{}

	for _, rs := range trace.ResourceSpans {
		// Extract namespace from resource attributes
		for _, attr := range rs.Resource.Attributes {
			switch attr.Key {
			case "k8s.namespace.name", "service.namespace":
				record.Namespace = attr.Value.StringValue
			case "http.request.header.x-organization", "x_organization":
				record.Organization = attr.Value.StringValue
			}
		}

		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				operationName := ""
				for _, attr := range span.Attributes {
					switch attr.Key {
					case "gen_ai.operation.name":
						operationName = attr.Value.StringValue
					case "gen_ai.agent.name":
						record.AgentName = attr.Value.StringValue
					case "gen_ai.agent.id":
						record.AgentID = attr.Value.StringValue
					case "gen_ai.request.model":
						record.ModelName = attr.Value.StringValue
					case "gen_ai.usage.input_tokens":
						if v, err := strconv.ParseInt(attr.Value.IntValue, 10, 64); err == nil {
							record.InputTokens += v
						}
					case "gen_ai.usage.output_tokens":
						if v, err := strconv.ParseInt(attr.Value.IntValue, 10, 64); err == nil {
							record.OutputTokens += v
						}
					case "gen_ai.usage.cache_read.input_tokens":
						if v, err := strconv.ParseInt(attr.Value.IntValue, 10, 64); err == nil {
							record.CacheReadTokens += v
						}
					}
				}

				switch operationName {
				case "invoke_agent":
					// Root agent span — extract duration
					startNano, _ := strconv.ParseInt(span.StartTimeUnixNano, 10, 64)
					endNano, _ := strconv.ParseInt(span.EndTimeUnixNano, 10, 64)
					if startNano > 0 && endNano > startNano {
						record.DurationSeconds = float64(endNano-startNano) / 1e9
					}
					record.TraceID = span.TraceID
				case "chat", "text_completion", "generate_content":
					record.LLMCallCount++
				case "execute_tool":
					record.ToolCallCount++
				}
			}
		}
	}

	return record
}

// aggregateRecords groups agent billing records by namespace+agent+model
// and produces daily aggregated rows
func aggregateRecords(records []*AgentBillingRecord, ts *promv1.Range) mappedCSVStruct {
	type aggKey struct {
		Namespace    string
		AgentName    string
		AgentID      string
		ModelName    string
		Organization string
	}

	type aggValue struct {
		InputTokens     int64
		OutputTokens    int64
		CacheReadTokens int64
		LLMCallCount    int
		ToolCallCount   int
		TotalDuration   float64
		Count           int
	}

	agg := make(map[aggKey]*aggValue)

	for _, r := range records {
		key := aggKey{r.Namespace, r.AgentName, r.AgentID, r.ModelName, r.Organization}
		if agg[key] == nil {
			agg[key] = &aggValue{}
		}
		v := agg[key]
		v.InputTokens += r.InputTokens
		v.OutputTokens += r.OutputTokens
		v.CacheReadTokens += r.CacheReadTokens
		v.LLMCallCount += r.LLMCallCount
		v.ToolCallCount += r.ToolCallCount
		v.TotalDuration += r.DurationSeconds
		v.Count++
	}

	rows := make(mappedCSVStruct)
	for key, val := range agg {
		rowKey := key.Namespace + "," + key.AgentName + "," + key.ModelName + "," + key.Organization
		row := newAgentBillingRow(ts)
		row.Namespace = key.Namespace
		row.AgentName = key.AgentName
		row.AgentID = key.AgentID
		row.ModelName = key.ModelName
		row.Organization = key.Organization
		row.InputTokens = strconv.FormatInt(val.InputTokens, 10)
		row.OutputTokens = strconv.FormatInt(val.OutputTokens, 10)
		row.CacheReadTokens = strconv.FormatInt(val.CacheReadTokens, 10)
		row.LLMCallCount = strconv.Itoa(val.LLMCallCount)
		row.ToolCallCount = strconv.Itoa(val.ToolCallCount)
		avgDuration := val.TotalDuration / float64(val.Count)
		row.DurationSeconds = strconv.FormatFloat(avgDuration, 'f', 6, 64)
		row.InvocationCount = strconv.Itoa(val.Count)
		rows[rowKey] = row
	}

	return rows
}
