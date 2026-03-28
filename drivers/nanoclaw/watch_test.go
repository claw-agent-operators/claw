// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeByTimestamp(t *testing.T) {
	a := []MessageRow{
		{Content: "user1", Timestamp: "2026-03-26T10:00:00Z", IsFromMe: true},
		{Content: "user2", Timestamp: "2026-03-26T10:00:02Z", IsFromMe: true},
	}
	b := []MessageRow{
		{Content: "agent1", Timestamp: "2026-03-26T10:00:01Z", IsBotMessage: true},
		{Content: "agent2", Timestamp: "2026-03-26T10:00:03Z", IsBotMessage: true},
	}

	merged := mergeByTimestamp(a, b, 10)

	if len(merged) != 4 {
		t.Fatalf("expected 4 merged messages, got %d", len(merged))
	}
	expected := []string{"user1", "agent1", "user2", "agent2"}
	for i, want := range expected {
		if merged[i].Content != want {
			t.Errorf("merged[%d].Content = %q, want %q", i, merged[i].Content, want)
		}
	}
}

func TestMergeByTimestampWithLimit(t *testing.T) {
	a := []MessageRow{
		{Content: "user1", Timestamp: "2026-03-26T10:00:00Z"},
		{Content: "user2", Timestamp: "2026-03-26T10:00:02Z"},
	}
	b := []MessageRow{
		{Content: "agent1", Timestamp: "2026-03-26T10:00:01Z"},
		{Content: "agent2", Timestamp: "2026-03-26T10:00:03Z"},
	}

	merged := mergeByTimestamp(a, b, 2)

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged messages with limit=2, got %d", len(merged))
	}
	// Last 2: user2, agent2
	if merged[0].Content != "user2" || merged[1].Content != "agent2" {
		t.Errorf("expected [user2, agent2], got [%s, %s]", merged[0].Content, merged[1].Content)
	}
}

func TestMergeByTimestampEmpty(t *testing.T) {
	merged := mergeByTimestamp(nil, nil, 10)
	if len(merged) != 0 {
		t.Errorf("expected 0, got %d", len(merged))
	}

	a := []MessageRow{{Content: "user1", Timestamp: "2026-03-26T10:00:00Z"}}
	merged = mergeByTimestamp(a, nil, 10)
	if len(merged) != 1 {
		t.Errorf("expected 1, got %d", len(merged))
	}
}

func TestWatchMergedOutput(t *testing.T) {
	dir := testSourceDir(t)
	createTestDB(t, dir)
	addTestGroup(t, dir, "jid-main", "main", "main", true)

	// Add user messages to SQLite.
	addTestMessageAt(t, dir, "jid-main", "2026-03-26T10:00:00Z", "Hello agent")
	addTestMessageAt(t, dir, "jid-main", "2026-03-26T10:00:02Z", "Thanks")

	// Add agent responses to JSONL session file.
	projDir := filepath.Join(dir, "data", "sessions", "main", ".claude", "projects", "proj1")
	jsonlPath := filepath.Join(projDir, "session.jsonl")
	writeJSONLFile(t, jsonlPath, []map[string]interface{}{
		makeUserEntry("Hello agent", "2026-03-26T10:00:00Z"),
		makeAssistantEntry("Hi! How can I help?", "2026-03-26T10:00:01Z"),
		makeUserEntry("Thanks", "2026-03-26T10:00:02Z"),
		makeAssistantEntry("You're welcome!", "2026-03-26T10:00:03Z"),
	})

	// Capture handleWatch output (it blocks on stdin, so we close it immediately).
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r

	oldStdout := os.Stdout
	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	// Close stdin to make the watch exit after initial history.
	_ = w.Close()

	handleWatch(map[string]interface{}{
		"type":       "watch_request",
		"source_dir": dir,
		"group":      "main",
		"lines":      float64(20),
	})

	_ = outW.Close()
	os.Stdin = oldStdin
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(outR)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))

	var messages []map[string]interface{}
	for _, line := range lines {
		var msg map[string]interface{}
		if json.Unmarshal(line, &msg) == nil && msg["type"] == "message" {
			messages = append(messages, msg)
		}
	}

	// Should have 4 messages: 2 user (SQLite) + 2 agent (JSONL), interleaved.
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %s", len(messages), buf.String())
	}

	// Verify ordering: user, agent, user, agent.
	expectedBots := []bool{false, true, false, true}
	expectedContents := []string{"Hello agent", "Hi! How can I help?", "Thanks", "You're welcome!"}
	for i, msg := range messages {
		isBot, _ := msg["is_bot"].(bool)
		content, _ := msg["content"].(string)
		if isBot != expectedBots[i] {
			t.Errorf("message[%d] is_bot = %v, want %v", i, isBot, expectedBots[i])
		}
		if content != expectedContents[i] {
			t.Errorf("message[%d] content = %q, want %q", i, content, expectedContents[i])
		}
	}
}

func TestWatchNoJSONL(t *testing.T) {
	dir := testSourceDir(t)
	createTestDB(t, dir)
	addTestGroup(t, dir, "jid-main", "main", "main", true)
	addTestMessageAt(t, dir, "jid-main", "2026-03-26T10:00:00Z", "Hello")

	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r

	oldStdout := os.Stdout
	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	_ = w.Close()

	handleWatch(map[string]interface{}{
		"type":       "watch_request",
		"source_dir": dir,
		"group":      "main",
		"lines":      float64(20),
	})

	_ = outW.Close()
	os.Stdin = oldStdin
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(outR)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))

	var messages []map[string]interface{}
	for _, line := range lines {
		var msg map[string]interface{}
		if json.Unmarshal(line, &msg) == nil && msg["type"] == "message" {
			messages = append(messages, msg)
		}
	}

	// Should have 1 message from SQLite, no JSONL.
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0]["content"] != "Hello" {
		t.Errorf("expected 'Hello', got %q", messages[0]["content"])
	}
}
