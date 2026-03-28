// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// jsonlEntry represents a single line from a Claude Code JSONL session file.
type jsonlEntry struct {
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Timestamp string `json:"timestamp"`
}

// contentBlock represents a content block in an assistant message.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// findLatestSessionFile returns the most recently modified JSONL file under
// data/sessions/<groupFolder>/.claude/projects/*/*.jsonl.
// Returns "" if no file is found.
func findLatestSessionFile(sourceDir, groupFolder string) string {
	projectsDir := filepath.Join(sourceDir, "data", "sessions", groupFolder, ".claude", "projects")

	var newest string
	var newestTime time.Time

	_ = filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if info.ModTime().After(newestTime) {
			newest = path
			newestTime = info.ModTime()
		}
		return nil
	})

	return newest
}

// readSessionAssistantMessages reads assistant messages from a JSONL session file.
// Returns the messages (up to limit) and the byte offset at the end of the file.
// Only messages with role "assistant" and non-empty text content are returned.
func readSessionAssistantMessages(path string, limit int) ([]MessageRow, int64) {
	if path == "" {
		return nil, 0
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, 0
	}
	fallbackTS := info.ModTime().UTC().Format(time.RFC3339)

	var msgs []MessageRow
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "\"assistant\"") {
			continue
		}

		var entry jsonlEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Message.Role != "assistant" {
			continue
		}

		text := extractAssistantText(entry.Message.Content)
		if text == "" {
			continue
		}

		ts := entry.Timestamp
		if ts == "" {
			ts = fallbackTS
		}

		msgs = append(msgs, MessageRow{
			SenderName:   "",
			Content:      text,
			Timestamp:    ts,
			IsFromMe:     false,
			IsBotMessage: true,
		})
	}

	endOffset := info.Size()

	// Return only the last N messages.
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}

	return msgs, endOffset
}

// readNewSessionMessages reads assistant messages appended after afterOffset.
// Returns new messages and the updated byte offset.
func readNewSessionMessages(path string, afterOffset int64) ([]MessageRow, int64) {
	if path == "" {
		return nil, afterOffset
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, afterOffset
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, afterOffset
	}

	if info.Size() <= afterOffset {
		return nil, afterOffset
	}

	if _, err := f.Seek(afterOffset, 0); err != nil {
		return nil, afterOffset
	}

	fallbackTS := info.ModTime().UTC().Format(time.RFC3339)

	var msgs []MessageRow
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "\"assistant\"") {
			continue
		}

		var entry jsonlEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Message.Role != "assistant" {
			continue
		}

		text := extractAssistantText(entry.Message.Content)
		if text == "" {
			continue
		}

		ts := entry.Timestamp
		if ts == "" {
			ts = fallbackTS
		}

		msgs = append(msgs, MessageRow{
			SenderName:   "",
			Content:      text,
			Timestamp:    ts,
			IsFromMe:     false,
			IsBotMessage: true,
		})
	}

	return msgs, info.Size()
}

// extractAssistantText extracts text from a message content field.
// Content can be a plain string or an array of content blocks.
func extractAssistantText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try as a plain string first.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}

	// Try as an array of content blocks.
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	return strings.Join(parts, "\n")
}
