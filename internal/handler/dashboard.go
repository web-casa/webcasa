package handler

import (
	"net/http"
	"runtime"

	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/service"
	"github.com/gin-gonic/gin"
)

// DashboardHandler handles dashboard statistics
type DashboardHandler struct {
	hostSvc  *service.HostService
	caddyMgr *caddy.Manager
	version  string
}

func NewDashboardHandler(hostSvc *service.HostService, caddyMgr *caddy.Manager, version string) *DashboardHandler {
	return &DashboardHandler{hostSvc: hostSvc, caddyMgr: caddyMgr, version: version}
}

// Stats returns comprehensive dashboard statistics
func (h *DashboardHandler) Stats(c *gin.Context) {
	hosts, err := h.hostSvc.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var (
		totalHosts    = len(hosts)
		activeHosts   int
		proxyHosts    int
		redirectHosts int
		tlsAuto       int
		tlsCustom     int
		tlsNone       int
		withAuth      int
	)

	for _, host := range hosts {
		enabled := host.Enabled == nil || *host.Enabled
		if enabled {
			activeHosts++
		}

		switch host.HostType {
		case "redirect":
			redirectHosts++
		default:
			proxyHosts++
		}

		tlsEnabled := host.TLSEnabled == nil || *host.TLSEnabled
		if tlsEnabled {
			if host.CustomCertPath != "" {
				tlsCustom++
			} else {
				tlsAuto++
			}
		} else {
			tlsNone++
		}

		if len(host.BasicAuths) > 0 {
			withAuth++
		}
	}

	// Caddy info
	caddyStatus := h.caddyMgr.Status()

	c.JSON(http.StatusOK, gin.H{
		"hosts": gin.H{
			"total":    totalHosts,
			"active":   activeHosts,
			"disabled": totalHosts - activeHosts,
			"proxy":    proxyHosts,
			"redirect": redirectHosts,
		},
		"tls": gin.H{
			"auto":   tlsAuto,
			"custom": tlsCustom,
			"none":   tlsNone,
		},
		"security": gin.H{
			"with_auth": withAuth,
		},
		"system": gin.H{
			"panel_version": h.version,
			"go_version":    runtime.Version(),
			"go_os":         runtime.GOOS,
			"go_arch":       runtime.GOARCH,
		},
		"caddy": caddyStatus,
	})
}
