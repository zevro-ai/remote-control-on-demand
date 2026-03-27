package chat

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultMaxMessages = 500

func CloneSession(sess *Session) *Session {
	if sess == nil {
		return nil
	}
	cloned := *sess
	if sess.Messages != nil {
		cloned.Messages = make([]Message, len(sess.Messages))
		for i, msg := range sess.Messages {
			cloned.Messages[i] = msg
			cloned.Messages[i].Attachments = CloneAttachments(msg.Attachments)
			cloned.Messages[i].Command = CloneCommand(msg.Command)
		}
	}
	return &cloned
}

func CloneAttachments(attachments []Attachment) []Attachment {
	if attachments == nil {
		return nil
	}
	cloned := make([]Attachment, len(attachments))
	copy(cloned, attachments)
	return cloned
}

func CloneCommand(command *CommandMeta) *CommandMeta {
	if command == nil {
		return nil
	}
	cloned := *command
	return &cloned
}

func CloneMessage(message *Message) *Message {
	if message == nil {
		return nil
	}
	cloned := *message
	cloned.Attachments = CloneAttachments(message.Attachments)
	cloned.Command = CloneCommand(message.Command)
	return &cloned
}

func AppendMessageWithLimit(messages []Message, message Message, maxMessages int) []Message {
	messages = append(messages, message)
	if maxMessages > 0 && len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}
	return messages
}

func ResolveProjectPath(baseFolder, folder string) (string, string, error) {
	if strings.TrimSpace(folder) == "" {
		return "", "", fmt.Errorf("folder is required")
	}

	baseAbs, err := filepath.Abs(baseFolder)
	if err != nil {
		return "", "", fmt.Errorf("resolving base folder: %w", err)
	}
	baseResolved, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolving base folder: %w", err)
	}

	targetAbs, err := filepath.Abs(filepath.Join(baseAbs, filepath.Clean(folder)))
	if err != nil {
		return "", "", fmt.Errorf("resolving folder %q: %w", folder, err)
	}
	targetResolved, err := evalSymlinksAllowMissing(targetAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolving folder %q: %w", folder, err)
	}

	relPath, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolving folder %q: %w", folder, err)
	}
	relResolved, err := filepath.Rel(baseResolved, targetResolved)
	if err != nil {
		return "", "", fmt.Errorf("resolving folder %q: %w", folder, err)
	}
	if relResolved == ".." || strings.HasPrefix(relResolved, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("folder %q must stay within rc.base_folder", folder)
	}

	return targetResolved, relPath, nil
}

func evalSymlinksAllowMissing(path string) (string, error) {
	current := filepath.Clean(path)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func GenerateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}

func generateID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateUniqueSessionID(existing map[string]*Session) (string, error) {
	for i := 0; i < 100; i++ {
		id, err := generateID()
		if err != nil {
			return "", err
		}
		if _, exists := existing[id]; !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique session ID after 100 attempts")
}

func latestSessionIDLocked(sessions map[string]*Session) string {
	var latestID string
	var latestTime time.Time
	for sessionID, sess := range sessions {
		if latestID == "" || sess.UpdatedAt.After(latestTime) {
			latestID = sessionID
			latestTime = sess.UpdatedAt
		}
	}
	return latestID
}
