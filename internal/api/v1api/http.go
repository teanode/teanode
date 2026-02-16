package v1api

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/web"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return true },
}

func (self *API) handleHealth(writer http.ResponseWriter, request *http.Request) error {
	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(`{"status":"ok"}`))
	return nil
}

func (self *API) handleMedia(writer http.ResponseWriter, request *http.Request) error {
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

func (self *API) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	connection, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("websocket upgrade error: %v", err)
		return
	}
	webSocketConnection := newWebSocketConnection(connection, self)
	webSocketConnection.serve()
}
