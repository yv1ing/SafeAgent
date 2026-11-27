package app_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const module = "github.com/yv1ing/SafeAgent/internal/"

const bun = "github.com/uptrace/bun"

const gin = "github.com/gin-gonic/gin"

type boundary struct {
	packagePath string
	forbidden   []string
	// shallow keeps the scan to the package's own files. A recursive scan is right for an
	// umbrella such as service or transport; it is wrong for a leaf package with an intentional
	// subpackage, such as observation and its adapter.
	shallow bool
}

func joined(paths ...string) []string {
	values := make([]string, 0, len(paths))
	for _, path := range paths {
		values = append(values, module+path)
	}
	return values
}

// The module is one direction of dependency: transport, then pipeline, then use cases, then
// storage. The wire model, the codecs, the provider catalog and the stream engine sit below the
// pipeline. Platform packages are utilities with no knowledge of any of them.
func boundaries() []boundary {
	above := joined("storage/entity", "storage/repository", "service", "pipeline", "transport", "app", "testkit")
	llmPackages := joined("llm/model", "llm/codec", "llm/provider", "llm/stream")
	domains := append(append([]string{}, llmPackages...), joined("observation", "observation/adapter", "redact", "trajectory", "state", "threat", "policy", "decision", "evidence", "harness")...)
	platform := joined("platform/config", "platform/limit", "platform/netguard", "platform/routing", "platform/secure")
	threatPlatform := []string{}
	for _, path := range platform {
		if path != module+"platform/netguard" {
			threatPlatform = append(threatPlatform, path)
		}
	}
	return []boundary{
		{packagePath: "transport", forbidden: append(joined("storage/repository", "app"), bun)},
		{packagePath: "pipeline", forbidden: append(joined("transport", "app"), gin, bun)},
		{packagePath: "service", forbidden: append(joined("transport", "app"), gin, bun)},
		{packagePath: "service/access", forbidden: append(joined("service/console", "service/control", "service/guard", "pipeline", "transport", "app", "testkit"), gin)},
		{packagePath: "service/console", forbidden: append(joined("service/control", "service/guard", "transport", "app"), gin)},
		{packagePath: "service/control", forbidden: append(joined("service/console", "service/guard", "transport", "app"), gin, bun)},
		{packagePath: "service/guard", forbidden: append(joined("service/console", "service/control", "transport", "app"), gin, bun)},
		{packagePath: "storage/repository", forbidden: append(joined("service", "pipeline", "transport", "app", "observation", "observation/adapter", "testkit"), gin)},
		{packagePath: "storage/entity", forbidden: append(joined("storage/repository", "service", "pipeline", "transport", "app", "llm", "redact", "threat", "observation", "observation/adapter", "platform/routing", "testkit"), gin)},
		{packagePath: "llm/model", forbidden: append(append([]string{}, above...), platform...)},
		{packagePath: "llm/codec", forbidden: append(append(append([]string{}, above...), platform...), joined("llm/provider", "llm/stream")...)},
		{packagePath: "llm/provider", forbidden: append(append(append([]string{}, above...), platform...), joined("llm/codec", "llm/stream")...)},
		{packagePath: "llm/stream", forbidden: append(append(append([]string{}, above...), platform...), joined("llm/provider")...)},
		{packagePath: "observation", forbidden: append(append(append([]string{}, above...), platform...), joined("llm")...), shallow: true},
		{packagePath: "observation/adapter", forbidden: joined("storage/entity", "storage/repository", "service", "pipeline", "transport", "app", "llm/codec", "llm/provider", "llm/stream", "testkit")},
		{packagePath: "trajectory", forbidden: append(append(append([]string{}, above...), platform...), joined("llm")...)},
		{packagePath: "state", forbidden: append(append(append([]string{}, above...), platform...), joined("llm")...)},
		{packagePath: "threat", forbidden: append(append(joined("storage", "service", "pipeline", "transport", "app", "testkit"), platform...), append(threatPlatform, llmPackages...)...)},
		{packagePath: "policy", forbidden: append(append(joined("storage", "service", "pipeline", "transport", "app", "testkit"), gin, bun), joined("llm")...)},
		{packagePath: "decision", forbidden: append(append(joined("storage", "service", "pipeline", "transport", "app", "testkit"), gin, bun), joined("llm")...)},
		{packagePath: "evidence", forbidden: append(append(joined("storage", "service", "pipeline", "transport", "app", "testkit"), gin, bun), joined("llm")...)},
		{packagePath: "harness", forbidden: append(append(joined("storage", "service", "pipeline", "transport", "app", "testkit"), gin, bun), joined("llm")...)},
		{packagePath: "redact", forbidden: append(append([]string{}, above...), platform...)},
		{packagePath: "platform/config", forbidden: append(append([]string{}, above...), domains...)},
		{packagePath: "platform/secure", forbidden: append(append([]string{}, above...), domains...)},
		{packagePath: "platform/limit", forbidden: append(append([]string{}, above...), domains...)},
		{packagePath: "platform/routing", forbidden: append(append([]string{}, above...), domains...)},
		{packagePath: "platform/netguard", forbidden: append(append([]string{}, above...), domains...)},
		{packagePath: "testkit", forbidden: joined("transport", "app")},
	}
}

func TestDependencyBoundaries(t *testing.T) {
	for _, entry := range boundaries() {
		imports := packageImports(t, filepath.Join("..", entry.packagePath), entry.shallow)
		for _, forbidden := range entry.forbidden {
			for _, imported := range imports {
				if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
					t.Fatalf("internal/%s must not import %s", entry.packagePath, imported)
				}
			}
		}
	}
}

func TestThreatHasNoStorageDependency(t *testing.T) {
	for _, path := range []string{"./internal/threat", "./internal/policy", "./internal/decision"} {
		deps := goListDependencies(t, path)
		for _, dependency := range deps {
			if dependency == module+"storage" || strings.HasPrefix(dependency, module+"storage/") {
				t.Fatalf("%s must not depend on %s, directly or transitively", path, dependency)
			}
		}
	}
}

// The console reads observations and provider descriptions. It must not name the wire model; the
// observation adapter is the only package allowed to bridge the two sides.
func TestConsoleDoesNotImportTheWireModel(t *testing.T) {
	for _, directory := range []string{"service/console", "transport/console"} {
		for _, imported := range packageImports(t, filepath.Join("..", directory), false) {
			for _, forbidden := range joined("llm/model", "llm/codec", "llm/stream") {
				if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
					t.Fatalf("internal/%s must not import %s", directory, imported)
				}
			}
		}
	}
}

func TestDomainLibrariesBelongToNoLayer(t *testing.T) {
	for _, library := range []string{"llm/model", "llm/codec", "llm/provider", "llm/provider/builtin", "llm/stream", "observation", "observation/adapter", "threat", "threat/builtin", "policy", "decision", "trajectory", "state", "evidence", "harness", "redact", "platform/config", "platform/limit", "platform/netguard", "platform/routing", "platform/secure"} {
		imports := packageImports(t, filepath.Join("..", library), false)
		for _, imported := range imports {
			for _, layer := range []string{module + "storage/entity", module + "storage/repository", module + "service", module + "transport"} {
				if imported == layer || strings.HasPrefix(imported, layer+"/") {
					t.Fatalf("internal/%s must not import the %s layer", library, strings.TrimPrefix(layer, module))
				}
			}
		}
	}
}

func TestWireModelDependsOnlyOnTheStandardLibrary(t *testing.T) {
	for _, imported := range packageImports(t, filepath.Join("..", "llm", "model"), false) {
		if strings.Contains(imported, ".") && !strings.HasPrefix(imported, module+"llm/model") {
			t.Fatalf("internal/llm/model must depend only on the standard library, found %s", imported)
		}
	}
}

// TestTestkitStaysOutOfProduction imports the in-memory fixtures only from tests. A production
// package that imports testkit would drag the repository, the four codecs and the fake upstream
// into the server binary.
func TestTestkitStaysOutOfProduction(t *testing.T) {
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, imported := range importsOf(t, path) {
			if imported == module+"testkit" {
				t.Errorf("%s imports internal/testkit; test fixtures belong in _test.go files", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan internal: %v", err)
	}
}

func importsOf(t *testing.T, path string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	imports := make([]string, 0, len(parsed.Imports))
	for _, spec := range parsed.Imports {
		imports = append(imports, strings.Trim(spec.Path.Value, `"`))
	}
	return imports
}

func goListDependencies(t *testing.T, path string) []string {
	t.Helper()
	command := exec.Command("go", "list", "-deps", path)
	command.Dir = filepath.Join("..", "..")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", path, err)
	}
	return strings.Fields(string(output))
}

func packageImports(t *testing.T, directory string, shallow bool) []string {
	t.Helper()
	files := token.NewFileSet()
	var imports []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Production imports define the boundaries. Tests may compose layers deliberately, for
		// example a repository integration test that drives the console services.
		if entry.IsDir() {
			if shallow && path != directory {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			imports = append(imports, strings.Trim(spec.Path.Value, `"`))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", directory, err)
	}
	return imports
}
