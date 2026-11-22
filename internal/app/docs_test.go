package app_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestHumanDocumentsAreBilingualAndAligned(t *testing.T) {
	files, err := filepath.Glob("../../docs/human/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("docs/human contains no documents")
	}
	for _, file := range files {
		name := filepath.Base(file)
		if strings.HasSuffix(name, "_zh.md") {
			continue
		}
		english, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		chinese, err := os.ReadFile(strings.TrimSuffix(file, ".md") + "_zh.md")
		if err != nil {
			t.Fatalf("%s has no Chinese counterpart: %v", name, err)
		}
		if got, want := countHeadings(string(chinese), "## "), countHeadings(string(english), "## "); got != want {
			t.Errorf("%s has %d sections, its Chinese counterpart has %d", name, want, got)
		}
		if !strings.Contains(string(english), "[中文](") {
			t.Errorf("%s has no link to its Chinese counterpart", name)
		}
		if !strings.Contains(string(chinese), "[English](") {
			t.Errorf("%s has no link to its English counterpart", strings.TrimSuffix(name, ".md")+"_zh.md")
		}
	}
}

func TestAgentDocumentsEndWithRelated(t *testing.T) {
	files, err := filepath.Glob("../../docs/agent/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("docs/agent contains no documents")
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sections := regexp.MustCompile(`(?m)^## (.+)$`).FindAllStringSubmatch(string(content), -1)
		if len(sections) == 0 || sections[len(sections)-1][1] != "Related" {
			t.Errorf("%s must end with a Related section", filepath.Base(file))
		}
	}
}

func countHeadings(document, prefix string) int {
	count := 0
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}

func TestDocumentationLinksResolve(t *testing.T) {
	expression := regexp.MustCompile(`\[[^\]]*\]\(([^)#]+)(?:#[^)]*)?\)`)
	checked := 0
	for _, file := range documentedFiles(t) {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range expression.FindAllStringSubmatch(string(content), -1) {
			target := match[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			checked++
			if _, err := os.Stat(filepath.Join(filepath.Dir(file), target)); err != nil {
				t.Errorf("%s links to missing file %s", filepath.Base(file), target)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no documentation links were checked")
	}
}

func TestReadmeDocumentsAreAligned(t *testing.T) {
	english, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	chinese, err := os.ReadFile("../../README_zh.md")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := countHeadings(string(chinese), "## "), countHeadings(string(english), "## "); got != want {
		t.Fatalf("README.md has %d sections, README_zh.md has %d", want, got)
	}
}

func TestDocumentedRepositoryPathsExist(t *testing.T) {
	expression := regexp.MustCompile("`((?:internal|cmd|testenv|web)/[^`\\s]+)`")
	checked := 0
	for _, file := range documentedFiles(t) {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range expression.FindAllStringSubmatch(string(content), -1) {
			path := strings.TrimRight(match[1], ".,;:)")
			if strings.ContainsAny(path, "*{}$") || strings.Contains(path, ".env") ||
				strings.Contains(path, "runtime") || strings.Contains(path, "web/dist") {
				continue
			}
			checked++
			if _, err := os.Stat(filepath.Join("../../", path)); err != nil {
				t.Errorf("%s documents missing path %s", filepath.Base(file), path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no repository paths were checked")
	}
}

func TestDocumentedMakeTargetsExist(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(string(makefile), "\n") {
		if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
			continue
		}
		if name, _, found := strings.Cut(line, ":"); found && !strings.Contains(name, "=") {
			targets[strings.TrimSpace(name)] = true
		}
	}
	expression := regexp.MustCompile("`make ([a-z][a-z0-9-]*)(?:[^`]*)`")
	checked := 0
	for _, file := range documentedFiles(t) {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range expression.FindAllStringSubmatch(string(content), -1) {
			checked++
			if !targets[match[1]] {
				t.Errorf("%s documents missing make target %s", filepath.Base(file), match[1])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no make targets were checked")
	}
}

func documentedFiles(t *testing.T) []string {
	t.Helper()
	files := []string{"../../README.md", "../../README_zh.md", "../../testenv/README.md"}
	err := filepath.WalkDir("../../docs", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
