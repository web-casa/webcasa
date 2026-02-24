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
		&model.BasicAuth{},
		&model.AuditLog{},
		&model.DnsProvider{},
		&model.Setting{},
		&model.Certificate{},
		&model.Group{},
		&model.Tag{},
		&model.HostTag{},
		&model.Template{},
	)
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// Seed default settings
	db.Where("key = ?", "auto_reload").FirstOrCreate(&model.Setting{Key: "auto_reload", Value: "true"})
	db.Where("key = ?", "server_ipv4").FirstOrCreate(&model.Setting{Key: "server_ipv4", Value: ""})
	db.Where("key = ?", "server_ipv6").FirstOrCreate(&model.Setting{Key: "server_ipv6", Value: ""})

	log.Println("Database initialized successfully")
	return db
}

// SeedTemplatePresets seeds preset templates if the templates table is empty.
// This is called from main.go after TemplateService is initialized.
func SeedTemplatePresets(db *gorm.DB, seedFunc func()) {
	var count int64
	db.Model(&model.Template{}).Count(&count)
	if count == 0 {
		seedFunc()
	}
}
