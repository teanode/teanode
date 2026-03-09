package dbstore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/teanode/teanode/internal/store/dbstore/dbmigrations"
	"gorm.io/gorm"
)

type databaseMigrationRecord struct {
	ID         string    `gorm:"column:id;type:varchar(256);primaryKey"`
	MigratedAt time.Time `gorm:"column:migrated_at;not null"`
	ReverseSQL string    `gorm:"column:reverse_sql;type:text"`
}

func (self databaseMigrationRecord) TableName() string {
	return "migrations"
}

func (self *databaseStore) Migrate(ctx context.Context) error {
	if self.database == nil {
		return fmt.Errorf("database is not opened")
	}
	if err := self.database.AutoMigrate(&databaseMigrationRecord{}); err != nil {
		return err
	}

	existingRecords := make([]databaseMigrationRecord, 0)
	if err := self.database.Find(&existingRecords).Error; err != nil {
		return err
	}
	existingRecordById := make(map[string]databaseMigrationRecord, len(existingRecords))
	for _, record := range existingRecords {
		existingRecordById[record.ID] = record
	}

	currentMigrations := dbmigrations.Migrations()
	currentMigrationIds := make(map[string]struct{}, len(currentMigrations))
	for _, migration := range currentMigrations {
		currentMigrationIds[migration.ID] = struct{}{}
	}

	unknownMigrationIds := make([]string, 0)
	for migrationId := range existingRecordById {
		if _, ok := currentMigrationIds[migrationId]; !ok {
			unknownMigrationIds = append(unknownMigrationIds, migrationId)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(unknownMigrationIds)))
	for _, migrationId := range unknownMigrationIds {
		record := existingRecordById[migrationId]
		if record.ReverseSQL == "" {
			return fmt.Errorf("missing reverse sql for migration %s", migrationId)
		}
		if err := self.database.Transaction(func(transaction *gorm.DB) error {
			if err := transaction.Exec(record.ReverseSQL).Error; err != nil {
				return err
			}
			if err := transaction.Where("id = ?", migrationId).Delete(&databaseMigrationRecord{}).Error; err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
	}

	for _, migration := range currentMigrations {
		if _, ok := existingRecordById[migration.ID]; ok {
			continue
		}
		if err := self.database.Transaction(func(transaction *gorm.DB) error {
			if migration.SQL != "" {
				if err := transaction.Exec(migration.SQL).Error; err != nil {
					return err
				}
			}
			return transaction.Create(&databaseMigrationRecord{
				ID:         migration.ID,
				MigratedAt: time.Now().In(time.Local),
				ReverseSQL: migration.ReverseSQL,
			}).Error
		}); err != nil {
			return err
		}
	}

	return nil
}
