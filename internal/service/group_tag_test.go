package service

import (
	"fmt"
	"testing"

	"github.com/web-casa/webcasa/internal/model"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDBUnique creates an in-memory SQLite database with a unique name to avoid shared state
func setupTestDBUnique(t *testing.T, name string) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	err = db.AutoMigrate(
		&model.Host{},
		&model.Upstream{},
		&model.Route{},
		&model.CustomHeader{},
		&model.AccessRule{},
		&model.BasicAuth{},
		&model.AuditLog{},
		&model.Setting{},
		&model.Group{},
		&model.Tag{},
		&model.HostTag{},
	)
	if err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}
	db.Create(&model.Setting{Key: "auto_reload", Value: "false"})
	return db
}

// Feature: phase6-enhancements, Property 10: Group/Tag CRUD round-trip — For any valid Group or Tag
// (name + color), create then query should return same data; update then query should return updated
// data; delete then query should return not found.
// **Validates: Requirements 5.1, 5.2**
func TestProperty10_GroupTagCRUDRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Group CRUD round-trip
	properties.Property("group CRUD round-trip", prop.ForAll(
		func(suffix int, colorIdx int) bool {
			db := setupTestDB(t)
			svc := setupTestHostService(t, db)
			groupSvc := NewGroupService(db, nil, nil, svc)

			name := fmt.Sprintf("group-%d", suffix)
			colors := []string{"#10b981", "#ef4444", "#3b82f6", "#f59e0b", ""}
			color := colors[colorIdx%len(colors)]

			// Create
			group, err := groupSvc.Create(name, color)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}
			if group.Name != name || group.Color != color {
				return false
			}

			// Read
			fetched, err := groupSvc.Get(group.ID)
			if err != nil {
				return false
			}
			if fetched.Name != name || fetched.Color != color {
				return false
			}

			// List
			groups, err := groupSvc.List()
			if err != nil || len(groups) != 1 {
				return false
			}

			// Update
			newName := name + "-updated"
			newColor := "#000000"
			updated, err := groupSvc.Update(group.ID, newName, newColor)
			if err != nil {
				return false
			}
			if updated.Name != newName || updated.Color != newColor {
				return false
			}

			// Verify update persisted
			fetched2, err := groupSvc.Get(group.ID)
			if err != nil {
				return false
			}
			if fetched2.Name != newName || fetched2.Color != newColor {
				return false
			}

			// Delete
			if err := groupSvc.Delete(group.ID); err != nil {
				return false
			}

			// Verify deleted
			_, err = groupSvc.Get(group.ID)
			if err == nil {
				return false // should not find deleted group
			}

			return true
		},
		gen.IntRange(1, 99999),
		gen.IntRange(0, 4),
	))

	// Tag CRUD round-trip
	properties.Property("tag CRUD round-trip", prop.ForAll(
		func(suffix int, colorIdx int) bool {
			db := setupTestDB(t)
			tagSvc := NewTagService(db)

			name := fmt.Sprintf("tag-%d", suffix)
			colors := []string{"#10b981", "#ef4444", "#3b82f6", "#f59e0b", ""}
			color := colors[colorIdx%len(colors)]

			// Create
			tag, err := tagSvc.Create(name, color)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}
			if tag.Name != name || tag.Color != color {
				return false
			}

			// Read
			fetched, err := tagSvc.Get(tag.ID)
			if err != nil {
				return false
			}
			if fetched.Name != name || fetched.Color != color {
				return false
			}

			// Update
			newName := name + "-updated"
			newColor := "#ffffff"
			updated, err := tagSvc.Update(tag.ID, newName, newColor)
			if err != nil {
				return false
			}
			if updated.Name != newName || updated.Color != newColor {
				return false
			}

			// Delete
			if err := tagSvc.Delete(tag.ID); err != nil {
				return false
			}
			_, err = tagSvc.Get(tag.ID)
			if err == nil {
				return false
			}

			return true
		},
		gen.IntRange(1, 99999),
		gen.IntRange(0, 4),
	))

	// Duplicate name rejection
	properties.Property("group duplicate name rejected", prop.ForAll(
		func(suffix int) bool {
			db := setupTestDB(t)
			svc := setupTestHostService(t, db)
			groupSvc := NewGroupService(db, nil, nil, svc)

			name := fmt.Sprintf("dup-group-%d", suffix)
			_, err := groupSvc.Create(name, "#000")
			if err != nil {
				return false
			}
			_, err = groupSvc.Create(name, "#fff")
			return err != nil && err.Error() == "error.group_name_exists"
		},
		gen.IntRange(1, 99999),
	))

	properties.Property("tag duplicate name rejected", prop.ForAll(
		func(suffix int) bool {
			db := setupTestDB(t)
			tagSvc := NewTagService(db)

			name := fmt.Sprintf("dup-tag-%d", suffix)
			_, err := tagSvc.Create(name, "#000")
			if err != nil {
				return false
			}
			_, err = tagSvc.Create(name, "#fff")
			return err != nil && err.Error() == "error.tag_name_exists"
		},
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 11: Host 分组关联 — For any Host and any Group,
// assigning the Host to the Group should return correct Group info; setting group_id to nil
// should remove the association.
// **Validates: Requirements 5.3**
func TestProperty11_HostGroupAssociation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("host group association", prop.ForAll(
		func(suffix int) bool {
			db := setupTestDB(t)
			hostSvc := setupTestHostService(t, db)
			groupSvc := NewGroupService(db, nil, nil, hostSvc)

			// Create a group
			group, err := groupSvc.Create(fmt.Sprintf("grp-%d", suffix), "#10b981")
			if err != nil {
				t.Logf("Create group failed: %v", err)
				return false
			}

			// Create a host with group_id
			enabled := true
			req := &model.HostCreateRequest{
				Domain:   fmt.Sprintf("host-%d.example.com", suffix),
				HostType: "proxy",
				Enabled:  &enabled,
				GroupID:  &group.ID,
				Upstreams: []model.UpstreamInput{
					{Address: "localhost:8080", Weight: 1},
				},
			}
			host, err := hostSvc.Create(req)
			if err != nil {
				t.Logf("Create host failed: %v", err)
				return false
			}

			// Verify group association
			if host.GroupID == nil || *host.GroupID != group.ID {
				t.Logf("GroupID check failed: host.GroupID=%v, expected=%d", host.GroupID, group.ID)
				return false
			}
			if host.Group == nil || host.Group.Name != group.Name {
				t.Logf("Group preload check failed: host.Group=%v", host.Group)
				return false
			}

			// Remove group association
			updateReq := &model.HostCreateRequest{
				Domain:   host.Domain,
				HostType: "proxy",
				Enabled:  &enabled,
				GroupID:  nil,
				Upstreams: []model.UpstreamInput{
					{Address: "localhost:8080", Weight: 1},
				},
			}
			updated, err := hostSvc.Update(host.ID, updateReq)
			if err != nil {
				t.Logf("Update host failed: %v", err)
				return false
			}
			if updated.GroupID != nil {
				t.Logf("GroupID not cleared: updated.GroupID=%v", updated.GroupID)
				return false
			}

			return true
		},
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 12: Host 标签关联 — For any Host and any set of Tags,
// assigning those tags should return the exact same tag set when querying the Host.
// **Validates: Requirements 5.4**
func TestProperty12_HostTagAssociation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("host tag association", prop.ForAll(
		func(suffix int, numTags int) bool {
			db := setupTestDB(t)
			hostSvc := setupTestHostService(t, db)
			tagSvc := NewTagService(db)

			// Create tags
			var tagIDs []uint
			for i := 0; i < numTags; i++ {
				tag, err := tagSvc.Create(fmt.Sprintf("tag-%d-%d", suffix, i), "#3b82f6")
				if err != nil {
					t.Logf("Create tag failed: %v", err)
					return false
				}
				tagIDs = append(tagIDs, tag.ID)
			}

			// Create host with tags
			enabled := true
			req := &model.HostCreateRequest{
				Domain:   fmt.Sprintf("tagged-%d.example.com", suffix),
				HostType: "proxy",
				Enabled:  &enabled,
				TagIDs:   tagIDs,
				Upstreams: []model.UpstreamInput{
					{Address: "localhost:8080", Weight: 1},
				},
			}
			host, err := hostSvc.Create(req)
			if err != nil {
				t.Logf("Create host failed: %v", err)
				return false
			}

			// Verify tag count matches
			if len(host.Tags) != numTags {
				t.Logf("Expected %d tags, got %d", numTags, len(host.Tags))
				return false
			}

			// Verify all tag IDs are present
			tagIDSet := make(map[uint]bool)
			for _, tag := range host.Tags {
				tagIDSet[tag.ID] = true
			}
			for _, id := range tagIDs {
				if !tagIDSet[id] {
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 99999),
		gen.IntRange(0, 5),
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 13: Host 筛选正确性 — For any dataset with multiple Hosts
// in different Groups and Tags, filtering by Group returns only that Group's hosts; filtering by Tag
// returns only hosts with that Tag; filtering by both returns the intersection.
// **Validates: Requirements 5.5, 5.6, 5.7**
func TestProperty13_HostFilterCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("host filter correctness", prop.ForAll(
		func(suffix int) bool {
			// Use unique DB per iteration to avoid cross-contamination
			db := setupTestDBUnique(t, fmt.Sprintf("filter-%d", suffix))
			hostSvc := setupTestHostService(t, db)
			groupSvc := NewGroupService(db, nil, nil, hostSvc)
			tagSvc := NewTagService(db)

			// Create 2 groups
			groupA, _ := groupSvc.Create(fmt.Sprintf("gA-%d", suffix), "#10b981")
			groupB, _ := groupSvc.Create(fmt.Sprintf("gB-%d", suffix), "#ef4444")

			// Create 2 tags
			tagX, _ := tagSvc.Create(fmt.Sprintf("tX-%d", suffix), "#3b82f6")
			tagY, _ := tagSvc.Create(fmt.Sprintf("tY-%d", suffix), "#f59e0b")

			enabled := true
			mkReq := func(domain string, groupID *uint, tagIDs []uint) *model.HostCreateRequest {
				return &model.HostCreateRequest{
					Domain:    domain,
					HostType:  "proxy",
					Enabled:   &enabled,
					GroupID:   groupID,
					TagIDs:    tagIDs,
					Upstreams: []model.UpstreamInput{{Address: "localhost:8080", Weight: 1}},
				}
			}

			// Host1: groupA, tagX
			hostSvc.Create(mkReq(fmt.Sprintf("h1-%d.example.com", suffix), &groupA.ID, []uint{tagX.ID}))
			// Host2: groupA, tagY
			hostSvc.Create(mkReq(fmt.Sprintf("h2-%d.example.com", suffix), &groupA.ID, []uint{tagY.ID}))
			// Host3: groupB, tagX
			hostSvc.Create(mkReq(fmt.Sprintf("h3-%d.example.com", suffix), &groupB.ID, []uint{tagX.ID}))
			// Host4: groupB, tagY
			hostSvc.Create(mkReq(fmt.Sprintf("h4-%d.example.com", suffix), &groupB.ID, []uint{tagY.ID}))

			// Filter by groupA → should get 2 hosts
			hostsA, err := hostSvc.List(HostListFilter{GroupID: &groupA.ID})
			if err != nil || len(hostsA) != 2 {
				t.Logf("groupA filter: expected 2, got %d, err=%v", len(hostsA), err)
				return false
			}

			// Filter by tagX → should get 2 hosts
			hostsX, err := hostSvc.List(HostListFilter{TagID: &tagX.ID})
			if err != nil || len(hostsX) != 2 {
				t.Logf("tagX filter: expected 2, got %d, err=%v", len(hostsX), err)
				return false
			}

			// Filter by groupA + tagX → should get 1 host (intersection)
			hostsAX, err := hostSvc.List(HostListFilter{GroupID: &groupA.ID, TagID: &tagX.ID})
			if err != nil || len(hostsAX) != 1 {
				t.Logf("groupA+tagX filter: expected 1, got %d, err=%v", len(hostsAX), err)
				return false
			}

			// No filter → should get all 4
			hostsAll, err := hostSvc.List()
			if err != nil || len(hostsAll) != 4 {
				t.Logf("no filter: expected 4, got %d, err=%v", len(hostsAll), err)
				return false
			}

			return true
		},
		gen.IntRange(1, 99999),
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 14: 批量启用/禁用 — For any Group and its associated Hosts,
// batch disable should set all hosts' enabled to false; batch enable should set all to true.
// **Validates: Requirements 5.9, 5.10**
func TestProperty14_BatchEnableDisable(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("batch enable/disable", prop.ForAll(
		func(suffix int, numHosts int) bool {
			db := setupTestDB(t)
			hostSvc := setupTestHostService(t, db)
			groupSvc := NewGroupService(db, nil, nil, hostSvc)

			group, _ := groupSvc.Create(fmt.Sprintf("batch-grp-%d", suffix), "#10b981")

			enabled := true
			for i := 0; i < numHosts; i++ {
				req := &model.HostCreateRequest{
					Domain:    fmt.Sprintf("batch-%d-%d.example.com", suffix, i),
					HostType:  "proxy",
					Enabled:   &enabled,
					GroupID:   &group.ID,
					Upstreams: []model.UpstreamInput{{Address: "localhost:8080", Weight: 1}},
				}
				if _, err := hostSvc.Create(req); err != nil {
					t.Logf("Create host failed: %v", err)
					return false
				}
			}

			// Batch disable
			if err := groupSvc.BatchDisable(group.ID); err != nil {
				t.Logf("BatchDisable failed: %v", err)
				return false
			}

			hosts, _ := hostSvc.List(HostListFilter{GroupID: &group.ID})
			for _, h := range hosts {
				if boolVal(h.Enabled) {
					t.Logf("Host %s still enabled after batch disable", h.Domain)
					return false
				}
			}

			// Batch enable
			if err := groupSvc.BatchEnable(group.ID); err != nil {
				t.Logf("BatchEnable failed: %v", err)
				return false
			}

			hosts, _ = hostSvc.List(HostListFilter{GroupID: &group.ID})
			for _, h := range hosts {
				if !boolVal(h.Enabled) {
					t.Logf("Host %s still disabled after batch enable", h.Domain)
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 99999),
		gen.IntRange(1, 5),
	))

	properties.TestingRun(t)
}

// Feature: phase6-enhancements, Property 15: 删除 Group 解除关联 — For any Group with associated
// Hosts, deleting the Group should leave the Hosts intact with group_id set to NULL.
// **Validates: Requirements 5.12**
func TestProperty15_DeleteGroupUnlinksHosts(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("delete group unlinks hosts", prop.ForAll(
		func(suffix int, numHosts int) bool {
			db := setupTestDB(t)
			hostSvc := setupTestHostService(t, db)
			groupSvc := NewGroupService(db, nil, nil, hostSvc)

			group, _ := groupSvc.Create(fmt.Sprintf("del-grp-%d", suffix), "#ef4444")

			enabled := true
			var hostIDs []uint
			for i := 0; i < numHosts; i++ {
				req := &model.HostCreateRequest{
					Domain:    fmt.Sprintf("del-%d-%d.example.com", suffix, i),
					HostType:  "proxy",
					Enabled:   &enabled,
					GroupID:   &group.ID,
					Upstreams: []model.UpstreamInput{{Address: "localhost:8080", Weight: 1}},
				}
				h, err := hostSvc.Create(req)
				if err != nil {
					t.Logf("Create host failed: %v", err)
					return false
				}
				hostIDs = append(hostIDs, h.ID)
			}

			// Delete the group
			if err := groupSvc.Delete(group.ID); err != nil {
				t.Logf("Delete group failed: %v", err)
				return false
			}

			// Verify group is gone
			_, err := groupSvc.Get(group.ID)
			if err == nil {
				return false
			}

			// Verify hosts still exist with group_id = NULL
			for _, hid := range hostIDs {
				h, err := hostSvc.Get(hid)
				if err != nil {
					t.Logf("Host %d not found after group delete", hid)
					return false
				}
				if h.GroupID != nil {
					t.Logf("Host %d still has group_id=%d after group delete", hid, *h.GroupID)
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 99999),
		gen.IntRange(1, 5),
	))

	properties.TestingRun(t)
}
