package v1api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
	_ "image/gif"
	_ "image/png"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return false },
}

func splitHostPortDefault(rawHost string, tls bool) (string, string) {
	host, port, err := net.SplitHostPort(rawHost)
	if err == nil {
		return strings.ToLower(host), port
	}
	defaultPort := "80"
	if tls {
		defaultPort = "443"
	}
	return strings.ToLower(strings.TrimSpace(rawHost)), defaultPort
}

func sameOriginHost(leftHost string, leftTLS bool, rightHost string, rightTLS bool) bool {
	leftName, leftPort := splitHostPortDefault(leftHost, leftTLS)
	rightName, rightPort := splitHostPortDefault(rightHost, rightTLS)
	return leftName == rightName && leftPort == rightPort
}

func (self *v1Api) isWebSocketOriginAllowed(request *http.Request) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	originUrl, err := url.Parse(origin)
	if err != nil || originUrl.Host == "" {
		return false
	}
	originTLS := strings.EqualFold(originUrl.Scheme, "https")
	requestTLS := request.TLS != nil
	if sameOriginHost(originUrl.Host, originTLS, request.Host, requestTLS) {
		return true
	}

	publicUrl := strings.TrimSpace(self.gateway.Config().Gateway.PublicURL)
	if publicUrl == "" {
		return false
	}
	parsedPublicUrl, err := url.Parse(publicUrl)
	if err != nil || parsedPublicUrl.Host == "" {
		return false
	}
	publicTLS := strings.EqualFold(parsedPublicUrl.Scheme, "https")
	return sameOriginHost(originUrl.Host, originTLS, parsedPublicUrl.Host, publicTLS)
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
const maxAvatarUploadSize = 10 << 20

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

func processAvatarImage(data []byte) ([]byte, string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	size := width
	if height < size {
		size = height
	}
	startX := bounds.Min.X + (width-size)/2
	startY := bounds.Min.Y + (height-size)/2

	square := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(square, square.Bounds(), img, image.Point{X: startX, Y: startY}, draw.Src)

	const avatarSize = 256
	resized := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	scaleNearestNeighbor(resized, square)

	var output bytes.Buffer
	if err := jpeg.Encode(&output, resized, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	return output.Bytes(), "jpeg", nil
}

func scaleNearestNeighbor(destination *image.RGBA, source *image.RGBA) {
	dstBounds := destination.Bounds()
	srcBounds := source.Bounds()
	dstWidth := dstBounds.Dx()
	dstHeight := dstBounds.Dy()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()
	if dstWidth <= 0 || dstHeight <= 0 || srcWidth <= 0 || srcHeight <= 0 {
		return
	}
	for y := 0; y < dstHeight; y++ {
		srcY := y * srcHeight / dstHeight
		for x := 0; x < dstWidth; x++ {
			srcX := x * srcWidth / dstWidth
			destination.Set(x, y, source.At(srcX, srcY))
		}
	}
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

	registry := self.gateway.ProviderRegistry()
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

func (self *v1Api) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	requestUpgrader := upgrader
	requestUpgrader.CheckOrigin = func(request *http.Request) bool {
		return self.isWebSocketOriginAllowed(request)
	}
	connection, err := requestUpgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("websocket upgrade error: %v", err)
		return
	}
	var sessionId string
	if cookie, err := request.Cookie("session"); err == nil {
		sessionId = cookie.Value
	}
	userId := ""
	if userContext := gw.UserFromContext(request.Context()); userContext != nil {
		userId = userContext.UserID
		if userContext.SessionID != "" {
			sessionId = userContext.SessionID
		}
	}
	webSocketConnection := newWebSocketConnection(connection, self, sessionId, userId)
	webSocketConnection.serve()
}
