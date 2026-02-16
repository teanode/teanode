package v1api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/media"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return true },
}

func (self *API) handleHealth(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(`{"status":"ok"}`))
}

func (self *API) handleMedia(writer http.ResponseWriter, request *http.Request) {
	mediaId := mux.Vars(request)["id"]
	if mediaId == "" {
		http.Error(writer, "missing media id", http.StatusBadRequest)
		return
	}
	data, format, err := self.gateway.MediaStore().Load(mediaId)
	if err != nil {
		http.Error(writer, "not found", http.StatusNotFound)
		return
	}
	writer.Header().Set("Content-Type", media.MimeType(format))
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writer.Write(data)
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
