package http

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/project-init/gommon/pkg/env"
	"github.com/rs/cors"
)

type UrlMap map[env.Env][]string

// GetCorsHandler returns a CORS handler configured based on the environment and port. It follows the
// principle of least privilege by restricting allowed origins based on environment, limits headers to a minimum
// of "Content-Type" and "Authorization" plus any additional headers provided, and allows only essential HTTP methods.
// Supplied Headers need not be case-sensitive and are normalized and sorted to ensure compatibility with the rs/cors
// library. It will return an error if the environment is not mapped to a URL. Multiple URL's can be provided per
// environment via the UrlMap.
//
//	GetCorsHandler(env.Development, UrlMap{env.Development: []string{"http://localhost:8081"}}, []string{"X-Custom-Header"})
//
// Allows origin "http://localhost:8081" with Allowed-Headers: "Content-Type", "Authorization", "X-Custom-Header"
//
//	GetCorsHandler(env.Staging, UrlMap{env.Staging: []string{"https://staging.example.com"}}, []string{})
//
// Allows origin "https://staging.example.com" with Allowed-Headers: "Content-Type", "Authorization"
func GetCorsHandler(e env.Env, urlMap UrlMap, allowedHeaders []string) (*cors.Cors, error) {
	// Grab URL's for the selected environment
	urls, ok := urlMap[e]
	if !ok {
		return nil, fmt.Errorf("unknown environment: %s", e)
	}

	// Build allowed headers list
	baseHeaders := []string{"content-type", "authorization"}
	allHeaders := make([]string, 0, len(baseHeaders)+len(allowedHeaders))
	allHeaders = append(allHeaders, baseHeaders...)

	// Normalize custom headers to lowercase
	for _, header := range allowedHeaders {
		allHeaders = append(allHeaders, strings.ToLower(header))
	}

	// CORS library's SortedSet requires alphabetical order
	sort.Strings(allHeaders)

	corsOptions := cors.Options{
		AllowCredentials: true,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowedHeaders:   allHeaders,
		AllowedOrigins:   urls,
	}

	return cors.New(corsOptions), nil
}
