package dbstore

import (
	"gorm.io/gorm"
)

type databaseTransaction struct {
	database           *gorm.DB
	rootDatabaseHandle *gorm.DB
}
