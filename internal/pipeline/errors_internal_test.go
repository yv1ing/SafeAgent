package pipeline

import "testing"

func TestRejectedFieldNeedsAnUnsupportedComplaint(t *testing.T) {
	cases := map[string]string{
		`{"error":{"message":"The reasoning_text in the thinking mode must be passed back to the API.","param":null}}`: "",
		`{"error":{"message":"The reasoning_content in the thinking mode must be passed back to the API."}}`:           "",
		`{"error":{"message":"Unsupported parameter: 'thinking' is not supported with this model."}}`:                  "thinking",
		`{"error":{"message":"thinking is not supported","param":"thinking"}}`:                                         "thinking",
		`{"error":{"message":"Unknown parameter: tools","param":"tools"}}`:                                             "tools",
		`{"error":{"message":"unsupported parameter: top_k"}}`:                                                         "top_k",
		`{"error":{"message":"reasoning_effort is not supported"}}`:                                                    "thinking",
		`{"error":{"message":"The tool output was rejected"}}`:                                                         "",
		`{"error":{"message":"Unsupported value: 'temperature' does not support 2"}}`:                                  "",
	}
	for payload, expected := range cases {
		if got := rejectedField([]byte(payload)); got != expected {
			t.Fatalf("%s: want %q, got %q", payload, expected, got)
		}
	}
}
