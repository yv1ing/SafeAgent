package auditevent_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
)

func TestCanonicalizeIsStable(t *testing.T) {
	raw := []byte(`{"b":1.0,"a":1e2,"s":"<x>","nested":{"z":[1,2.50],"a":true}}`)
	canonical, err := auditevent.Canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":100,"b":1,"nested":{"a":true,"z":[1,2.5]},"s":"<x>"}`
	if string(canonical) != want {
		t.Fatalf("canonical=%s want=%s", canonical, want)
	}
	again, err := auditevent.Canonicalize(canonical)
	if err != nil || string(again) != want {
		t.Fatalf("canonicalization is not idempotent: %s %v", again, err)
	}
}

func TestCanonicalizeRejectsTrailingData(t *testing.T) {
	if _, err := auditevent.Canonicalize([]byte(`{"a":1}{"b":2}`)); err == nil {
		t.Fatal("trailing data must be rejected")
	}
}

func TestSanitizePayloadRunsBeforeSigning(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	event := &auditevent.ChainEvent{
		CallID: "call", Sequence: 1, ProjectID: "project", Kind: auditevent.KindRequest,
		SchemaVersion: auditevent.SchemaVersion, Payload: []byte(`{"text":"a\u0000b"}`),
		CreatedAt: time.Now().UTC(),
	}
	if err := auditevent.Sign(event, secret); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(event.Payload), `\u0000`) {
		t.Fatalf("payload was not sanitized: %s", event.Payload)
	}
	if !strings.Contains(string(event.Payload), "a�b") {
		t.Fatalf("the NUL escape must become one replacement character: %s", event.Payload)
	}
	report := auditevent.VerifyChain([]auditevent.ChainEvent{*event}, secret)
	if report.Status != auditevent.StatusVerified {
		t.Fatalf("report=%+v", report)
	}
}

func TestVerifyChainLocatesTampering(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	events := signedChain(t, secret)
	if report := auditevent.VerifyChain(events, secret); report.Status != auditevent.StatusVerified || report.Events != 3 {
		t.Fatalf("valid chain: %+v", report)
	}
	tampered := append([]auditevent.ChainEvent(nil), events...)
	tampered[1].Payload = json.RawMessage(`{"text":"changed"}`)
	report := auditevent.VerifyChain(tampered, secret)
	if report.Status != auditevent.StatusBroken || report.BrokenAt != 2 || report.Error != "hash_mismatch" {
		t.Fatalf("payload tamper: %+v", report)
	}
	tampered = append([]auditevent.ChainEvent(nil), events...)
	tampered[2].PrevHash = strings.Repeat("0", 64)
	report = auditevent.VerifyChain(tampered, secret)
	if report.Status != auditevent.StatusBroken || report.BrokenAt != 3 || report.Error != "previous_hash_mismatch" {
		t.Fatalf("previous hash tamper: %+v", report)
	}
	if report := auditevent.VerifyChain(events, []byte("ffffffffffffffffffffffffffffffff")); report.Status != auditevent.StatusBroken || report.BrokenAt != 1 {
		t.Fatalf("wrong key: %+v", report)
	}
}

func signedChain(t *testing.T, secret []byte) []auditevent.ChainEvent {
	t.Helper()
	events := make([]auditevent.ChainEvent, 0, 3)
	previous := ""
	for index, payload := range []string{`{"n":1}`, `{"n":2}`, `{"n":3}`} {
		event := auditevent.ChainEvent{
			CallID: "call", Sequence: int64(index + 1), ProjectID: "project", Kind: auditevent.KindRequest,
			SchemaVersion: auditevent.SchemaVersion, Payload: []byte(payload),
			CreatedAt: time.Unix(1000+int64(index), 0).UTC(), PrevHash: previous,
		}
		if err := auditevent.Sign(&event, secret); err != nil {
			t.Fatal(err)
		}
		previous = event.Hash
		events = append(events, event)
	}
	return events
}

func TestDecodeAndTimeline(t *testing.T) {
	records := []auditevent.Record{
		{Sequence: 1, Kind: auditevent.KindRequest, Payload: json.RawMessage(`{"items":[]}`), CreatedAt: time.Unix(1, 0)},
		{Sequence: 2, Kind: auditevent.KindAttempt, Payload: json.RawMessage(`{"channel_id":"ch","channel_name":"primary","model_id":"model_1","upstream_model":"m","key_id":"k"}`), CreatedAt: time.Unix(2, 0)},
		{Sequence: 3, Kind: auditevent.KindToolCall, Payload: json.RawMessage(`{"id":"call_1","name":"shell","blocked":true}`), CreatedAt: time.Unix(3, 0)},
		{Sequence: 4, Kind: auditevent.KindResponse, Payload: json.RawMessage(`{"response":{"status":"blocked","finish":{"kind":"stop"}},"blocked":true}`), CreatedAt: time.Unix(4, 0)},
		{Sequence: 5, Kind: auditevent.KindTerminal, Payload: json.RawMessage(`{"state":"blocked","code":"tool_call_blocked"}`), CreatedAt: time.Unix(5, 0)},
	}
	entries, err := auditevent.BuildTimeline(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 || entries[2].Status != "blocked" || entries[3].Status != "blocked" || entries[4].Status != "blocked" {
		t.Fatalf("entries=%+v", entries)
	}
	if entries[1].Message != "primary" || entries[1].Code != "m" || entries[1].Decoded.Attempt.ModelID != "model_1" {
		t.Fatalf("attempt entry=%+v", entries[1])
	}
}
