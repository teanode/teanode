package db

import (
	"fmt"
	"sync"

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
	mutex    sync.Mutex
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

func (self *databaseStore) Transaction(run func(store.Transaction) error) error {
	if self.database == nil {
		return store.ErrNotImplemented
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()

	databaseTransactionHandle := self.database.Begin()
	if databaseTransactionHandle.Error != nil {
		return databaseTransactionHandle.Error
	}
	transaction := &databaseTransaction{
		database:           databaseTransactionHandle,
		rootDatabaseHandle: self.database,
	}
	runError := run(transaction)
	if runError != nil {
		if !transaction.finalized {
			_ = databaseTransactionHandle.Rollback()
			transaction.finalized = true
		}
		return runError
	}
	if transaction.finalized {
		return nil
	}
	if commitError := databaseTransactionHandle.Commit().Error; commitError != nil {
		return commitError
	}
	transaction.finalized = true
	return nil
}
