package service

import (
	"fmt"

	"github.com/web-casa/webcasa/internal/model"
	"gorm.io/gorm"
)

// TagService handles business logic for host tags
type TagService struct {
	db *gorm.DB
}

// NewTagService creates a new TagService
func NewTagService(db *gorm.DB) *TagService {
	return &TagService{db: db}
}

// List returns all tags
func (s *TagService) List() ([]model.Tag, error) {
	var tags []model.Tag
	err := s.db.Order("id ASC").Find(&tags).Error
	return tags, err
}

// Get returns a single tag by ID
func (s *TagService) Get(id uint) (*model.Tag, error) {
	var tag model.Tag
	if err := s.db.First(&tag, id).Error; err != nil {
		return nil, err
	}
	return &tag, nil
}

// Create creates a new tag
func (s *TagService) Create(name, color string) (*model.Tag, error) {
	var count int64
	s.db.Model(&model.Tag{}).Where("name = ?", name).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("error.tag_name_exists")
	}

	tag := &model.Tag{
		Name:  name,
		Color: color,
	}
	if err := s.db.Create(tag).Error; err != nil {
		return nil, fmt.Errorf("failed to create tag: %w", err)
	}
	return tag, nil
}

// Update modifies an existing tag
func (s *TagService) Update(id uint, name, color string) (*model.Tag, error) {
	tag, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	var count int64
	s.db.Model(&model.Tag{}).Where("name = ? AND id != ?", name, id).Count(&count)
	if count > 0 {
		return nil, fmt.Errorf("error.tag_name_exists")
	}

	tag.Name = name
	tag.Color = color
	if err := s.db.Save(tag).Error; err != nil {
		return nil, fmt.Errorf("failed to update tag: %w", err)
	}
	return tag, nil
}

// Delete removes a tag and cleans up host_tags associations
func (s *TagService) Delete(id uint) error {
	_, err := s.Get(id)
	if err != nil {
		return fmt.Errorf("error.tag_not_found")
	}

	// Remove host_tags associations
	s.db.Where("tag_id = ?", id).Delete(&model.HostTag{})

	if err := s.db.Delete(&model.Tag{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete tag: %w", err)
	}
	return nil
}
