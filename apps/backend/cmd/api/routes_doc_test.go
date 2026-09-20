package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const devFakerRoute = "POST /v1/dev/faker"

var muxRegistration = regexp.MustCompile(`mux\.Handle(?:Func)?\("([A-Z]+ /[^"]*)"`)

// registeredRoutes reads the route patterns out of this package's source. The mux does not
// expose its patterns, and the source is what a reviewer reads, so it is the reference.
func registeredRoutes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, match := range muxRegistration.FindAllStringSubmatch(string(source), -1) {
			seen[match[1]] = true
		}
	}
	routes := make([]string, 0, len(seen))
	for route := range seen {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
}

func TestAPIRoutes_listMatchesWhatTheMuxRegisters(t *testing.T) {
	// Arrange
	listed := map[string]bool{devFakerRoute: true} // appended at boot when enabled
	for _, route := range apiRoutes {
		if listed[route] {
			t.Errorf("apiRoutes lists %q twice", route)
		}
		listed[route] = true
	}

	// Act
	registered := registeredRoutes(t)

	// Assert
	for _, route := range registered {
		if !listed[route] {
			t.Errorf("%q is registered on the mux but missing from apiRoutes, so the boot log undercounts", route)
		}
		delete(listed, route)
	}
	for route := range listed {
		t.Errorf("apiRoutes lists %q but nothing registers it", route)
	}
}

func TestAPIReference_documentsEveryRegisteredRoute(t *testing.T) {
	// Arrange
	path := filepath.Join("..", "..", "..", "..", "docs", "api.md")
	reference, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read API reference: %v", err)
	}
	documented := map[string]bool{}
	row := regexp.MustCompile("(?m)^\\| `([A-Z]+ /[^`]*)`")
	for _, match := range row.FindAllStringSubmatch(string(reference), -1) {
		documented[match[1]] = true
	}

	// Act + Assert: every route has a row, and no row describes a route that is gone.
	for _, route := range registeredRoutes(t) {
		if !documented[route] {
			t.Errorf("route %q is missing from the route table in docs/api.md", route)
		}
		delete(documented, route)
	}
	for route := range documented {
		t.Errorf("docs/api.md documents %q, which the API does not register", route)
	}
}
