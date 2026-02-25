package dbstore

import (
	"context"
	"gorm.io/gorm"
)

type databaseTransaction struct {
	database           *gorm.DB
	rootDatabaseHandle *gorm.DB
	finalized          bool
}

func (self *databaseTransaction) Commit(ctx context.Context) error {
	if self.finalized {
		return nil
	}
	if self.database == nil {
		self.finalized = true
		return nil
	}
	commitError := self.database.Commit().Error
	if commitError != nil {
		return commitError
	}
	self.finalized = true
	return nil
}
