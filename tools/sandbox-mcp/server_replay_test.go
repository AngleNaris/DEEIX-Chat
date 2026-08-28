package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func signedToolCallBody(apiKey string, uid, cid uint, requestID, callID, command string, ts int64) []byte {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "sandbox_exec",
			"arguments": map[string]any{"command": command},
			"_meta": map[string]any{
				"user_id": uid, "conversation_id": cid, "request_id": requestID, "call_id": callID,
				"ts": ts, "sig": metaSignature(apiKey, uid, cid, requestID, callID, ts),
			},
		},
	}
	body, _ := json.Marshal(payload)
	return body
}

func serveSignedRequest(handler http.Handler, apiKey string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestAuthMiddlewareRejectsSequentialAndConcurrentToolCallReplay(t *testing.T) {
	const apiKey = "replay-test-key"
	var handled atomic.Int32
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handled.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	})
	handler := authMiddleware(&Config{APIKey: apiKey}, next)
	now := time.Now().Unix()
	body := signedToolCallBody(apiKey, 7, 11, "request-sequential", "call-sequential", "echo first", now)

	first := serveSignedRequest(handler, apiKey, body)
	second := serveSignedRequest(handler, apiKey, body)
	if first.Code != http.StatusOK || second.Code != http.StatusConflict {
		t.Fatalf("sequential replay statuses = %d, %d; want 200, 409", first.Code, second.Code)
	}
	if handled.Load() != 1 {
		t.Fatalf("sequential replay reached handler %d times", handled.Load())
	}
	if !strings.Contains(second.Body.String(), `"id":"request-sequential"`) || !strings.Contains(second.Body.String(), `"code":-32009`) {
		t.Fatalf("replay response lost JSON-RPC id/error: %s", second.Body.String())
	}

	concurrentBody := signedToolCallBody(apiKey, 7, 11, "request-concurrent", "call-concurrent", "echo concurrent", now)
	const requests = 32
	var wg sync.WaitGroup
	wg.Add(requests)
	statuses := make(chan int, requests)
	for range requests {
		go func() {
			defer wg.Done()
			statuses <- serveSignedRequest(handler, apiKey, concurrentBody).Code
		}()
	}
	wg.Wait()
	close(statuses)
	successes := 0
	conflicts := 0
	for status := range statuses {
		switch status {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected concurrent replay status %d", status)
		}
	}
	if successes != 1 || conflicts != requests-1 || handled.Load() != 2 {
		t.Fatalf("concurrent replay successes=%d conflicts=%d handled=%d", successes, conflicts, handled.Load())
	}
}

func TestAuthMiddlewareRejectsSameIdentityWithChangedBody(t *testing.T) {
	const apiKey = "replay-body-key"
	var handled atomic.Int32
	handler := authMiddleware(&Config{APIKey: apiKey}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handled.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	now := time.Now().Unix()
	first := signedToolCallBody(apiKey, 7, 11, "same-request", "same-call", "echo safe", now)
	changed := signedToolCallBody(apiKey, 7, 11, "same-request", "same-call", "echo changed", now)
	if status := serveSignedRequest(handler, apiKey, first).Code; status != http.StatusOK {
		t.Fatalf("first request status = %d", status)
	}
	if status := serveSignedRequest(handler, apiKey, changed).Code; status != http.StatusConflict {
		t.Fatalf("changed-body replay status = %d, want 409", status)
	}
	if handled.Load() != 1 {
		t.Fatalf("changed-body replay reached handler %d times", handled.Load())
	}
}

func TestRequestReplayCacheExpiresWithSignatureWindow(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	current := now
	cache := newRequestReplayCache(func() time.Time { return current })
	meta := &Meta{UserID: 7, ConversationID: 11, RequestID: "request", CallID: "call", Timestamp: now.Unix()}
	if !cache.claim(meta) || cache.claim(meta) {
		t.Fatal("cache did not atomically reject a live duplicate")
	}
	current = now.Add(metaTimestampWindow + time.Second)
	if cache.claim(meta) {
		t.Fatal("cache accepted a request after its signed timestamp expired")
	}

	fresh := *meta
	fresh.Timestamp = current.Unix()
	if !cache.claim(&fresh) {
		t.Fatal("cache did not release the key for a newly signed request after expiry")
	}
	if got := len(cache.items); got != 1 {
		t.Fatalf("expired replay entries were not pruned: %d", got)
	}
}

func TestReplayRejectionResponseUsesNullForMissingID(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeReplayRejected(recorder, nil)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"id":null`) {
		t.Fatalf("unexpected missing-id replay response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type = %q", contentType)
	}
}
