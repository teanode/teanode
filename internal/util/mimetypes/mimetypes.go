package mimetypes

import (
	"encoding/json"
	"strings"
)

type MediaContent struct {
	Base64  string `json:"base64,omitempty"`
	Format  string `json:"format,omitempty"`
	MediaID string `json:"mediaId,omitempty"`
}

func MIMETypeFromFormat(format string) string {
	switch strings.ToLower(format) {
	case "png":
		return "image/png"
	case "jpeg", "jpg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "pdf":
		return "application/pdf"
	case "mp4":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mov":
		return "video/quicktime"
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg":
		return "audio/ogg"
	case "svg":
		return "image/svg+xml"
	case "txt":
		return "text/plain"
	case "json":
		return "application/json"
	case "csv":
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}

func FormatFromMIMEType(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	case "application/pdf":
		return "pdf"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "video/quicktime":
		return "mov"
	case "audio/mpeg":
		return "mp3"
	case "audio/wav":
		return "wav"
	case "audio/ogg":
		return "ogg"
	case "text/plain":
		return "txt"
	case "application/json":
		return "json"
	case "text/csv":
		return "csv"
	default:
		return ""
	}
}

func DetectMedia(toolResult string) *MediaContent {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(toolResult), &raw); err != nil {
		return nil
	}

	format, _ := raw["format"].(string)
	if format == "" {
		return nil
	}

	if base64Data, ok := raw["base64"].(string); ok && base64Data != "" {
		return &MediaContent{Base64: base64Data, Format: format}
	}
	if mediaId, ok := raw["mediaId"].(string); ok && mediaId != "" {
		return &MediaContent{MediaID: mediaId, Format: format}
	}
	return nil
}

func IsImageFormat(format string) bool {
	switch strings.ToLower(format) {
	case "png", "jpeg", "jpg", "gif", "webp":
		return true
	}
	return false
}
