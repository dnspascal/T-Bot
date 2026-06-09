package util

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func WriteJSONLog(filename string, data any) (string, error) {
	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create logs directory: %w", err)
	}

	shouldAppend := filepath.Ext(filename) == ".json"

	if !shouldAppend {
		filename = fmt.Sprintf("%s_%d.json", filename, time.Now().Unix())
	}

	filePath := filepath.Join(logsDir, filename)

	var entries []any
	if existing, err := os.ReadFile(filePath); err == nil && len(existing) > 0 {
		_ = json.Unmarshal(existing, &entries)
	}

	entries = append(entries, data)

	jsonBytes, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal data: %w", err)
	}

	if err := os.WriteFile(filePath, jsonBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return filePath, nil
}

func WriteLog(filename string, data string) (string, error) {
	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create logs directory: %w", err)
	}

	filePath := filepath.Join(logsDir, filename)

	if err := os.WriteFile(filePath, []byte(data), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return filePath, nil
}
