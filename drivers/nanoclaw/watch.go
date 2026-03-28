// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"os"
	"sort"
	"time"
)

func handleWatch(msg map[string]interface{}) {
	sourceDir, _ := msg["source_dir"].(string)
	groupName, _ := msg["group"].(string)
	jid, _ := msg["jid"].(string)
	lines := 20
	if v, ok := msg["lines"].(float64); ok && v > 0 {
		lines = int(v)
	}

	group, sourceDir, err := resolveGroup(sourceDir, groupName, jid)
	if err != nil {
		writeError("GROUP_NOT_FOUND", err.Error())
		return
	}

	agentName := "Agent"

	// Emit historical messages from SQLite + JSONL session files.
	history, err := readMessages(sourceDir, group.JID, lines)
	if err != nil {
		writeError("DB_ERROR", err.Error())
		return
	}

	jsonlPath := findLatestSessionFile(sourceDir, group.Folder)
	sessionMsgs, sessionOffset := readSessionAssistantMessages(jsonlPath, lines)

	merged := mergeByTimestamp(history, sessionMsgs, lines)

	lastTS := ""
	for _, m := range merged {
		emitMessage(m, agentName)
		lastTS = m.Timestamp
	}

	if lastTS == "" {
		lastTS = time.Now().UTC().Format(time.RFC3339)
	}

	// Watch for stdin close in a goroutine
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		_, _ = os.Stdin.Read(buf)
		close(done)
	}()

	// Poll loop
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// Poll SQLite for new user messages.
			msgs, err := readNewMessages(sourceDir, group.JID, lastTS)
			if err == nil {
				for _, m := range msgs {
					emitMessage(m, agentName)
					lastTS = m.Timestamp
				}
			}

			// Poll JSONL for new agent messages.
			currentPath := findLatestSessionFile(sourceDir, group.Folder)
			if currentPath != jsonlPath {
				jsonlPath = currentPath
				sessionOffset = 0
			}
			if jsonlPath != "" {
				newMsgs, newOffset := readNewSessionMessages(jsonlPath, sessionOffset)
				for _, m := range newMsgs {
					emitMessage(m, agentName)
					if m.Timestamp > lastTS {
						lastTS = m.Timestamp
					}
				}
				sessionOffset = newOffset
			}
		}
	}
}

// mergeByTimestamp merges two sorted message slices by timestamp and returns
// the last N entries.
func mergeByTimestamp(a, b []MessageRow, limit int) []MessageRow {
	merged := make([]MessageRow, 0, len(a)+len(b))
	merged = append(merged, a...)
	merged = append(merged, b...)

	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Timestamp < merged[j].Timestamp
	})

	if limit > 0 && len(merged) > limit {
		merged = merged[len(merged)-limit:]
	}
	return merged
}

func emitMessage(m MessageRow, agentName string) {
	sender := m.SenderName
	if m.IsBotMessage {
		sender = agentName
	} else if m.IsFromMe {
		if sender == "" {
			sender = "You"
		}
	} else if sender == "" {
		sender = "?"
	}

	write(map[string]interface{}{
		"type":      "message",
		"timestamp": m.Timestamp,
		"sender":    sender,
		"content":   m.Content,
		"is_bot":    m.IsBotMessage,
	})
}
