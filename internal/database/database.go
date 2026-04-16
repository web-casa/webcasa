package database

import (
	"log"

	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/notify"
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
		&notify.Channel{},
	)
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// Seed default settings
	db.Where("key = ?", "auto_reload").FirstOrCreate(&model.Setting{Key: "auto_reload", Value: "true"})
	db.Where("key = ?", "server_ipv4").FirstOrCreate(&model.Setting{Key: "server_ipv4", Value: ""})
	db.Where("key = ?", "dns_verify_on_create").FirstOrCreate(&model.Setting{Key: "dns_verify_on_create", Value: "false"})
	db.Where("key = ?", "wildcard_domain").FirstOrCreate(&model.Setting{Key: "wildcard_domain", Value: ""})
	db.Where("key = ?", "wildcard_tls_mode").FirstOrCreate(&model.Setting{Key: "wildcard_tls_mode", Value: "auto"})
	db.Where("key = ?", "server_ipv6").FirstOrCreate(&model.Setting{Key: "server_ipv6", Value: ""})

	// RBAC migration: promote first admin to owner if no owner exists yet.
	var ownerCount int64
	db.Model(&model.User{}).Where("role = ?", "owner").Count(&ownerCount)
	if ownerCount == 0 {
		// Select the exact user ID first, then update by PK (safe across all SQL dialects).
		var firstAdmin model.User
		if db.Where("role = ?", "admin").Order("id ASC").First(&firstAdmin).Error == nil {
			db.Model(&model.User{}).Where("id = ?", firstAdmin.ID).Update("role", "owner")
			log.Printf("RBAC migration: promoted user '%s' (ID %d) to owner", firstAdmin.Username, firstAdmin.ID)
		} else {
			// No admin users either — promote the very first user regardless of role.
			var firstUser model.User
			if db.Order("id ASC").First(&firstUser).Error == nil {
				db.Model(&model.User{}).Where("id = ?", firstUser.ID).Update("role", "owner")
				log.Printf("RBAC migration: promoted user '%s' (ID %d) to owner (no admin found)", firstUser.Username, firstUser.ID)
			}
		}
	}

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
