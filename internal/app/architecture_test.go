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
	above := joined("storage/entity", "storage/repository", "storage/auditevent", "service", "pipeline", "transport", "app", "testkit")
	llmPackages := joined("llm/model", "llm/codec", "llm/provider", "llm/stream")
	domains := append(append([]string{}, llmPackages...), joined("observation", "observe", "analysis", "redact")...)
	platform := joined("platform/config", "platform/limit", "platform/routing", "platform/secure")
	return []boundary{
		{packagePath: "transport", forbidden: append(joined("storage/repository", "app"), bun)},
		{packagePath: "pipeline", forbidden: append(joined("transport", "app"), gin, bun)},
		{packagePath: "service", forbidden: append(joined("transport", "app"), gin, bun)},
		{packagePath: "service/console", forbidden: joined("transport", "app")},
		{packagePath: "storage/repository", forbidden: append(joined("service", "pipeline", "transport", "app", "redact", "routing", "llm", "analysis", "observation", "observe", "testkit"), gin)},
		{packagePath: "storage/entity", forbidden: append(joined("storage/repository", "service", "pipeline", "transport", "app", "llm", "redact", "analysis", "observation", "observe", "platform/routing", "testkit"), gin)},
		{packagePath: "storage/auditevent", forbidden: append(joined("storage/repository", "service", "pipeline", "transport", "app", "analysis", "observation", "observe", "testkit"), gin)},
		{packagePath: "llm/model", forbidden: append(append([]string{}, above...), platform...)},
		{packagePath: "llm/codec", forbidden: append(append(append([]string{}, above...), platform...), joined("llm/provider", "llm/stream")...)},
		{packagePath: "llm/provider", forbidden: append(append(append([]string{}, above...), platform...), joined("llm/codec", "llm/stream")...)},
		{packagePath: "llm/stream", forbidden: append(append(append([]string{}, above...), platform...), joined("llm/provider")...)},
		{packagePath: "observation", forbidden: append(append([]string{}, above...), platform...)},
		{packagePath: "observe", forbidden: joined("storage/entity", "storage/repository", "service", "pipeline", "transport", "app", "analysis", "llm/codec", "llm/provider", "llm/stream", "testkit")},
		{packagePath: "analysis", forbidden: append(append([]string{}, above...), append(append([]string{}, platform...), llmPackages...)...)},
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
		imports := packageImports(t, filepath.Join("..", entry.packagePath))
		for _, forbidden := range entry.forbidden {
			for _, imported := range imports {
				if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
					t.Fatalf("internal/%s must not import %s", entry.packagePath, imported)
				}
			}
		}
	}
}

func TestAnalysisHasNoTransitiveLLMDependency(t *testing.T) {
	for _, path := range []string{"./internal/analysis", "./internal/analysis/policy", "./internal/observation"} {
		deps := goListDependencies(t, path)
		for _, dependency := range deps {
			if dependency == module+"llm" || strings.HasPrefix(dependency, module+"llm/") {
				t.Fatalf("%s must not depend on %s, directly or transitively", path, dependency)
			}
		}
	}
}

// The console reads observations and provider descriptions. It must not name the wire model; the
// observation adapter is the only package allowed to bridge the two sides.
func TestConsoleDoesNotImportTheWireModel(t *testing.T) {
	for _, directory := range []string{"service/console", "transport/console"} {
		for _, imported := range packageImports(t, filepath.Join("..", directory)) {
			for _, forbidden := range joined("llm/model", "llm/codec", "llm/stream") {
				if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
					t.Fatalf("internal/%s must not import %s", directory, imported)
				}
			}
		}
	}
}

func TestDomainLibrariesBelongToNoLayer(t *testing.T) {
	for _, library := range []string{"llm/model", "llm/codec", "llm/provider", "llm/stream", "observation", "observe", "analysis", "redact", "platform/config", "platform/limit", "platform/routing", "platform/secure"} {
		imports := packageImports(t, filepath.Join("..", library))
		if library == "analysis" {
			imports = append(imports, packageImports(t, filepath.Join("..", "analysis", "policy"))...)
		}
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
	for _, imported := range packageImports(t, filepath.Join("..", "llm", "model")) {
		if strings.Contains(imported, ".") && !strings.HasPrefix(imported, module+"llm/model") {
			t.Fatalf("internal/llm/model must depend only on the standard library, found %s", imported)
		}
	}
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

func packageImports(t *testing.T, directory string) []string {
	t.Helper()
	files := token.NewFileSet()
	var imports []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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
