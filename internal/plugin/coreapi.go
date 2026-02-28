package plugin

import (
	"fmt"

	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/model"
	"github.com/web-casa/webcasa/internal/service"
	"gorm.io/gorm"
)

// coreAPIImpl implements CoreAPI by delegating to existing services.
type coreAPIImpl struct {
	db       *gorm.DB
	hostSvc  *service.HostService
	caddyMgr *caddy.Manager
}

// NewCoreAPI creates a CoreAPI backed by the given services.
func NewCoreAPI(db *gorm.DB, hostSvc *service.HostService, caddyMgr *caddy.Manager) CoreAPI {
	return &coreAPIImpl{
		db:       db,
		hostSvc:  hostSvc,
		caddyMgr: caddyMgr,
	}
}

func (a *coreAPIImpl) CreateHost(req CreateHostRequest) (uint, error) {
	tlsEnabled := req.TLSEnabled
	httpRedirect := req.HTTPRedirect
	ws := req.WebSocket

	hostReq := &model.HostCreateRequest{
		Domain:       req.Domain,
		HostType:     "proxy",
		Enabled:      boolPtr(true),
		TLSEnabled:   &tlsEnabled,
		HTTPRedirect: &httpRedirect,
		WebSocket:    &ws,
		Upstreams: []model.UpstreamInput{
			{Address: req.UpstreamAddr, Weight: 1},
		},
	}

	host, err := a.hostSvc.Create(hostReq)
	if err != nil {
		return 0, fmt.Errorf("create host: %w", err)
	}
	return host.ID, nil
}

func (a *coreAPIImpl) DeleteHost(id uint) error {
	return a.hostSvc.Delete(id)
}

func (a *coreAPIImpl) ReloadCaddy() error {
	return a.caddyMgr.Reload()
}

func (a *coreAPIImpl) GetSetting(key string) (string, error) {
	var s model.Setting
	if err := a.db.Where("key = ?", key).First(&s).Error; err != nil {
		return "", err
	}
	return s.Value, nil
}

func (a *coreAPIImpl) SetSetting(key, value string) error {
	return a.db.Where("key = ?", key).
		Assign(model.Setting{Key: key, Value: value}).
		FirstOrCreate(&model.Setting{}).Error
}

func (a *coreAPIImpl) GetDB() *gorm.DB {
	return a.db
}

func boolPtr(v bool) *bool {
	return &v
}
