package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RouteTree will produce a map of string -> []string for all method + route combos that aren't excluded because of
// their prefix. This should be used like
//
//	r.Get("/routes", router.RouteTree(r, []string{
//		"/assets",
//		"/metrics",
//		"/routes",
//	 ...
//	}))
func RouteTree(router chi.Router, excludeRouteStartsWith []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		routeInfo := make(map[string][]string)
		walkFunc := func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			route = strings.Replace(route, "/*/", "/", -1)
			for _, exclude := range excludeRouteStartsWith {
				if strings.HasPrefix(route, exclude) {
					return nil
				}
			}

			if routeInfo[method] == nil {
				routeInfo[method] = make([]string, 0)
			}
			routeInfo[method] = append(routeInfo[method], route)
			return nil
		}

		if err := chi.Walk(router, walkFunc); err != nil {
			fmt.Printf("Logging err: %s\n", err.Error())
		}
		routeInfoBytes, _ := json.Marshal(routeInfo)
		_, _ = w.Write(routeInfoBytes)
	}
}
