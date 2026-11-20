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
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "\n## Related\n") {
			t.Errorf("%s has no Related section", filepath.Base(file))
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
	files := []string{"../../README.md", "../../README_zh.md"}
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
	expression := regexp.MustCompile(`\[[^\]]*\]\(([^)#]+)(?:#[^)]*)?\)`)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range expression.FindAllStringSubmatch(string(content), -1) {
			target := match[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(file), target)); err != nil {
				t.Errorf("%s links to missing file %s", filepath.Base(file), target)
			}
		}
	}
}
