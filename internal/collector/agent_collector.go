//
// Copyright 2021 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0
//

package collector

import (
	"context"
	"fmt"
	"os"
	"time"

	gologr "github.com/go-logr/logr"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"

	"github.com/project-koku/koku-metrics-operator/internal/dirconfig"
)

const (
	agentBillingFilePrefix = "cm-openshift-agent-billing-"
	defaultTempoURL        = "http://tempo.openshift-tempo-operator.svc:3200"
	agentTraceQLQuery      = `{ span."gen_ai.operation.name" = "invoke_agent" }`
	agentTraceSearchLimit  = 1000
)

// generateAgentBillingReport queries Tempo for agent invocation traces
// and produces a CSV report with aggregated billing data.
func generateAgentBillingReport(log gologr.Logger, timeSeries *promv1.Range, dirCfg *dirconfig.DirectoryConfig, yearMonth string) error {
	tempoURL := os.Getenv("TEMPO_URL")
	if tempoURL == "" {
		tempoURL = defaultTempoURL
	}

	// If TEMPO_URL is explicitly set to "disabled", skip agent billing
	if tempoURL == "disabled" {
		log.Info("agent billing disabled (TEMPO_URL=disabled)")
		return nil
	}

	log.Info("querying Tempo for agent invocation traces", "url", tempoURL)

	client := NewTempoClient(tempoURL)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Search for all invoke_agent traces in the collection window
	searchResp, err := client.Search(ctx, agentTraceQLQuery, timeSeries.Start, timeSeries.End, agentTraceSearchLimit)
	if err != nil {
		// Agent billing is optional — log and continue if Tempo is unavailable
		log.Info("Tempo not available, skipping agent billing", "error", err.Error())
		return nil
	}

	if len(searchResp.Traces) == 0 {
		log.Info("no agent invocation traces found")
		return nil
	}

	log.Info("found agent invocation traces", "count", len(searchResp.Traces))

	// Fetch each trace and extract billing records
	var records []*AgentBillingRecord
	for _, traceRef := range searchResp.Traces {
		trace, err := client.GetTrace(ctx, traceRef.TraceID)
		if err != nil {
			log.Info("failed to fetch trace, skipping", "traceID", traceRef.TraceID, "error", err.Error())
			continue
		}

		record := extractAgentBillingRecord(trace)
		if record.AgentName != "" || record.InputTokens > 0 || record.OutputTokens > 0 {
			records = append(records, record)
		}
	}

	if len(records) == 0 {
		log.Info("no agent billing records extracted from traces")
		return nil
	}

	log.Info("extracted agent billing records", "count", len(records))

	// Aggregate by namespace + agent + model
	agentRows := aggregateRecords(records, timeSeries)

	emptyRow := newAgentBillingRow(timeSeries)
	agentReport := report{
		file: &file{
			name: agentBillingFilePrefix + yearMonth + ".csv",
			path: dirCfg.Reports.Path,
		},
		data: &data{
			queryData: agentRows,
			headers:   emptyRow.csvHeader(),
			prefix:    emptyRow.dateTimes.string(),
		},
	}

	log.WithName("writeResults").Info("writing agent billing results to file", "filename", agentReport.file.getName())
	if err := agentReport.writeReport(); err != nil {
		return fmt.Errorf("failed to write agent billing report: %v", err)
	}

	return nil
}
