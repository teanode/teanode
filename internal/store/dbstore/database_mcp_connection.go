package dbstore

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/valueor"
)

type databaseMcpConnectionRecord struct {
	ID              string     `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID          *string    `gorm:"column:user_id;type:varchar(32);index"`
	ServerName      *string    `gorm:"column:server_name;type:varchar(256)"`
	Status          *string    `gorm:"column:status;type:varchar(32)"`
	Authorization   *string    `gorm:"column:auth_value;type:text"`
	LastError       *string    `gorm:"column:last_error;type:text"`
	LastConnectedAt *time.Time `gorm:"column:last_connected_at"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt      time.Time  `gorm:"column:modified_at;not null"`
}

func (self databaseMcpConnectionRecord) TableName() string {
	return "mcp_connections"
}

func (self *databaseTransaction) ListMCPConnections(ctx context.Context, userId string, options *store.Option) ([]*models.MCPConnection, error) {
	query := self.database.Model(&databaseMcpConnectionRecord{})
	if userId != "" {
		query = query.Where("user_id = ?", userId)
	}
	query = applyOption(query.Order("id ASC"), options)
	records := make([]databaseMcpConnectionRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	connections := make([]*models.MCPConnection, 0, len(records))
	for _, record := range records {
		connections = append(connections, mcpConnectionRecordToModel(&record))
	}
	return connections, nil
}

func (self *databaseTransaction) CreateMCPConnection(ctx context.Context, connection *models.MCPConnection, options *store.Option) (*models.MCPConnection, error) {
	if connection == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToMcpConnectionRecord(connection)
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	if record.Status == nil {
		record.Status = ptrto.Value(string(models.MCPConnectionStatusPending))
	}
	now := ptrto.TimeNowInLocal()
	record.CreatedAt = *now
	record.ModifiedAt = *now
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return self.GetMCPConnection(ctx, record.ID, options)
}

func (self *databaseTransaction) GetMCPConnection(ctx context.Context, connectionId string, options *store.Option) (*models.MCPConnection, error) {
	record := &databaseMcpConnectionRecord{}
	getError := self.database.Where("id = ?", connectionId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return mcpConnectionRecordToModel(record), nil
}

func (self *databaseTransaction) GetMCPConnectionByServer(ctx context.Context, userId string, serverName string, options *store.Option) (*models.MCPConnection, error) {
	record := &databaseMcpConnectionRecord{}
	getError := self.database.Where("user_id = ? AND server_name = ?", userId, serverName).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return mcpConnectionRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyMCPConnection(ctx context.Context, connectionId string, modifier func(*models.MCPConnection) error, options *store.Option) (*models.MCPConnection, error) {
	connection, getError := self.GetMCPConnection(ctx, connectionId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(connection); modifierError != nil {
		return nil, modifierError
	}
	record := modelToMcpConnectionRecord(connection)
	record.ID = connectionId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseMcpConnectionRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"user_id":           record.UserID,
		"server_name":       record.ServerName,
		"status":            record.Status,
		"auth_value":        record.Authorization,
		"last_error":        record.LastError,
		"last_connected_at": record.LastConnectedAt,
		"modified_at":       record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetMCPConnection(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteMCPConnection(ctx context.Context, connectionId string, options *store.Option) error {
	result := self.database.Where("id = ?", connectionId).Delete(&databaseMcpConnectionRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToMcpConnectionRecord(connection *models.MCPConnection) *databaseMcpConnectionRecord {
	var lastConnectedAt *time.Time
	if connection.LastConnectedAt != nil {
		lastConnectedAtValue := connection.LastConnectedAt.UTC()
		lastConnectedAt = &lastConnectedAtValue
	}
	var status *string
	if connection.Status != nil && *connection.Status != "" {
		status = ptrto.Value(string(*connection.Status))
	}
	return &databaseMcpConnectionRecord{
		ID:              connection.ID,
		UserID:          ptrto.TrimmedString(connection.GetUserID()),
		ServerName:      ptrto.TrimmedString(connection.GetServerName()),
		Status:          status,
		Authorization:   ptrto.TrimmedString(connection.GetAuthorization()),
		LastError:       ptrto.TrimmedString(connection.GetLastError()),
		LastConnectedAt: lastConnectedAt,
	}
}

func mcpConnectionRecordToModel(record *databaseMcpConnectionRecord) *models.MCPConnection {
	var status *models.MCPConnectionStatus
	if statusValue := valueor.Zero(record.Status); statusValue != "" {
		converted := models.MCPConnectionStatus(statusValue)
		status = &converted
	}
	return &models.MCPConnection{
		ID:              record.ID,
		UserID:          ptrto.TrimmedString(valueor.Zero(record.UserID)),
		ServerName:      ptrto.TrimmedString(valueor.Zero(record.ServerName)),
		Status:          status,
		Authorization:   ptrto.TrimmedString(valueor.Zero(record.Authorization)),
		LastError:       ptrto.TrimmedString(valueor.Zero(record.LastError)),
		LastConnectedAt: record.LastConnectedAt,
		CreatedAt:       &record.CreatedAt,
		ModifiedAt:      &record.ModifiedAt,
	}
}
