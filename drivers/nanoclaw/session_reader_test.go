// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeJSONLFile creates a JSONL file with the given entries (one per line).
func writeJSONLFile(t *testing.T, path string, entries []map[string]interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, e := range entries {
		data, _ := json.Marshal(e)
		_, _ = f.Write(data)
		_, _ = f.Write([]byte("\n"))
	}
}

func makeAssistantEntry(text, ts string) map[string]interface{} {
	entry := map[string]interface{}{
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": text},
			},
		},
	}
	if ts != "" {
		entry["timestamp"] = ts
	}
	return entry
}

func makeUserEntry(text, ts string) map[string]interface{} {
	entry := map[string]interface{}{
		"message": map[string]interface{}{
			"role":    "user",
			"content": text,
		},
	}
	if ts != "" {
		entry["timestamp"] = ts
	}
	return entry
}

func makeToolUseEntry() map[string]interface{} {
	return map[string]interface{}{
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "tool_use", "name": "Bash", "id": "toolu_test"},
			},
		},
	}
}

func TestFindLatestSessionFile(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "data", "sessions", "main", ".claude", "projects", "proj1")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create two JSONL files with different mtimes.
	older := filepath.Join(projDir, "old-session.jsonl")
	newer := filepath.Join(projDir, "new-session.jsonl")

	if err := os.WriteFile(older, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Set older file to the past.
	past := time.Now().Add(-1 * time.Hour)
	_ = os.Chtimes(older, past, past)

	if err := os.WriteFile(newer, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := findLatestSessionFile(dir, "main")
	if got != newer {
		t.Errorf("expected %s, got %s", newer, got)
	}
}

func TestFindLatestSessionFileNone(t *testing.T) {
	dir := t.TempDir()
	got := findLatestSessionFile(dir, "main")
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestReadSessionAssistantMessages(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "data", "sessions", "main", ".claude", "projects", "proj1")
	path := filepath.Join(projDir, "session.jsonl")

	entries := []map[string]interface{}{
		makeUserEntry("Hello", "2026-03-26T10:00:00Z"),
		makeAssistantEntry("Hi there!", "2026-03-26T10:00:01Z"),
		makeUserEntry("What's up?", "2026-03-26T10:00:02Z"),
		makeToolUseEntry(),
		makeAssistantEntry("Not much!", "2026-03-26T10:00:03Z"),
	}
	writeJSONLFile(t, path, entries)

	msgs, offset := readSessionAssistantMessages(path, 10)

	if len(msgs) != 2 {
		t.Fatalf("expected 2 assistant messages, got %d", len(msgs))
	}
	if msgs[0].Content != "Hi there!" {
		t.Errorf("expected 'Hi there!', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "Not much!" {
		t.Errorf("expected 'Not much!', got %q", msgs[1].Content)
	}
	if !msgs[0].IsBotMessage {
		t.Error("expected IsBotMessage to be true")
	}
	if msgs[0].Timestamp != "2026-03-26T10:00:01Z" {
		t.Errorf("expected timestamp from entry, got %q", msgs[0].Timestamp)
	}
	if offset <= 0 {
		t.Error("expected positive offset")
	}
}

func TestReadSessionAssistantMessagesLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	entries := []map[string]interface{}{
		makeAssistantEntry("First", "2026-03-26T10:00:01Z"),
		makeAssistantEntry("Second", "2026-03-26T10:00:02Z"),
		makeAssistantEntry("Third", "2026-03-26T10:00:03Z"),
	}
	writeJSONLFile(t, path, entries)

	msgs, _ := readSessionAssistantMessages(path, 2)

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages with limit=2, got %d", len(msgs))
	}
	// Should be the last 2.
	if msgs[0].Content != "Second" {
		t.Errorf("expected 'Second', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "Third" {
		t.Errorf("expected 'Third', got %q", msgs[1].Content)
	}
}

func TestReadSessionAssistantMessagesToolUseOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	entries := []map[string]interface{}{
		makeToolUseEntry(),
		makeToolUseEntry(),
	}
	writeJSONLFile(t, path, entries)

	msgs, _ := readSessionAssistantMessages(path, 10)

	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for tool-use-only, got %d", len(msgs))
	}
}

func TestReadSessionAssistantMessagesNoFile(t *testing.T) {
	msgs, offset := readSessionAssistantMessages("", 10)
	if len(msgs) != 0 || offset != 0 {
		t.Errorf("expected empty result for no file")
	}
}

func TestReadSessionAssistantMessagesFallbackTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// No timestamp field in the entry.
	entries := []map[string]interface{}{
		makeAssistantEntry("Hello", ""),
	}
	writeJSONLFile(t, path, entries)

	msgs, _ := readSessionAssistantMessages(path, 10)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	// Should use file mtime as fallback — just verify it's non-empty.
	if msgs[0].Timestamp == "" {
		t.Error("expected non-empty fallback timestamp")
	}
}

func TestReadNewSessionMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// Write initial content.
	entries := []map[string]interface{}{
		makeAssistantEntry("First", "2026-03-26T10:00:01Z"),
	}
	writeJSONLFile(t, path, entries)

	// Read all — get the offset.
	_, offset := readSessionAssistantMessages(path, 10)

	// Append more content.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(makeAssistantEntry("Second", "2026-03-26T10:00:02Z"))
	_, _ = f.Write(data)
	_, _ = f.Write([]byte("\n"))
	_ = f.Close()

	// Read new messages from the offset.
	newMsgs, newOffset := readNewSessionMessages(path, offset)

	if len(newMsgs) != 1 {
		t.Fatalf("expected 1 new message, got %d", len(newMsgs))
	}
	if newMsgs[0].Content != "Second" {
		t.Errorf("expected 'Second', got %q", newMsgs[0].Content)
	}
	if newOffset <= offset {
		t.Error("expected offset to advance")
	}
}

func TestReadNewSessionMessagesNoNewContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	entries := []map[string]interface{}{
		makeAssistantEntry("First", "2026-03-26T10:00:01Z"),
	}
	writeJSONLFile(t, path, entries)

	_, offset := readSessionAssistantMessages(path, 10)

	// No new content added.
	newMsgs, newOffset := readNewSessionMessages(path, offset)

	if len(newMsgs) != 0 {
		t.Errorf("expected 0 new messages, got %d", len(newMsgs))
	}
	if newOffset != offset {
		t.Errorf("expected offset unchanged, got %d vs %d", newOffset, offset)
	}
}

func TestExtractAssistantTextString(t *testing.T) {
	raw := json.RawMessage(`"Hello world"`)
	got := extractAssistantText(raw)
	if got != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", got)
	}
}

func TestExtractAssistantTextArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"Hello"},{"type":"tool_use","name":"Bash"},{"type":"text","text":"World"}]`)
	got := extractAssistantText(raw)
	if got != "Hello\nWorld" {
		t.Errorf("expected 'Hello\\nWorld', got %q", got)
	}
}

func TestExtractAssistantTextEmpty(t *testing.T) {
	got := extractAssistantText(nil)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractAssistantTextToolUseOnly(t *testing.T) {
	raw := json.RawMessage(`[{"type":"tool_use","name":"Bash","id":"toolu_test"}]`)
	got := extractAssistantText(raw)
	if got != "" {
		t.Errorf("expected empty for tool-use-only, got %q", got)
	}
}
