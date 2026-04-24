//
// Copyright 2021 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0
//

package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type TempoClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewTempoClient(baseURL string) *TempoClient {
	return &TempoClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// TempoSearchResponse is the response from GET /api/search
type TempoSearchResponse struct {
	Traces []TempoTraceRef `json:"traces"`
}

type TempoTraceRef struct {
	TraceID           string `json:"traceID"`
	RootServiceName   string `json:"rootServiceName"`
	RootTraceName     string `json:"rootTraceName"`
	StartTimeUnixNano string `json:"startTimeUnixNano"`
	DurationMs        int64  `json:"durationMs"`
}

// TempoTrace is the response from GET /api/traces/{traceID}
// Using the OTLP JSON format
type TempoTrace struct {
	ResourceSpans []ResourceSpan `json:"resourceSpans"`
}

type ResourceSpan struct {
	Resource   Resource    `json:"resource"`
	ScopeSpans []ScopeSpan `json:"scopeSpans"`
}

type Resource struct {
	Attributes []Attribute `json:"attributes"`
}

type ScopeSpan struct {
	Spans []Span `json:"spans"`
}

type Span struct {
	TraceID           string      `json:"traceId"`
	SpanID            string      `json:"spanId"`
	Name              string      `json:"name"`
	Attributes        []Attribute `json:"attributes"`
	StartTimeUnixNano string      `json:"startTimeUnixNano"`
	EndTimeUnixNano   string      `json:"endTimeUnixNano"`
}

type Attribute struct {
	Key   string         `json:"key"`
	Value AttributeValue `json:"value"`
}

type AttributeValue struct {
	StringValue string `json:"stringValue,omitempty"`
	IntValue    string `json:"intValue,omitempty"`
}

// Search queries Tempo for traces matching a TraceQL query
func (c *TempoClient) Search(ctx context.Context, traceQL string, start, end time.Time, limit int) (*TempoSearchResponse, error) {
	params := url.Values{
		"q":     {traceQL},
		"start": {fmt.Sprintf("%d", start.Unix())},
		"end":   {fmt.Sprintf("%d", end.Unix())},
		"limit": {fmt.Sprintf("%d", limit)},
	}
	reqURL := fmt.Sprintf("%s/api/search?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Tempo search request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing Tempo search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Tempo search returned %d", resp.StatusCode)
	}

	var result TempoSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding Tempo search response: %w", err)
	}
	return &result, nil
}

// GetTrace fetches a full trace by ID
func (c *TempoClient) GetTrace(ctx context.Context, traceID string) (*TempoTrace, error) {
	reqURL := fmt.Sprintf("%s/api/traces/%s", c.baseURL, traceID)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Tempo get trace request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing Tempo get trace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Tempo get trace returned %d", resp.StatusCode)
	}

	var result TempoTrace
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding Tempo trace response: %w", err)
	}
	return &result, nil
}
