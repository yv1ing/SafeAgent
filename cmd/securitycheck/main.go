package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

func findings(reader io.Reader) ([]string, error) {
	decoder := json.NewDecoder(reader)
	identifiers := map[string]bool{}
	configured := false
	for {
		var record struct {
			Config  json.RawMessage `json:"config"`
			Finding *struct {
				OSV   string `json:"osv"`
				Trace []struct {
					Function string `json:"function"`
				} `json:"trace"`
			} `json:"finding"`
		}
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		configured = configured || len(record.Config) > 0
		if record.Finding != nil && len(record.Finding.Trace) > 0 && record.Finding.Trace[0].Function != "" {
			identifiers[record.Finding.OSV] = true
		}
	}
	if !configured {
		return nil, errors.New("report does not contain scanner configuration")
	}
	result := make([]string, 0, len(identifiers))
	for identifier := range identifiers {
		result = append(result, identifier)
	}
	sort.Strings(result)
	return result, nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: securitycheck <govulncheck-json-report>")
		os.Exit(2)
	}
	report, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer report.Close()
	identifiers, err := findings(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(identifiers) > 0 {
		fmt.Fprintf(os.Stderr, "Release blocked by %d reachable vulnerability advisories: %v\n", len(identifiers), identifiers)
		os.Exit(1)
	}
	fmt.Println("No symbol-reachable vulnerability findings in report")
}
