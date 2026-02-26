package v1api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	_ "image/gif"
	_ "image/png"

	"github.com/gorilla/mux"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/mimetypes"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
)

func (self *v1Api) handleHealth(writer http.ResponseWriter, request *http.Request) error {
	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(`{"status":"ok"}`))
	return nil
}

func (self *v1Api) handleMedia(writer http.ResponseWriter, request *http.Request) error {
	mediaId := mux.Vars(request)["id"]
	if mediaId == "" {
		return web.Error(400, "missing media id")
	}
	var mediaReader io.ReadCloser
	var metadata *models.Media
	transactionError := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
		var openError error
		mediaReader, metadata, openError = transaction.OpenMedia(ctx, mediaId, nil)
		return openError
	})
	if transactionError != nil {
		return web.ErrNotFound
	}
	defer mediaReader.Close()
	contentType := metadata.GetContentType()
	if contentType == "" {
		contentType = mimetypes.MIMETypeFromFormat(metadata.GetFormat())
	}
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, copyError := io.Copy(writer, mediaReader)
	if copyError != nil {
		return copyError
	}
	return nil
}

const maxUploadSize = 50 << 20 // 50 MB

func (self *v1Api) handleMediaUpload(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.Error(405, "method not allowed")
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxUploadSize)
	if err := request.ParseMultipartForm(maxUploadSize); err != nil {
		return web.Error(400, "file too large or invalid multipart form")
	}

	file, header, err := request.FormFile("file")
	if err != nil {
		return web.Error(400, "missing file field")
	}
	defer file.Close()

	// Determine format from file extension, falling back to Content-Type.
	filename := header.Filename
	ext := strings.TrimPrefix(filepath.Ext(filename), ".")
	format := strings.ToLower(ext)
	if format == "" {
		format = mimetypes.FormatFromMIMEType(header.Header.Get("Content-Type"))
	}
	if format == "" {
		format = "bin"
	}

	userId := ""
	if user := models.UserFromContext(request.Context()); user != nil {
		userId = user.ID
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mimetypes.MIMETypeFromFormat(format)
	}
	metadata := &models.Media{
		UserID:       ptrto.TrimmedString(userId),
		Format:       ptrto.TrimmedString(format),
		ContentType:  ptrto.TrimmedString(contentType),
		Source:       ptrto.TrimmedString("upload"),
		OriginalName: ptrto.TrimmedString(filename),
	}
	var saved *models.Media
	createError := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
		var err error
		saved, err = transaction.CreateMedia(ctx, file, metadata, nil)
		return err
	})
	if createError != nil {
		return web.Error(500, "failed to save file: "+createError.Error())
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"mediaId":  saved.ID,
		"format":   format,
		"filename": filename,
	})
	return nil
}

const maxAudioUploadSize = 25 << 20 // 25 MB (OpenAI Whisper limit)

func (self *v1Api) handleAudioTranscribe(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.Error(405, "method not allowed")
	}

	registry := self.coordinator.Providers()
	if registry == nil {
		return web.Error(500, "provider registry not available")
	}

	transcriber, _, ok := registry.FindTranscriber()
	if !ok {
		return web.Error(501, "no audio transcription provider configured")
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxAudioUploadSize)
	if err := request.ParseMultipartForm(maxAudioUploadSize); err != nil {
		return web.Error(400, "file too large or invalid multipart form")
	}

	file, header, err := request.FormFile("file")
	if err != nil {
		return web.Error(400, "missing file field")
	}
	defer file.Close()

	ext := strings.TrimPrefix(filepath.Ext(header.Filename), ".")
	format := strings.ToLower(ext)
	if format == "" {
		format = "webm"
	}

	language := request.FormValue("language")

	result, err := transcriber.Transcribe(request.Context(), providers.TranscribeRequest{
		Audio:    file,
		Format:   format,
		Language: language,
	})
	if err != nil {
		return web.Error(500, "transcription failed: "+err.Error())
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"text": result.Text,
	})
	return nil
}

func (self *v1Api) handleAudioSynthesize(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.Error(405, "method not allowed")
	}

	registry := self.coordinator.Providers()
	if registry == nil {
		return web.Error(500, "provider registry not available")
	}

	if _, _, ok := registry.FindSynthesizer(); !ok {
		return web.Error(501, "no audio synthesis provider configured")
	}

	var params struct {
		Text  string  `json:"text"`
		Voice string  `json:"voice"`
		Speed float64 `json:"speed"`
	}
	if err := json.NewDecoder(request.Body).Decode(&params); err != nil {
		return web.Error(400, "invalid JSON body: "+err.Error())
	}
	if params.Text == "" {
		return web.Error(400, "text is required")
	}

	token := security.NewULID()
	now := time.Now()

	self.synthesisTokensMutex.Lock()
	// Lazy cleanup: remove expired tokens.
	for key, entry := range self.synthesisTokens {
		if now.After(entry.ExpiresAt) {
			delete(self.synthesisTokens, key)
		}
	}
	self.synthesisTokens[token] = synthesisToken{
		Text:      params.Text,
		Voice:     params.Voice,
		Speed:     params.Speed,
		ExpiresAt: now.Add(60 * time.Second),
	}
	self.synthesisTokensMutex.Unlock()

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]string{"token": token})
	return nil
}

func (self *v1Api) handleAudioStream(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodGet {
		return web.Error(405, "method not allowed")
	}

	tokenValue := request.URL.Query().Get("token")
	if tokenValue == "" {
		return web.Error(400, "missing token parameter")
	}

	now := time.Now()

	self.synthesisTokensMutex.Lock()
	// Lazy cleanup: remove expired tokens.
	for key, entry := range self.synthesisTokens {
		if now.After(entry.ExpiresAt) {
			delete(self.synthesisTokens, key)
		}
	}
	entry, found := self.synthesisTokens[tokenValue]
	if found {
		delete(self.synthesisTokens, tokenValue) // Single-use.
	}
	self.synthesisTokensMutex.Unlock()

	if !found {
		return web.Error(404, "token not found or expired")
	}
	if now.After(entry.ExpiresAt) {
		return web.Error(410, "token expired")
	}

	registry := self.coordinator.Providers()
	if registry == nil {
		return web.Error(500, "provider registry not available")
	}

	synthesizer, _, ok := registry.FindSynthesizer()
	if !ok {
		return web.Error(501, "no audio synthesis provider configured")
	}

	result, err := synthesizer.Synthesize(request.Context(), providers.SynthesizeRequest{
		Text:   entry.Text,
		Voice:  entry.Voice,
		Format: "mp3",
		Speed:  entry.Speed,
	})
	if err != nil {
		return web.Error(500, "synthesis failed: "+err.Error())
	}
	defer result.Audio.Close()

	writer.Header().Set("Content-Type", result.ContentType)
	if flusher, ok := writer.(http.Flusher); ok {
		buffer := make([]byte, 4096)
		for {
			bytesRead, readError := result.Audio.Read(buffer)
			if bytesRead > 0 {
				writer.Write(buffer[:bytesRead])
				flusher.Flush()
			}
			if readError != nil {
				break
			}
		}
	} else {
		io.Copy(writer, result.Audio)
	}
	return nil
}
