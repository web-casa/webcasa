package service

import (
	"fmt"
	"log"

	"github.com/caddypanel/caddypanel/internal/caddy"
	"github.com/caddypanel/caddypanel/internal/config"
	"github.com/caddypanel/caddypanel/internal/model"
	"gorm.io/gorm"
)

// GroupService handles business logic for host groups
type GroupService struct {
	db       *gorm.DB
	caddyMgr *caddy.Manager
	cfg      *config.Config
	hostSvc  *HostService
}

// NewGroupService creates a new GroupService
func NewGroupService(db *gorm.DB, caddyMgr *caddy.Manager, cfg *config.Config, hostSvc *HostService) *GroupService {
	return &GroupService{db: db, caddyMgr: caddyMgr, cfg: cfg, hostSvc: hostSvc}
}

// List returns all groups
func (s *GroupService) List() ([]model.Group, error) {
	var groups []model.Group
	err := s.db.Order("id ASC").Find(&groups).Error
	return groups, err
}

// Get returns a single group by ID
func (s *GroupService) Get(id uint) (*model.Group, error) {
	var group model.Group
	if err := s.db.First(&group, id).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

// Create creates a new group
func (s *GroupService) Create(name, color string) (*model.Group, error) {
	var count int64
	s.db.Model(&model.Group{}).Where("name = ?", name).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("error.group_name_exists")
	}

	group := &model.Group{
		Name:  name,
		Color: color,
	}
	if err := s.db.Create(group).Error; err != nil {
		return nil, fmt.Errorf("failed to create group: %w", err)
	}
	return group, nil
}

// Update modifies an existing group
func (s *GroupService) Update(id uint, name, color string) (*model.Group, error) {
	group, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	var count int64
	s.db.Model(&model.Group{}).Where("name = ? AND id != ?", name, id).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("error.group_name_exists")
	}

	group.Name = name
	group.Color = color
	if err := s.db.Save(group).Error; err != nil {
		return nil, fmt.Errorf("failed to update group: %w", err)
	}
	return group, nil
}

// Delete removes a group and sets associated hosts' group_id to NULL
func (s *GroupService) Delete(id uint) error {
	_, err := s.Get(id)
	if err != nil {
		return fmt.Errorf("error.group_not_found")
	}

	// Set associated hosts' group_id to NULL
	s.db.Model(&model.Host{}).Where("group_id = ?", id).Update("group_id", nil)

	if err := s.db.Delete(&model.Group{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}
	return nil
}

// BatchEnable enables all hosts in a group and applies config
func (s *GroupService) BatchEnable(groupID uint) error {
	_, err := s.Get(groupID)
	if err != nil {
		return fmt.Errorf("error.group_not_found")
	}

	enabled := true
	if err := s.db.Model(&model.Host{}).Where("group_id = ?", groupID).Update("enabled", &enabled).Error; err != nil {
		return fmt.Errorf("failed to batch enable hosts: %w", err)
	}

	if err := s.hostSvc.ApplyConfig(); err != nil {
		log.Printf("Warning: failed to apply config after batch enable: %v", err)
	}
	return nil
}

// BatchDisable disables all hosts in a group and applies config
func (s *GroupService) BatchDisable(groupID uint) error {
	_, err := s.Get(groupID)
	if err != nil {
		return fmt.Errorf("error.group_not_found")
	}

	enabled := false
	if err := s.db.Model(&model.Host{}).Where("group_id = ?", groupID).Update("enabled", &enabled).Error; err != nil {
		return fmt.Errorf("failed to batch disable hosts: %w", err)
	}

	if err := s.hostSvc.ApplyConfig(); err != nil {
		log.Printf("Warning: failed to apply config after batch disable: %v", err)
	}
	return nil
}
