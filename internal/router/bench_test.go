// M7 benchmarks — map index vs polynomial-hash index. LEARNING.md M7:
// "benchmark it against the map — that benchmark is the whole
// justification story."
//
// Run:  go test -bench . -benchmem ./internal/router
package router

import (
	"fmt"
	"testing"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
)

func noopHandler(w response.Writer, req *request.Request) {}

// buildRoutes returns a deterministic mixed route table: static routes,
// param routes, and the occasional wildcard.
func buildRoutes(n int) []string {
	routes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		switch i % 10 {
		case 3, 7:
			routes = append(routes, fmt.Sprintf("/api/v1/res%03d/:id", i))
		case 9:
			routes = append(routes, fmt.Sprintf("/static%03d/*path", i))
		default:
			routes = append(routes, fmt.Sprintf("/api/v1/res%03d", i))
		}
	}
	return routes
}

// lookupPaths derives one hit per route plus a fixed share of misses.
func lookupPaths(routes []string) []string {
	paths := make([]string, 0, len(routes)*2)
	for i, r := range routes {
		switch i % 10 {
		case 3, 7:
			paths = append(paths, r[:len(r)-len(":id")]+"42")
		case 9:
			paths = append(paths, r[:len(r)-len("*path")]+"a/b/c.txt")
		default:
			paths = append(paths, r)
		}
		paths = append(paths, fmt.Sprintf("/miss/res%03d", i))
	}
	return paths
}

func benchmarkLookup(b *testing.B, mk func() *Router) {
	for _, size := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("routes=%d", size), func(b *testing.B) {
			r := mk()
			routes := buildRoutes(size)
			for _, pattern := range routes {
				if err := r.Register("GET", pattern, noopHandler); err != nil {
					b.Fatalf("register %q: %v", pattern, err)
				}
			}
			paths := lookupPaths(routes)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = r.Lookup("GET", paths[i%len(paths)])
			}
		})
	}
}

func BenchmarkLookupMap(b *testing.B) {
	benchmarkLookup(b, New)
}

func BenchmarkLookupHash(b *testing.B) {
	benchmarkLookup(b, NewHashed)
}
