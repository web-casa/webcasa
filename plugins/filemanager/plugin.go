package filemanager

import (
	"time"

	pluginpkg "github.com/web-casa/webcasa/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for File Manager + Terminal.
type Plugin struct {
	fileOps *FileOps
	termMgr *TerminalManager
	handler *Handler
	stopCh  chan struct{}
}

// New creates a new File Manager plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "filemanager",
		Name:        "File Manager",
		Version:     "1.0.0",
		Description: "File browser, online editor, and web terminal",
		Author:      "Web.Casa",
		Priority:    40,
		Icon:        "FolderOpen",
		Category:    "tool",
	}
}

// Init initialises the File Manager plugin.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	rootPath := ctx.ConfigStore.Get("root_path")
	if rootPath == "" {
		rootPath = "/"
	}

	p.fileOps = NewFileOps(rootPath)
	p.termMgr = NewTerminalManager(ctx.Logger)
	p.handler = NewHandler(p.fileOps, p.termMgr)

	r := ctx.Router

	// File operations
	r.GET("/list", p.handler.List)
	r.GET("/read", p.handler.Read)
	r.POST("/write", p.handler.Write)
	r.POST("/upload", p.handler.Upload)
	r.GET("/download", p.handler.Download)
	r.POST("/mkdir", p.handler.Mkdir)
	r.DELETE("/delete", p.handler.Delete)
	r.POST("/rename", p.handler.Rename)
	r.POST("/chmod", p.handler.Chmod)
	r.GET("/info", p.handler.Info)

	// Archive
	r.POST("/compress", p.handler.Compress)
	r.POST("/extract", p.handler.Extract)

	// Terminal
	r.GET("/terminal/ws", p.handler.TerminalWS)

	ctx.Logger.Info("File Manager plugin routes registered")
	return nil
}

// Start begins background session cleanup.
func (p *Plugin) Start() error {
	p.stopCh = make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.termMgr.CleanupStale(2 * time.Hour)
			case <-p.stopCh:
				return
			}
		}
	}()
	return nil
}

// Stop cleans up all terminal sessions.
func (p *Plugin) Stop() error {
	if p.stopCh != nil {
		close(p.stopCh)
	}
	if p.termMgr != nil {
		p.termMgr.CloseAll()
	}
	return nil
}

// FrontendManifest declares frontend routes.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "filemanager",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/files", Component: "FileManager", Menu: true, Icon: "FolderOpen", Label: "Files", LabelZh: "文件管理"},
			{Path: "/files/edit", Component: "FileEditor", Label: "File Editor", LabelZh: "文件编辑"},
			{Path: "/terminal", Component: "WebTerminal", Menu: true, Icon: "SquareTerminal", Label: "Terminal", LabelZh: "终端"},
		},
		MenuGroup: "tool",
		MenuOrder: 40,
	}
}

func init() {
	// Ensure Plugin implements both interfaces at compile time.
	var _ pluginpkg.Plugin = (*Plugin)(nil)
	var _ pluginpkg.FrontendProvider = (*Plugin)(nil)
}
