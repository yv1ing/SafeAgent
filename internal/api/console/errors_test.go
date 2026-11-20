package console

import (
	"os"
	"strings"
	"testing"
)

func TestEveryErrorCodeHasAConsoleLabel(t *testing.T) {
	catalog, err := os.ReadFile("../../../web/src/i18n/messages.ts")
	if err != nil {
		t.Fatal(err)
	}
	chinese, err := os.ReadFile("../../../web/src/i18n/zh-CN.ts")
	if err != nil {
		t.Fatal(err)
	}
	for code := range errorMessages {
		for name, content := range map[string]string{"English": string(catalog), "Chinese": string(chinese)} {
			if !strings.Contains(content, code+":") {
				t.Errorf("%s catalog has no label for error code %s", name, code)
			}
		}
	}
}
