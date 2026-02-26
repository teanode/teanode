package dbstore

import (
	"context"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/teanode/teanode/internal/store"
)

type Settings struct {
	Host     string
	Port     uint16
	User     string
	Password string
	Database string
	SSLMode  string
}

type databaseStore struct {
	database *gorm.DB
}

func Open(settings Settings) (store.Store, error) {
	connectionString := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		settings.Host,
		settings.Port,
		settings.User,
		settings.Password,
		settings.Database,
		settings.SSLMode,
	)
	database, openError := gorm.Open(postgres.Open(connectionString), &gorm.Config{})
	if openError != nil {
		return nil, openError
	}
	return &databaseStore{database: database}, nil
}

func (self *databaseStore) Close() error {
	if self.database == nil {
		return nil
	}
	sqlDatabase, getDatabaseError := self.database.DB()
	if getDatabaseError != nil {
		return getDatabaseError
	}
	return sqlDatabase.Close()
}

func (self *databaseStore) Transaction(ctx context.Context, run func(context.Context, store.Transaction) error) error {
	if self.database == nil {
		return store.ErrNotImplemented
	}

	databaseTransactionHandle := self.database.Begin()
	if databaseTransactionHandle.Error != nil {
		return databaseTransactionHandle.Error
	}
	transaction := &databaseTransaction{
		database:           databaseTransactionHandle,
		rootDatabaseHandle: self.database,
	}
	runError := run(ctx, transaction)
	if runError != nil {
		_ = databaseTransactionHandle.Rollback()
		return runError
	}
	if commitError := databaseTransactionHandle.Commit().Error; commitError != nil {
		return commitError
	}
	return nil
}
