package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"time"

	"github.com/web-casa/webcasa/internal/caddy"
	"github.com/web-casa/webcasa/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
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

	// OS info via gopsutil
	sysInfo := gin.H{
		"panel_version": h.version,
		"go_version":    runtime.Version(),
		"go_os":         runtime.GOOS,
		"go_arch":       runtime.GOARCH,
	}

	if hi, err := host.Info(); err == nil {
		sysInfo["hostname"] = hi.Hostname
		sysInfo["os_name"] = hi.Platform
		sysInfo["os_version"] = hi.PlatformVersion
		sysInfo["kernel"] = hi.KernelVersion
		sysInfo["uptime"] = hi.Uptime
	}

	if cpuInfos, err := cpu.Info(); err == nil && len(cpuInfos) > 0 {
		sysInfo["cpu_model"] = cpuInfos[0].ModelName
		sysInfo["cpu_cores"] = runtime.NumCPU()
	}

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
		"system": sysInfo,
		"caddy":  caddyStatus,
	})
}

// News proxies the official WebCasa news feed.
func (h *DashboardHandler) News(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://news.web.casa/api/news", nil)
	if err != nil {
		c.JSON(http.StatusOK, []any{})
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, []any{})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		c.JSON(http.StatusOK, []any{})
		return
	}

	var news []any
	if err := json.Unmarshal(body, &news); err != nil {
		c.JSON(http.StatusOK, []any{})
		return
	}
	c.JSON(http.StatusOK, news)
}
