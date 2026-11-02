package datadog

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Point is one timeseries point.
type Point struct {
	T time.Time
	V float64
}

type queryResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Series []struct {
		QueryIndex int           `json:"query_index"`
		Pointlist  [][2]*float64 `json:"pointlist"`
	} `json:"series"`
}

// Query runs the queries as one /api/v1/query call, so a batch costs one request against the metrics rate limit.
// The result has the points of each query, in order. A query with no data in the window gets none, and the null
// points Datadog pads a batch onto one time grid with are dropped.
func (c *Client) Query(ctx context.Context, queries []string, from, to time.Time) ([][]Point, error) {
	params := url.Values{
		"from":  {strconv.FormatInt(from.Unix(), 10)},
		"to":    {strconv.FormatInt(to.Unix(), 10)},
		"query": {strings.Join(queries, ",")},
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/v1/query?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	var resp queryResponse
	if err := c.Do(req, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("datadog query failed: %s", resp.Error)
	}

	out := make([][]Point, len(queries))
	for _, s := range resp.Series {
		if s.QueryIndex < 0 || s.QueryIndex >= len(queries) {
			return nil, fmt.Errorf("datadog returned query_index %d for %d queries", s.QueryIndex, len(queries))
		}
		for _, p := range s.Pointlist {
			if p[0] == nil || p[1] == nil {
				continue
			}
			out[s.QueryIndex] = append(out[s.QueryIndex], Point{T: time.UnixMilli(int64(*p[0])), V: *p[1]})
		}
	}
	return out, nil
}

// ListActiveMetrics lists the metrics that reported since from.
func (c *Client) ListActiveMetrics(ctx context.Context, from time.Time) ([]string, error) {
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/v1/metrics?from="+strconv.FormatInt(from.Unix(), 10), nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Metrics []string `json:"metrics"`
	}
	if err := c.Do(req, &resp); err != nil {
		return nil, err
	}
	return resp.Metrics, nil
}
