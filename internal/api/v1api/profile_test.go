package v1api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
)

const testProfileUserId = "user-1"

type userProfileFixture struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	AvatarMediaID string `json:"avatarMediaId,omitempty"`
}

func setupProfileStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store backend: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return openedStore
}

func seedProfile(t *testing.T, openedStore store.Store, userId string, profile *userProfileFixture) {
	t.Helper()
	seedError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, getError := transaction.GetUser(context.Background(), userId, nil)
		if getError == nil {
			_, modifyError := transaction.ModifyUser(context.Background(), userId, func(user *models.User) error {
				name := profile.Name
				description := profile.Description
				avatarMediaId := profile.AvatarMediaID
				user.Username = ptrto.Value(name)
				user.Description = ptrto.Value(description)
				user.AvatarMediaID = ptrto.Value(avatarMediaId)
				return nil
			}, nil)
			return modifyError
		}
		admin := false
		name := profile.Name
		description := profile.Description
		avatarMediaId := profile.AvatarMediaID
		_, createError := transaction.CreateUser(context.Background(), &models.User{
			ID:            userId,
			Username:      ptrto.Value(name),
			Description:   ptrto.Value(description),
			AvatarMediaID: ptrto.Value(avatarMediaId),
			Admin:         &admin,
		}, nil, nil)
		return createError
	})
	if seedError != nil {
		t.Fatalf("seeding profile: %v", seedError)
	}
}

func loadProfileFromStore(t *testing.T, openedStore store.Store, userId string) userProfileFixture {
	t.Helper()
	result := userProfileFixture{}
	loadError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		user, getError := transaction.GetUser(context.Background(), userId, nil)
		if getError != nil {
			return getError
		}
		result = userProfileFixture{
			Name:          user.GetUsername(),
			Description:   user.GetDescription(),
			AvatarMediaID: user.GetAvatarMediaID(),
		}
		return nil
	})
	if loadError != nil {
		t.Fatalf("loading profile from store: %v", loadError)
	}
	return result
}

func newTestApi(t *testing.T, openedStore store.Store) *v1Api {
	t.Helper()
	return New(
		gw.New(
			store.ContextWithStore(context.Background(), openedStore),
			&models.Configuration{},
			agents.NewAgentRegistry(store.ContextWithStore(context.Background(), openedStore)),
			nil,
			nil,
			nil,
		),
	)
}

func withProfileUser(request *http.Request, openedStore store.Store) *http.Request {
	contextWithUser := models.ContextWithUserSessionToken(
		request.Context(),
		&models.User{ID: testProfileUserId},
		nil,
		nil,
	)
	return request.WithContext(store.ContextWithStore(contextWithUser, openedStore))
}

func newRPCWebSocketPair(t *testing.T, api *v1Api, openedStore store.Store) (*webSocketConnection, *websocket.Conn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}
	serverConnectionChannel := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, upgradeError := upgrader.Upgrade(writer, request, nil)
		if upgradeError != nil {
			t.Errorf("failed to upgrade websocket: %v", upgradeError)
			return
		}
		serverConnectionChannel <- connection
	}))
	webSocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConnection, _, dialError := websocket.DefaultDialer.Dial(webSocketURL, nil)
	if dialError != nil {
		server.Close()
		t.Fatalf("failed to dial websocket: %v", dialError)
	}
	serverConnection := <-serverConnectionChannel
	requestContext := store.ContextWithStore(context.Background(), openedStore)
	requestContext = models.ContextWithUserSessionToken(
		requestContext,
		&models.User{ID: testProfileUserId},
		&models.Session{ID: "test-session"},
		nil,
	)
	connection := newWebSocketConnection(serverConnection, api, requestContext)
	cleanup := func() {
		_ = clientConnection.Close()
		_ = serverConnection.Close()
		server.Close()
	}
	return connection, clientConnection, cleanup
}

func readRPCResponse(t *testing.T, connection *websocket.Conn) responseFrame {
	t.Helper()
	if deadlineError := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); deadlineError != nil {
		t.Fatalf("failed to set read deadline: %v", deadlineError)
	}
	var response responseFrame
	if readError := connection.ReadJSON(&response); readError != nil {
		t.Fatalf("failed to read rpc response: %v", readError)
	}
	return response
}

type rpcProfileResponsePayload struct {
	Name          string `json:"name"`
	AvatarMediaID string `json:"avatarMediaId"`
}

func decodeRPCProfilePayload(t *testing.T, payload interface{}) rpcProfileResponsePayload {
	t.Helper()
	raw, marshalError := json.Marshal(payload)
	if marshalError != nil {
		t.Fatalf("failed to marshal payload: %v", marshalError)
	}
	var decoded rpcProfileResponsePayload
	if unmarshalError := json.Unmarshal(raw, &decoded); unmarshalError != nil {
		t.Fatalf("failed to decode payload: %v", unmarshalError)
	}
	return decoded
}

func TestWebSocketProfileRPCMethods(t *testing.T) {
	openedStore := setupProfileStore(t)
	seedProfile(t, openedStore, testProfileUserId, &userProfileFixture{
		Name:          "Disk Name",
		AvatarMediaID: "avatar_initial",
	})

	api := newTestApi(t, openedStore)
	connection, clientConnection, cleanup := newRPCWebSocketPair(t, api, openedStore)
	defer cleanup()

	t.Run("profile.get", func(t *testing.T) {
		connection.dispatch(requestFrame{Type: "req", ID: "1", Method: "profile.get"})
		response := readRPCResponse(t, clientConnection)
		if !response.OK {
			t.Fatalf("response ok = false, error = %+v", response.Error)
		}
		payload := decodeRPCProfilePayload(t, response.Payload)
		if payload.Name != "Disk Name" || payload.AvatarMediaID != "avatar_initial" {
			t.Fatalf("payload = %+v", payload)
		}
	})

	t.Run("profile.update", func(t *testing.T) {
		connection.dispatch(requestFrame{
			Type:   "req",
			ID:     "2",
			Method: "profile.update",
			Params: json.RawMessage("{\"name\":\"  Updated Name  \"}"),
		})
		response := readRPCResponse(t, clientConnection)
		if !response.OK {
			t.Fatalf("response ok = false, error = %+v", response.Error)
		}
		payload := decodeRPCProfilePayload(t, response.Payload)
		if payload.Name != "Updated Name" || payload.AvatarMediaID != "avatar_initial" {
			t.Fatalf("payload = %+v", payload)
		}

		persisted := loadProfileFromStore(t, openedStore, testProfileUserId)
		if persisted.Name != "Updated Name" || persisted.AvatarMediaID != "avatar_initial" {
			t.Fatalf("persisted profile = %+v", persisted)
		}
	})

	t.Run("profile.avatar.remove", func(t *testing.T) {
		connection.dispatch(requestFrame{
			Type:   "req",
			ID:     "3",
			Method: "profile.avatar.remove",
			Params: json.RawMessage(`{}`),
		})
		response := readRPCResponse(t, clientConnection)
		if !response.OK {
			t.Fatalf("response ok = false, error = %+v", response.Error)
		}
		payload := decodeRPCProfilePayload(t, response.Payload)
		if payload.AvatarMediaID != "" {
			t.Fatalf("avatarMediaId = %q, want empty", payload.AvatarMediaID)
		}

		persisted := loadProfileFromStore(t, openedStore, testProfileUserId)
		if persisted.AvatarMediaID != "" {
			t.Fatalf("persisted avatarMediaId = %q, want empty", persisted.AvatarMediaID)
		}
	})
}
