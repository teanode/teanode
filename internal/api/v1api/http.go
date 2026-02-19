package v1api

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/web"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return true },
}

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
	mediaFile, err := self.gateway.MediaStore().Open(mediaId)
	if err != nil {
		return web.ErrNotFound
	}
	defer mediaFile.File.Close()
	writer.Header().Set("Content-Type", media.MimeType(mediaFile.Format))
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(writer, request, "", time.Time{}, mediaFile.File)
	return nil
}

const maxUploadSize = 50 << 20 // 50 MB

func (self *v1Api) handleMediaUpload(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.Error(405, "method not allowed")
	}

	mediaStore := self.gateway.MediaStore()
	if mediaStore == nil {
		return web.Error(500, "media store not available")
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

	data, err := io.ReadAll(file)
	if err != nil {
		return web.Error(400, "failed to read file")
	}

	// Determine format from file extension, falling back to Content-Type.
	filename := header.Filename
	ext := strings.TrimPrefix(filepath.Ext(filename), ".")
	format := strings.ToLower(ext)
	if format == "" {
		format = media.FormatFromMimeType(header.Header.Get("Content-Type"))
	}
	if format == "" {
		format = "bin"
	}

	saved, err := mediaStore.Save(data, format, media.SaveOptions{
		SourceType:   "upload",
		OriginalName: filename,
	})
	if err != nil {
		return web.Error(500, "failed to save file: "+err.Error())
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"mediaId":  saved.MediaID,
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

	registry := self.gateway.ProviderRegistry()
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

	registry := self.gateway.ProviderRegistry()
	if registry == nil {
		return web.Error(500, "provider registry not available")
	}

	synthesizer, _, ok := registry.FindSynthesizer()
	if !ok {
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

	result, err := synthesizer.Synthesize(request.Context(), providers.SynthesizeRequest{
		Text:   params.Text,
		Voice:  params.Voice,
		Format: "mp3",
		Speed:  params.Speed,
	})
	if err != nil {
		return web.Error(500, "synthesis failed: "+err.Error())
	}
	defer result.Audio.Close()

	writer.Header().Set("Content-Type", result.ContentType)
	io.Copy(writer, result.Audio)
	return nil
}

func (self *v1Api) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	connection, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("websocket upgrade error: %v", err)
		return
	}
	var sessionId string
	if cookie, err := request.Cookie("session"); err == nil {
		sessionId = cookie.Value
	}
	webSocketConnection := newWebSocketConnection(connection, self, sessionId)
	webSocketConnection.serve()
}
