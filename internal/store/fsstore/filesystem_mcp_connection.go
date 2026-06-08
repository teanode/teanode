package fsstore

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

type fileSystemMcpConnectionRecord struct {
	ID                string     `yaml:"id"`
	UserID            string     `yaml:"userId"`
	ServerName        string     `yaml:"serverName"`
	Status            string     `yaml:"status,omitempty"`
	Authorization     string     `yaml:"authorization,omitempty"`
	LastError         string     `yaml:"lastError,omitempty"`
	CreatedAt         time.Time  `yaml:"createdAt"`
	ModifiedAt        time.Time  `yaml:"modifiedAt"`
	LastConnectedAt   *time.Time `yaml:"lastConnectedAt,omitempty"`
	AccessToken       string     `yaml:"accessToken,omitempty"`
	RefreshToken      string     `yaml:"refreshToken,omitempty"`
	TokenType         string     `yaml:"tokenType,omitempty"`
	TokenExpiresAt    *time.Time `yaml:"tokenExpiresAt,omitempty"`
	Scope             string     `yaml:"scope,omitempty"`
	OAuthClientID     string     `yaml:"oauthClientId,omitempty"`
	OAuthClientSecret string     `yaml:"oauthClientSecret,omitempty"`
	OAuthState        string     `yaml:"oauthState,omitempty"`
	CodeVerifier      string     `yaml:"codeVerifier,omitempty"`
	OAuthRedirectURI  string     `yaml:"oauthRedirectUri,omitempty"`
}

func (self *fileSystemTransaction) ListMCPConnections(ctx context.Context, userId string, options *store.Option) ([]*models.MCPConnection, error) {
	return self.listMcpConnections(userId, options)
}

func (self *fileSystemTransaction) CreateMCPConnection(ctx context.Context, connection *models.MCPConnection, options *store.Option) (*models.MCPConnection, error) {
	return self.createMcpConnection(connection, options)
}

func (self *fileSystemTransaction) GetMCPConnection(ctx context.Context, connectionId string, options *store.Option) (*models.MCPConnection, error) {
	return self.getMcpConnection(connectionId, options)
}

func (self *fileSystemTransaction) GetMCPConnectionByServer(ctx context.Context, userId string, serverName string, options *store.Option) (*models.MCPConnection, error) {
	return self.getMcpConnectionByServer(userId, serverName, options)
}

func (self *fileSystemTransaction) ModifyMCPConnection(ctx context.Context, connectionId string, modifier func(*models.MCPConnection) error, options *store.Option) (*models.MCPConnection, error) {
	return self.modifyMcpConnection(ctx, connectionId, modifier, options)
}

func (self *fileSystemTransaction) DeleteMCPConnection(ctx context.Context, connectionId string, options *store.Option) error {
	return self.deleteMcpConnection(connectionId, options)
}

func (self *fileSystemTransaction) listMcpConnections(userId string, options *store.Option) ([]*models.MCPConnection, error) {
	entries, readError := os.ReadDir(self.userMcpConnectionsDirectory(userId))
	if os.IsNotExist(readError) {
		return []*models.MCPConnection{}, nil
	}
	if readError != nil {
		return nil, readError
	}
	connections := make([]*models.MCPConnection, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		connectionId := strings.TrimSuffix(entry.Name(), ".yaml")
		record, loadError := self.readMcpConnectionRecord(userId, connectionId)
		if loadError != nil {
			continue
		}
		connection := mcpConnectionRecordToModel(record)
		connections = append(connections, &connection)
	}
	return applyOffsetLimit(connections, options), nil
}

func (self *fileSystemTransaction) createMcpConnection(connection *models.MCPConnection, options *store.Option) (*models.MCPConnection, error) {
	if connection == nil || connection.UserID == nil || strings.TrimSpace(*connection.UserID) == "" {
		return nil, fmt.Errorf("fsstore: mcp connection userId is required")
	}
	if strings.TrimSpace(connection.GetServerName()) == "" {
		return nil, fmt.Errorf("fsstore: mcp connection serverName is required")
	}
	userId := *connection.UserID
	connectionId := connection.ID
	if connectionId == "" {
		connectionId = security.NewULID()
	}
	now := time.Now()
	status := string(connection.GetStatus())
	if status == "" {
		status = string(models.MCPConnectionStatusPending)
	}
	record := fileSystemMcpConnectionRecord{
		ID:                connectionId,
		UserID:            userId,
		ServerName:        connection.GetServerName(),
		Status:            status,
		Authorization:     connection.GetAuthorization(),
		LastError:         connection.GetLastError(),
		CreatedAt:         now,
		ModifiedAt:        now,
		LastConnectedAt:   connection.LastConnectedAt,
		AccessToken:       connection.GetAccessToken(),
		RefreshToken:      connection.GetRefreshToken(),
		TokenType:         connection.GetTokenType(),
		TokenExpiresAt:    connection.TokenExpiresAt,
		Scope:             connection.GetScope(),
		OAuthClientID:     connection.GetOAuthClientID(),
		OAuthClientSecret: connection.GetOAuthClientSecret(),
		OAuthState:        connection.GetOAuthState(),
		CodeVerifier:      connection.GetCodeVerifier(),
		OAuthRedirectURI:  connection.GetOAuthRedirectURI(),
	}
	if err := self.writeMcpConnectionRecord(userId, record); err != nil {
		return nil, err
	}
	result := mcpConnectionRecordToModel(record)
	return &result, nil
}

func (self *fileSystemTransaction) getMcpConnection(connectionId string, options *store.Option) (*models.MCPConnection, error) {
	userRecords, err := self.listUserRecords()
	if err != nil {
		return nil, err
	}
	for _, userRecord := range userRecords {
		record, readError := self.readMcpConnectionRecord(userRecord.ID, connectionId)
		if readError != nil {
			continue
		}
		result := mcpConnectionRecordToModel(record)
		return &result, nil
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) getMcpConnectionByServer(userId string, serverName string, options *store.Option) (*models.MCPConnection, error) {
	connections, err := self.listMcpConnections(userId, nil)
	if err != nil {
		return nil, err
	}
	for _, connection := range connections {
		if connection.GetServerName() == serverName {
			return connection, nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) modifyMcpConnection(ctx context.Context, connectionId string, modifier func(*models.MCPConnection) error, options *store.Option) (*models.MCPConnection, error) {
	connection, err := self.GetMCPConnection(ctx, connectionId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(connection); err != nil {
		return nil, err
	}
	userId := connection.GetUserID()
	record, err := self.readMcpConnectionRecord(userId, connectionId)
	if err != nil {
		return nil, err
	}
	record.ServerName = connection.GetServerName()
	record.Status = string(connection.GetStatus())
	record.Authorization = connection.GetAuthorization()
	record.LastError = connection.GetLastError()
	record.LastConnectedAt = connection.LastConnectedAt
	record.AccessToken = connection.GetAccessToken()
	record.RefreshToken = connection.GetRefreshToken()
	record.TokenType = connection.GetTokenType()
	record.TokenExpiresAt = connection.TokenExpiresAt
	record.Scope = connection.GetScope()
	record.OAuthClientID = connection.GetOAuthClientID()
	record.OAuthClientSecret = connection.GetOAuthClientSecret()
	record.OAuthState = connection.GetOAuthState()
	record.CodeVerifier = connection.GetCodeVerifier()
	record.OAuthRedirectURI = connection.GetOAuthRedirectURI()
	record.ModifiedAt = time.Now()
	if err := self.writeMcpConnectionRecord(userId, record); err != nil {
		return nil, err
	}
	result := mcpConnectionRecordToModel(record)
	return &result, nil
}

func (self *fileSystemTransaction) deleteMcpConnection(connectionId string, options *store.Option) error {
	userRecords, err := self.listUserRecords()
	if err != nil {
		return err
	}
	for _, userRecord := range userRecords {
		connectionPath := self.userMcpConnectionFilename(userRecord.ID, connectionId)
		if _, statError := os.Stat(connectionPath); statError == nil {
			return trash.Move(connectionPath, self.trashDirectory())
		}
	}
	return store.ErrNotFound
}

func (self *fileSystemTransaction) readMcpConnectionRecord(userId, connectionId string) (fileSystemMcpConnectionRecord, error) {
	data, readError := os.ReadFile(self.userMcpConnectionFilename(userId, connectionId))
	if readError != nil {
		return fileSystemMcpConnectionRecord{}, readError
	}
	record := fileSystemMcpConnectionRecord{}
	if unmarshalError := yaml.Unmarshal(data, &record); unmarshalError != nil {
		return fileSystemMcpConnectionRecord{}, unmarshalError
	}
	return record, nil
}

func (self *fileSystemTransaction) writeMcpConnectionRecord(userId string, record fileSystemMcpConnectionRecord) error {
	if record.ID == "" {
		return fmt.Errorf("fsstore: mcp connection ID is required")
	}
	directory := self.userMcpConnectionsDirectory(userId)
	if makeDirectoryError := os.MkdirAll(directory, 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	return writeYamlFile(self.userMcpConnectionFilename(userId, record.ID), record)
}

func mcpConnectionRecordToModel(record fileSystemMcpConnectionRecord) models.MCPConnection {
	createdAt := record.CreatedAt
	modifiedAt := record.ModifiedAt
	if modifiedAt.IsZero() {
		modifiedAt = createdAt
	}
	return models.MCPConnection{
		ID:                record.ID,
		UserID:            ptrto.TrimmedString(record.UserID),
		ServerName:        ptrto.TrimmedString(record.ServerName),
		Status:            mcpConnectionStatusPointer(record.Status),
		Authorization:     ptrto.TrimmedString(record.Authorization),
		LastError:         ptrto.TrimmedString(record.LastError),
		CreatedAt:         &createdAt,
		ModifiedAt:        &modifiedAt,
		LastConnectedAt:   record.LastConnectedAt,
		AccessToken:       ptrto.TrimmedString(record.AccessToken),
		RefreshToken:      ptrto.TrimmedString(record.RefreshToken),
		TokenType:         ptrto.TrimmedString(record.TokenType),
		TokenExpiresAt:    record.TokenExpiresAt,
		Scope:             ptrto.TrimmedString(record.Scope),
		OAuthClientID:     ptrto.TrimmedString(record.OAuthClientID),
		OAuthClientSecret: ptrto.TrimmedString(record.OAuthClientSecret),
		OAuthState:        ptrto.TrimmedString(record.OAuthState),
		CodeVerifier:      ptrto.TrimmedString(record.CodeVerifier),
		OAuthRedirectURI:  ptrto.TrimmedString(record.OAuthRedirectURI),
	}
}

func mcpConnectionStatusPointer(value string) *models.MCPConnectionStatus {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	status := models.MCPConnectionStatus(trimmed)
	return &status
}
