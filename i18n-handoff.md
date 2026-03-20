# i18n 国际化状态

## 框架

- **库**: react-i18next
- **配置**: `web/src/i18n.js`
- **翻译文件**: `web/src/locales/en.json` / `zh.json`
- **默认语言**: English
- **Fallback**: English
- **语言检测**: 浏览器语言自动检测

## 翻译覆盖

### 已完成

- 所有核心页面已完成 i18n（Dashboard、HostList、Settings、Login 等）
- 12 个插件的名称、描述、分类标签（`plugins.names.*` / `plugins.descriptions.*` / `plugins.categories.*`）
- 侧边栏导航（Layout.jsx）
- 插件管理页面（PluginsPage.jsx）
- 插件前端 Manifest 支持 `label` + `label_zh` 双语标签

### 翻译 Key 命名空间

| 命名空间 | 说明 |
|----------|------|
| `common` | 通用词汇（保存、取消、删除等） |
| `nav` | 导航菜单 |
| `dashboard` | 仪表盘 |
| `hosts` | 站点管理 |
| `settings` | 系统设置 |
| `deploy` | 项目部署 |
| `docker` | Docker 管理 |
| `database` | 数据库管理 |
| `monitoring` | 系统监控 |
| `backup` | 备份管理 |
| `appstore` | 应用商店 |
| `mcp` | MCP 服务 |
| `plugins` | 插件管理 |
| `notify` | 通知 |
| `ai` | AI 助手 |
| `firewall` | 防火墙 |
| `cronjob` | 定时任务 |
| `php` | PHP 管理 |

### 添加新翻译的规范

1. 同时在 `en.json` 和 `zh.json` 中添加 key
2. 在组件中使用 `const { t } = useTranslation()` + `t('namespace.key')`
3. 对于可能不存在的 key，使用 `t('key', { defaultValue: 'fallback' })`
4. 插件名称翻译统一放在 `plugins.names.{pluginId}`
