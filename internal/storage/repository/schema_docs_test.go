package repository_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSchemaTablesAreDocumented(t *testing.T) {
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	docs, err := os.ReadFile("../../../docs/agent/storage.md")
	if err != nil {
		t.Fatal(err)
	}
	expression := regexp.MustCompile(`CREATE TABLE IF NOT EXISTS ([a-z_]+)`)
	matches := expression.FindAllStringSubmatch(string(schema), -1)
	if len(matches) == 0 {
		t.Fatal("schema.sql has no tables")
	}
	for _, match := range matches {
		table := match[1]
		if !strings.Contains(string(docs), "`"+table+"`") {
			t.Errorf("table %s is missing from docs/agent/storage.md", table)
		}
	}
}

func TestEntityTablesExistInSchema(t *testing.T) {
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	created := map[string]bool{}
	for _, match := range regexp.MustCompile(`CREATE TABLE IF NOT EXISTS ([a-z_]+)`).FindAllStringSubmatch(string(schema), -1) {
		created[match[1]] = true
	}
	files, err := filepath.Glob("../entity/*.go")
	if err != nil {
		t.Fatal(err)
	}
	expression := regexp.MustCompile(`bun:"table:([a-z_]+)`)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range expression.FindAllStringSubmatch(string(content), -1) {
			if !created[match[1]] {
				t.Errorf("%s maps to table %s, which schema.sql does not create", filepath.Base(file), match[1])
			}
		}
	}
}
