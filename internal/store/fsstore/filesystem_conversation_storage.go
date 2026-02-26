package fsstore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
)

type conversationFileHeader struct {
	Type         string `json:"type"`
	Version      int    `json:"version"`
	ID           string `json:"id"`
	Timestamp    string `json:"timestamp"`
	Title        string `json:"title,omitempty"`
	Summary      string `json:"summary,omitempty"`
	SummarizedAt int64  `json:"summarizedAt,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
}

type conversationFileMessage struct {
	ID         string          `json:"id,omitempty"`
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Timestamp  int64           `json:"timestamp"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	StopReason string          `json:"stopReason,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	Model      string          `json:"model,omitempty"`
	Provider   string          `json:"provider,omitempty"`
	ToolCalls  json.RawMessage `json:"toolCalls,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
}

type conversationLinePeek struct {
	Type string `json:"type"`
}

func (self *fileSystemTransaction) conversationFilePath(userId string, agentId string, conversationId string) string {
	return filepath.Join(self.userAgentConversationsDirectory(userId, agentId), conversationId+".jsonl")
}

func (self *fileSystemTransaction) loadConversationHeaderByPath(conversationPath string) (*conversationFileHeader, error) {
	file, openError := os.Open(conversationPath)
	if openError != nil {
		return nil, openError
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		if scanError := scanner.Err(); scanError != nil {
			return nil, scanError
		}
		return nil, fmt.Errorf("empty conversation file")
	}
	header := &conversationFileHeader{}
	if unmarshalError := json.Unmarshal([]byte(scanner.Text()), header); unmarshalError != nil {
		return nil, unmarshalError
	}
	return header, nil
}

func (self *fileSystemTransaction) loadConversationData(userId string, agentId string, conversationId string) (*conversationFileHeader, []conversationFileMessage, error) {
	conversationPath := self.conversationFilePath(userId, agentId, conversationId)
	file, openError := os.Open(conversationPath)
	if openError != nil {
		return nil, nil, openError
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		if scanError := scanner.Err(); scanError != nil {
			return nil, nil, scanError
		}
		return nil, nil, fmt.Errorf("empty conversation file")
	}

	header := &conversationFileHeader{}
	if unmarshalError := json.Unmarshal([]byte(scanner.Text()), header); unmarshalError != nil {
		return nil, nil, unmarshalError
	}

	messages := make([]conversationFileMessage, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		peek := conversationLinePeek{}
		if unmarshalError := json.Unmarshal([]byte(line), &peek); unmarshalError == nil {
			if peek.Type == "conversation" || peek.Type == "session" {
				continue
			}
		}
		message := conversationFileMessage{}
		if unmarshalError := json.Unmarshal([]byte(line), &message); unmarshalError != nil {
			continue
		}
		if message.Role == "" {
			continue
		}
		messages = append(messages, message)
	}
	if scanError := scanner.Err(); scanError != nil {
		return nil, nil, scanError
	}

	return header, messages, nil
}

func (self *fileSystemTransaction) rewriteConversationFile(userId string, agentId string, conversationId string, header *conversationFileHeader, messages []conversationFileMessage) error {
	if header == nil {
		return fmt.Errorf("conversation header is required")
	}
	conversationPath := self.conversationFilePath(userId, agentId, conversationId)
	if makeDirectoryError := os.MkdirAll(filepath.Dir(conversationPath), 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	headerData, marshalError := json.Marshal(header)
	if marshalError != nil {
		return marshalError
	}
	buffer := bytes.NewBuffer(nil)
	buffer.Write(headerData)
	buffer.WriteByte('\n')
	for _, message := range messages {
		messageData, messageMarshalError := json.Marshal(message)
		if messageMarshalError != nil {
			return messageMarshalError
		}
		buffer.Write(messageData)
		buffer.WriteByte('\n')
	}
	return atomicfile.WriteFile(conversationPath, buffer.Bytes())
}

func (self *fileSystemTransaction) createConversationFile(userId string, agentId string, conversationId string) error {
	conversationPath := self.conversationFilePath(userId, agentId, conversationId)
	if makeDirectoryError := os.MkdirAll(filepath.Dir(conversationPath), 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	header := &conversationFileHeader{
		Type:      "conversation",
		Version:   1,
		ID:        security.NewULID(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	return self.rewriteConversationFile(userId, agentId, conversationId, header, []conversationFileMessage{})
}

func (self *fileSystemTransaction) updateConversationHeader(userId string, agentId string, conversationId string, mutate func(header *conversationFileHeader)) error {
	conversationPath := self.conversationFilePath(userId, agentId, conversationId)
	fileInfo, statError := os.Stat(conversationPath)
	if statError != nil {
		return statError
	}
	originalModifiedAt := fileInfo.ModTime()

	data, readError := os.ReadFile(conversationPath)
	if readError != nil {
		return readError
	}
	newLineIndex := bytes.IndexByte(data, '\n')
	if newLineIndex < 0 {
		return fmt.Errorf("invalid conversation file")
	}

	header := &conversationFileHeader{}
	if unmarshalError := json.Unmarshal(data[:newLineIndex], header); unmarshalError != nil {
		return unmarshalError
	}
	mutate(header)

	headerData, marshalError := json.Marshal(header)
	if marshalError != nil {
		return marshalError
	}

	buffer := bytes.NewBuffer(nil)
	buffer.Write(headerData)
	buffer.Write(data[newLineIndex:])
	if writeError := atomicfile.WriteFile(conversationPath, buffer.Bytes()); writeError != nil {
		return writeError
	}
	return os.Chtimes(conversationPath, originalModifiedAt, originalModifiedAt)
}
