package database

import (
	"log"

	"github.com/caddypanel/caddypanel/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Init initializes the SQLite database and runs auto-migration
func Init(dbPath string) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Enable WAL mode for better concurrent read performance
	sqlDB, _ := db.DB()
	sqlDB.Exec("PRAGMA journal_mode=WAL")
	sqlDB.Exec("PRAGMA foreign_keys=ON")

	// Auto-migrate all models
	err = db.AutoMigrate(
		&model.User{},
		&model.Host{},
		&model.Upstream{},
		&model.Route{},
		&model.CustomHeader{},
		&model.AccessRule{},
	)
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	log.Println("Database initialized successfully")
	return db
}
