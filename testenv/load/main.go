// Command load drives a bounded text workload against a provider directly and through the
// gateway. The compare subcommand checks the two reports against the relative acceptance
// thresholds.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultPrompt    = "Reply with exactly: OK"
	defaultRequests  = 50
	defaultWorkers   = 2
	defaultWarmup    = 1
	defaultMaxTokens = 16
	maxBodyBytes     = 4 << 20
	maxErrorSample   = 240
)

type runConfig struct {
	label       string
	endpoint    string
	model       string
	protocol    string
	auth        string
	apiVersion  string
	prompt      string
	stream      bool
	noThinking  bool
	requests    int
	concurrency int
	warmup      int
	maxTokens   int
	timeout     time.Duration
	reportPath  string
}

type outcome struct {
	ok               bool
	kind             string
	status           int
	detail           string
	promptTokens     int
	completionTokens int
	firstToken       time.Duration
}

type result struct {
	started time.Time
	latency time.Duration
	outcome outcome
}

type latency struct {
	Mean float64 `json:"mean_ms"`
	P50  float64 `json:"p50_ms"`
	P95  float64 `json:"p95_ms"`
	P99  float64 `json:"p99_ms"`
	Max  float64 `json:"max_ms"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type hostInfo struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	NumCPU int    `json:"num_cpu"`
	Go     string `json:"go_version"`
}

type loadReport struct {
	Label           string              `json:"label"`
	Protocol        string              `json:"protocol"`
	Auth            string              `json:"auth"`
	Endpoint        string              `json:"endpoint"`
	Model           string              `json:"model"`
	Prompt          string              `json:"prompt"`
	MaxTokens       int                 `json:"max_tokens"`
	Stream          bool                `json:"stream"`
	NoThinking      bool                `json:"no_thinking"`
	Requests        int                 `json:"requests"`
	Concurrency     int                 `json:"concurrency"`
	Warmup          int                 `json:"warmup"`
	StartedAt       time.Time           `json:"started_at"`
	DurationSeconds float64             `json:"duration_seconds"`
	Succeeded       int                 `json:"succeeded"`
	Failed          int                 `json:"failed"`
	SuccessRate     float64             `json:"success_rate"`
	ErrorCounts     map[string]int      `json:"error_counts"`
	StatusCounts    map[string]int      `json:"status_counts"`
	ErrorSamples    map[string][]string `json:"error_samples"`
	LatencyMS       latency             `json:"latency_ms"`
	FirstTokenMS    latency             `json:"first_token_ms"`
	ThroughputRPS   float64             `json:"throughput_rps"`
	FirstHalfP95MS  float64             `json:"first_half_p95_ms"`
	SecondHalfP95MS float64             `json:"second_half_p95_ms"`
	Usage           usage               `json:"usage"`
	Host            hostInfo            `json:"host"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "compare" {
		os.Exit(compare(os.Args[2:]))
	}
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("load", flag.ExitOnError)
	label := fs.String("label", "load", "report label")
	endpoint := fs.String("endpoint", "", "full endpoint")
	model := fs.String("model", "", "model name sent in the request")
	protocol := fs.String("protocol", "chat", "chat, responses, messages or gemini")
	auth := fs.String("auth", "", "bearer, anthropic or gemini; derived from protocol when empty")
	apiVersion := fs.String("api-version", "v1beta", "Gemini API version for a base URL")
	stream := fs.Bool("stream", false, "use a streaming response")
	noThinking := fs.Bool("no-thinking", true, "disable provider reasoning for a text-only load")
	requests := fs.Int("requests", defaultRequests, "measured requests")
	concurrency := fs.Int("concurrency", defaultWorkers, "concurrent workers")
	warmup := fs.Int("warmup", defaultWarmup, "sequential warm-up requests")
	maxTokens := fs.Int("max-tokens", defaultMaxTokens, "completion token cap")
	timeout := fs.Duration("timeout", 90*time.Second, "per-request timeout")
	prompt := fs.String("prompt", defaultPrompt, "user prompt")
	reportPath := fs.String("report", "", "write the JSON report to this path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	key := os.Getenv("SAFEAGENT_LOAD_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "SAFEAGENT_LOAD_KEY is required")
		return 2
	}
	if *endpoint == "" || *model == "" || *reportPath == "" {
		fmt.Fprintln(os.Stderr, "endpoint, model and report are required")
		return 2
	}
	if *requests < 1 || *concurrency < 1 || *concurrency > *requests || *warmup < 0 || *maxTokens < 1 {
		fmt.Fprintln(os.Stderr, "invalid requests, concurrency, warmup or max-tokens")
		return 2
	}
	if *auth == "" {
		switch *protocol {
		case "messages":
			*auth = "anthropic"
		case "gemini":
			*auth = "gemini"
		default:
			*auth = "bearer"
		}
	}
	cfg := runConfig{
		label: *label, endpoint: *endpoint, model: *model, protocol: *protocol, auth: *auth,
		apiVersion: *apiVersion, prompt: *prompt, stream: *stream, noThinking: *noThinking, requests: *requests,
		concurrency: *concurrency, warmup: *warmup, maxTokens: *maxTokens,
		timeout: *timeout, reportPath: *reportPath,
	}
	report, err := measure(cfg, key)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeReport(*reportPath, report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	printReport(report)
	return 0
}

func measure(cfg runConfig, key string) (loadReport, error) {
	body, err := json.Marshal(requestBody(cfg))
	if err != nil {
		return loadReport{}, err
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          cfg.concurrency * 2,
		MaxIdleConnsPerHost:   cfg.concurrency,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     true,
	}
	client := &http.Client{Transport: transport}
	endpoint := endpointFor(cfg)

	for i := 0; i < cfg.warmup; i++ {
		res := call(client, cfg, endpoint, key, body)
		if !res.outcome.ok {
			return loadReport{}, fmt.Errorf("warm-up request %d failed: %s %s", i+1, res.outcome.kind, res.outcome.detail)
		}
	}

	startedAt := time.Now()
	jobs := make(chan int)
	results := make(chan result, cfg.requests)
	var wg sync.WaitGroup
	wg.Add(cfg.concurrency)
	for i := 0; i < cfg.concurrency; i++ {
		go func() {
			defer wg.Done()
			for range jobs {
				results <- call(client, cfg, endpoint, key, body)
			}
		}()
	}
	for i := 0; i < cfg.requests; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(results)

	elapsed := time.Since(startedAt)
	all := make([]result, 0, cfg.requests)
	for res := range results {
		all = append(all, res)
	}
	return summarize(cfg, startedAt, elapsed, all), nil
}

func endpointFor(cfg runConfig) string {
	endpoint := strings.ReplaceAll(cfg.endpoint, "{model}", cfg.model)
	if cfg.protocol == "gemini" {
		if strings.HasSuffix(endpoint, "/") {
			endpoint = strings.TrimRight(endpoint, "/")
		}
		if !strings.Contains(endpoint, ":generateContent") && !strings.Contains(endpoint, ":streamGenerateContent") {
			action := ":generateContent"
			if cfg.stream {
				action = ":streamGenerateContent?alt=sse"
			}
			endpoint += "/" + cfg.model + action
		} else if cfg.stream && !strings.Contains(endpoint, "alt=sse") {
			separator := "?"
			if strings.Contains(endpoint, "?") {
				separator = "&"
			}
			endpoint += separator + "alt=sse"
		}
	}
	return endpoint
}

func requestBody(cfg runConfig) map[string]any {
	switch cfg.protocol {
	case "responses":
		body := map[string]any{
			"model": cfg.model, "input": cfg.prompt, "max_output_tokens": cfg.maxTokens,
			"temperature": 0, "stream": cfg.stream,
		}
		if cfg.noThinking {
			body["reasoning"] = map[string]any{"effort": "none"}
		}
		return body
	case "messages":
		body := map[string]any{
			"model": cfg.model, "max_tokens": cfg.maxTokens, "temperature": 0, "stream": cfg.stream,
			"messages": []map[string]string{{"role": "user", "content": cfg.prompt}},
		}
		if cfg.noThinking {
			body["thinking"] = map[string]any{"type": "disabled"}
		}
		return body
	case "gemini":
		config := map[string]any{"maxOutputTokens": cfg.maxTokens, "temperature": 0}
		if cfg.noThinking {
			config["thinkingConfig"] = map[string]any{"thinkingBudget": 0}
		}
		return map[string]any{
			"contents":         []map[string]any{{"role": "user", "parts": []map[string]string{{"text": cfg.prompt}}}},
			"generationConfig": config,
		}
	default:
		body := map[string]any{
			"model": cfg.model, "messages": []map[string]string{{"role": "user", "content": cfg.prompt}},
			"max_tokens": cfg.maxTokens, "temperature": 0, "stream": cfg.stream,
		}
		if cfg.noThinking {
			body["thinking"] = map[string]any{"type": "disabled"}
		}
		if cfg.stream {
			body["stream_options"] = map[string]bool{"include_usage": true}
		}
		return body
	}
}

func call(client *http.Client, cfg runConfig, endpoint, key string, body []byte) result {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return result{started: time.Now(), outcome: outcome{kind: "request", detail: err.Error()}}
	}
	switch cfg.auth {
	case "anthropic":
		request.Header.Set("x-api-key", key)
		request.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		request.Header.Set("x-goog-api-key", key)
	default:
		request.Header.Set("Authorization", "Bearer "+key)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "safeagent-load/1")

	started := time.Now()
	resp, err := client.Do(request)
	if err != nil {
		kind := "network"
		if errors.Is(err, context.DeadlineExceeded) {
			kind = "timeout"
		}
		return result{started: started, latency: time.Since(started), outcome: outcome{kind: kind, detail: err.Error()}}
	}
	defer resp.Body.Close()

	parsed := outcome{status: resp.StatusCode}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		parsed.kind = statusKind(resp.StatusCode)
		parsed.detail = readError(resp.Body)
		return result{started: started, latency: time.Since(started), outcome: parsed}
	}
	validatorStarted := time.Now()
	if cfg.stream {
		parsed = validateStream(cfg.protocol, resp.Body)
	} else {
		parsed = validateJSON(cfg.protocol, resp.Body)
	}
	parsed.status = resp.StatusCode
	latency := time.Since(started)
	if parsed.firstToken == 0 {
		parsed.firstToken = latency
	} else {
		parsed.firstToken += validatorStarted.Sub(started)
	}
	return result{started: started, latency: latency, outcome: parsed}
}

func sanitizeEndpoint(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	changed := false
	for _, key := range []string{"key", "api_key", "apikey", "token", "access_token"} {
		if query.Has(key) {
			query.Set(key, "REDACTED")
			changed = true
		}
	}
	if changed {
		parsed.RawQuery = query.Encode()
	}
	if parsed.User != nil {
		parsed.User = url.User("REDACTED")
	}
	return parsed.String()
}

func statusKind(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "http_429"
	case status >= 400 && status < 500:
		return "http_4xx"
	case status >= 500:
		return "http_5xx"
	default:
		return "http_other"
	}
}

func readError(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 1024))
	return compact(string(raw))
}

func compact(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > maxErrorSample {
		return value[:maxErrorSample]
	}
	return value
}

func validateJSON(protocol string, body io.Reader) outcome {
	raw, err := io.ReadAll(io.LimitReader(body, maxBodyBytes))
	if err != nil {
		return outcome{kind: "network", detail: err.Error()}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return outcome{kind: "protocol", detail: "invalid JSON: " + compact(err.Error())}
	}
	if failure, found := payload["error"]; found && failure != nil {
		return outcome{kind: "protocol", detail: compact("provider error: " + compact(fmt.Sprint(failure)))}
	}
	switch protocol {
	case "responses":
		if textOfResponses(payload) == "" {
			return outcome{kind: "protocol", detail: "response has no output text"}
		}
		usage := objectOf(payload["usage"])
		return outcome{ok: true, promptTokens: intOf(usage, "input_tokens"), completionTokens: intOf(usage, "output_tokens")}
	case "messages":
		if textOfMessages(payload) == "" {
			return outcome{kind: "protocol", detail: "response has no text"}
		}
		usage := objectOf(payload["usage"])
		return outcome{ok: true, promptTokens: intOf(usage, "input_tokens"), completionTokens: intOf(usage, "output_tokens")}
	case "gemini":
		if textOfGemini(payload) == "" {
			return outcome{kind: "protocol", detail: "response has no text"}
		}
		usage := objectOf(payload["usageMetadata"])
		return outcome{ok: true, promptTokens: intOf(usage, "promptTokenCount"), completionTokens: intOf(usage, "candidatesTokenCount")}
	default:
		choices := arrayOf(payload["choices"])
		if len(choices) == 0 {
			return outcome{kind: "protocol", detail: "response has no choices"}
		}
		message := objectOf(objectOf(choices[0])["message"])
		if strings.TrimSpace(stringOf(message["content"])) == "" {
			return outcome{kind: "protocol", detail: "response has no text"}
		}
		usage := objectOf(payload["usage"])
		return outcome{ok: true, promptTokens: intOf(usage, "prompt_tokens"), completionTokens: intOf(usage, "completion_tokens")}
	}
}

func validateStream(protocol string, body io.Reader) outcome {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	started := time.Now()
	sawText := false
	sawTerminal := false
	firstToken := time.Duration(0)
	promptTokens := 0
	completionTokens := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			sawTerminal = true
			break
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			return outcome{kind: "protocol", detail: "invalid SSE JSON: " + compact(err.Error())}
		}
		if failure, found := frame["error"]; found && failure != nil {
			return outcome{kind: "protocol", detail: compact("provider error: " + compact(fmt.Sprint(failure)))}
		}
		text := ""
		switch protocol {
		case "responses":
			kind := stringOf(frame["type"])
			if kind == "response.output_text.delta" {
				text = stringOf(frame["delta"])
			}
			if kind == "response.completed" || kind == "response.incomplete" || kind == "response.failed" {
				sawTerminal = true
				response := objectOf(frame["response"])
				usage := objectOf(response["usage"])
				promptTokens = intOf(usage, "input_tokens")
				completionTokens = intOf(usage, "output_tokens")
			}
		case "messages":
			delta := objectOf(frame["delta"])
			if stringOf(delta["type"]) == "text_delta" {
				text = stringOf(delta["text"])
			}
			if stringOf(frame["type"]) == "message_start" {
				usage := objectOf(objectOf(frame["message"])["usage"])
				promptTokens = intOf(usage, "input_tokens")
			}
			if stringOf(frame["type"]) == "message_stop" {
				sawTerminal = true
			}
			if stringOf(frame["type"]) == "message_delta" {
				usage := objectOf(frame["usage"])
				if value := intOf(usage, "output_tokens"); value > 0 {
					completionTokens = value
				}
			}
		case "gemini":
			for _, value := range arrayOf(frame["candidates"]) {
				candidate := objectOf(value)
				if stringOf(candidate["finishReason"]) != "" {
					sawTerminal = true
				}
				for _, partValue := range arrayOf(objectOf(candidate["content"])["parts"]) {
					part := objectOf(partValue)
					if thought, _ := part["thought"].(bool); !thought {
						text += stringOf(part["text"])
					}
				}
			}
			usage := objectOf(frame["usageMetadata"])
			if value := intOf(usage, "promptTokenCount"); value > 0 {
				promptTokens = value
			}
			if value := intOf(usage, "candidatesTokenCount"); value > 0 {
				completionTokens = value
			}
		default:
			usage := objectOf(frame["usage"])
			if value := intOf(usage, "prompt_tokens"); value > 0 {
				promptTokens = value
			}
			if value := intOf(usage, "completion_tokens"); value > 0 {
				completionTokens = value
			}
			for _, value := range arrayOf(frame["choices"]) {
				choice := objectOf(value)
				delta := objectOf(choice["delta"])
				text += stringOf(delta["content"])
				if stringOf(choice["finish_reason"]) != "" {
					sawTerminal = true
				}
			}
		}
		if strings.TrimSpace(text) != "" {
			if !sawText {
				firstToken = time.Since(started)
			}
			sawText = true
		}
	}
	if err := scanner.Err(); err != nil {
		return outcome{kind: "network", detail: err.Error()}
	}
	if !sawText {
		return outcome{kind: "protocol", detail: "stream has no visible text"}
	}
	if !sawTerminal {
		return outcome{kind: "protocol", detail: "stream has no terminal event"}
	}
	return outcome{ok: true, promptTokens: promptTokens, completionTokens: completionTokens, firstToken: firstToken}
}

func summarize(cfg runConfig, startedAt time.Time, elapsed time.Duration, all []result) loadReport {
	report := loadReport{
		Label: cfg.label, Protocol: cfg.protocol, Auth: cfg.auth, Endpoint: sanitizeEndpoint(endpointFor(cfg)),
		Model: cfg.model, Prompt: cfg.prompt, MaxTokens: cfg.maxTokens, Stream: cfg.stream, NoThinking: cfg.noThinking,
		Requests: cfg.requests, Concurrency: cfg.concurrency, Warmup: cfg.warmup,
		StartedAt: startedAt, DurationSeconds: round(elapsed.Seconds(), 3),
		ErrorCounts: map[string]int{}, StatusCounts: map[string]int{}, ErrorSamples: map[string][]string{},
		Host: hostInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, NumCPU: runtime.NumCPU(), Go: runtime.Version()},
	}
	latencies := make([]float64, 0, len(all))
	firstTokens := make([]float64, 0, len(all))
	first := make([]float64, 0, len(all))
	second := make([]float64, 0, len(all))
	mid := startedAt.Add(elapsed / 2)
	for _, res := range all {
		if res.outcome.status > 0 {
			report.StatusCounts[fmt.Sprint(res.outcome.status)]++
		}
		if !res.outcome.ok {
			report.Failed++
			report.ErrorCounts[res.outcome.kind]++
			if len(report.ErrorSamples[res.outcome.kind]) < 3 && res.outcome.detail != "" {
				report.ErrorSamples[res.outcome.kind] = append(report.ErrorSamples[res.outcome.kind], res.outcome.detail)
			}
			continue
		}
		report.Succeeded++
		report.Usage.PromptTokens += res.outcome.promptTokens
		report.Usage.CompletionTokens += res.outcome.completionTokens
		ms := float64(res.latency) / float64(time.Millisecond)
		latencies = append(latencies, ms)
		firstTokens = append(firstTokens, float64(res.outcome.firstToken)/float64(time.Millisecond))
		if res.started.Before(mid) {
			first = append(first, ms)
		} else {
			second = append(second, ms)
		}
	}
	if len(all) > 0 {
		report.SuccessRate = round(float64(report.Succeeded)/float64(len(all)), 4)
	}
	if elapsed > 0 {
		report.ThroughputRPS = round(float64(len(all))/elapsed.Seconds(), 3)
	}
	report.LatencyMS = latencyFor(latencies)
	report.FirstTokenMS = latencyFor(firstTokens)
	sort.Float64s(first)
	sort.Float64s(second)
	report.FirstHalfP95MS = percentile(first, 95)
	report.SecondHalfP95MS = percentile(second, 95)
	return report
}

func latencyFor(values []float64) latency {
	if len(values) == 0 {
		return latency{}
	}
	sort.Float64s(values)
	var sum float64
	for _, value := range values {
		sum += value
	}
	return latency{
		Mean: round(sum/float64(len(values)), 2),
		P50:  percentile(values, 50),
		P95:  percentile(values, 95),
		P99:  percentile(values, 99),
		Max:  round(values[len(values)-1], 2),
	}
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(p/100*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return round(values[index], 2)
}

func round(value float64, places int) float64 {
	factor := math.Pow(10, float64(places))
	return math.Round(value*factor) / factor
}

func writeReport(path string, report loadReport) error {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func printReport(report loadReport) {
	fmt.Printf("%s: %d/%d succeeded, p95 %.0f ms, first token p95 %.0f ms, %.2f req/s, errors %v\n",
		report.Label, report.Succeeded, report.Requests, report.LatencyMS.P95, report.FirstTokenMS.P95,
		report.ThroughputRPS, report.ErrorCounts)
}

func objectOf(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func arrayOf(value any) []any {
	array, _ := value.([]any)
	return array
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

func intOf(object map[string]any, key string) int {
	if object == nil {
		return 0
	}
	switch value := object[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	}
	return 0
}

type thresholds struct {
	maxP95Ratio      float64
	maxP95AddMS      float64
	minThroughput    float64
	maxErrorDeltaPct float64
}

type check struct {
	name   string
	pass   bool
	detail string
}

func compare(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	maxP95Ratio := fs.Float64("max-p95-ratio", 1.20, "candidate p95 divided by baseline p95")
	maxP95AddMS := fs.Float64("max-p95-add-ms", 100, "allowed p95 addition in milliseconds")
	minThroughput := fs.Float64("min-throughput-ratio", 0.90, "candidate throughput divided by baseline")
	maxErrorDelta := fs.Float64("max-error-delta-pp", 0.5, "allowed error-rate increase in percentage points")
	hardPerformance := fs.Bool("hard-performance", false, "fail on p95 and throughput thresholds; the quick smoke only reports them")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: load compare [flags] baseline.json candidate.json")
		return 2
	}
	baseline, err := readReport(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	candidate, err := readReport(fs.Arg(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	limit := thresholds{
		maxP95Ratio: *maxP95Ratio, maxP95AddMS: *maxP95AddMS,
		minThroughput: *minThroughput, maxErrorDeltaPct: *maxErrorDelta,
	}
	checks := evaluate(baseline, candidate, limit)
	if !*hardPerformance {
		for index := range checks {
			if checks[index].name == "p95 latency" || checks[index].name == "throughput" {
				checks[index].name += " (report)"
				checks[index].pass = true
				checks[index].detail = "informational at this sample size: " + checks[index].detail
			}
		}
	}
	fmt.Printf("performance smoke: %s -> %s\n", baseline.Label, candidate.Label)
	fmt.Printf("%-22s %10s %10s\n", "metric", "baseline", "gateway")
	fmt.Printf("%-22s %10d %10d\n", "succeeded/requests", baseline.Succeeded, candidate.Succeeded)
	fmt.Printf("%-22s %9.2f%% %9.2f%%\n", "error rate", (1-baseline.SuccessRate)*100, (1-candidate.SuccessRate)*100)
	fmt.Printf("%-22s %10.1f %10.1f\n", "p50 ms", baseline.LatencyMS.P50, candidate.LatencyMS.P50)
	fmt.Printf("%-22s %10.1f %10.1f\n", "p95 ms", baseline.LatencyMS.P95, candidate.LatencyMS.P95)
	fmt.Printf("%-22s %10.1f %10.1f\n", "p99 ms", baseline.LatencyMS.P99, candidate.LatencyMS.P99)
	fmt.Printf("%-22s %10.1f %10.1f\n", "first token p95", baseline.FirstTokenMS.P95, candidate.FirstTokenMS.P95)
	fmt.Printf("%-22s %10.2f %10.2f\n", "requests/s", baseline.ThroughputRPS, candidate.ThroughputRPS)
	fmt.Println()
	status := 0
	for _, item := range checks {
		mark := "PASS"
		if !item.pass {
			mark = "FAIL"
			status = 1
		}
		fmt.Printf("%s %-18s %s\n", mark, item.name, item.detail)
	}
	return status
}

func evaluate(baseline, candidate loadReport, limit thresholds) []check {
	sameScenario := baseline.Protocol == candidate.Protocol &&
		baseline.Stream == candidate.Stream &&
		baseline.Requests == candidate.Requests &&
		baseline.Concurrency == candidate.Concurrency &&
		baseline.Prompt == candidate.Prompt &&
		baseline.MaxTokens == candidate.MaxTokens &&
		baseline.NoThinking == candidate.NoThinking
	checks := []check{{
		name: "same scenario", pass: sameScenario,
		detail: fmt.Sprintf("protocol=%s stream=%t requests=%d concurrency=%d max_tokens=%d",
			candidate.Protocol, candidate.Stream, candidate.Requests, candidate.Concurrency, candidate.MaxTokens),
	}}
	baselineErrorRate := 1 - baseline.SuccessRate
	candidateErrorRate := 1 - candidate.SuccessRate
	baselineValid := baselineErrorRate <= 0.01 && baseline.ErrorCounts["http_429"] == 0 && baseline.ErrorCounts["protocol"] == 0
	checks = append(checks, check{
		name: "baseline valid", pass: baselineValid,
		detail: fmt.Sprintf("error rate %.2f%%, 429 %d, protocol %d", baselineErrorRate*100, baseline.ErrorCounts["http_429"], baseline.ErrorCounts["protocol"]),
	})

	candidateClean := candidateErrorRate <= 0.01 && candidate.ErrorCounts["http_5xx"] == 0 && candidate.ErrorCounts["http_429"] == 0 && candidate.ErrorCounts["protocol"] == 0
	checks = append(checks, check{
		name: "gateway errors", pass: candidateClean,
		detail: fmt.Sprintf("error rate %.2f%%, 5xx %d, 429 %d, protocol %d", candidateErrorRate*100,
			candidate.ErrorCounts["http_5xx"], candidate.ErrorCounts["http_429"], candidate.ErrorCounts["protocol"]),
	})
	delta := candidateErrorRate - baselineErrorRate
	checks = append(checks, check{
		name: "error delta", pass: delta <= limit.maxErrorDeltaPct/100+1e-9,
		detail: fmt.Sprintf("%+.2f pp (limit %.2f pp)", delta*100, limit.maxErrorDeltaPct),
	})

	allowedP95 := baseline.LatencyMS.P95*limit.maxP95Ratio + limit.maxP95AddMS
	checks = append(checks, check{
		name: "p95 latency", pass: candidate.LatencyMS.P95 <= allowedP95,
		detail: fmt.Sprintf("%.1f ms (limit %.1f ms)", candidate.LatencyMS.P95, allowedP95),
	})

	requiredThroughput := baseline.ThroughputRPS * limit.minThroughput
	checks = append(checks, check{
		name: "throughput", pass: candidate.ThroughputRPS >= requiredThroughput,
		detail: fmt.Sprintf("%.2f req/s (minimum %.2f)", candidate.ThroughputRPS, requiredThroughput),
	})
	return checks
}

func readReport(path string) (loadReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return loadReport{}, fmt.Errorf("read report: %w", err)
	}
	var report loadReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return loadReport{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return report, nil
}

func textOfResponses(payload map[string]any) string {
	var text strings.Builder
	for _, value := range arrayOf(payload["output"]) {
		item := objectOf(value)
		for _, partValue := range arrayOf(item["content"]) {
			part := objectOf(partValue)
			if stringOf(part["type"]) == "output_text" {
				text.WriteString(stringOf(part["text"]))
			}
		}
	}
	return text.String()
}

func textOfMessages(payload map[string]any) string {
	var text strings.Builder
	for _, value := range arrayOf(payload["content"]) {
		block := objectOf(value)
		if stringOf(block["type"]) == "text" {
			text.WriteString(stringOf(block["text"]))
		}
	}
	return text.String()
}

func textOfGemini(payload map[string]any) string {
	var text strings.Builder
	for _, value := range arrayOf(payload["candidates"]) {
		candidate := objectOf(value)
		for _, partValue := range arrayOf(objectOf(candidate["content"])["parts"]) {
			part := objectOf(partValue)
			if thought, _ := part["thought"].(bool); !thought {
				text.WriteString(stringOf(part["text"]))
			}
		}
	}
	return text.String()
}
