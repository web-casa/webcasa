import axios from 'axios'

const api = axios.create({
    baseURL: '/api',
    timeout: 15000,
    headers: { 'Content-Type': 'application/json' },
})

// Attach JWT token to every request
api.interceptors.request.use((config) => {
    const token = localStorage.getItem('token')
    if (token) {
        config.headers.Authorization = `Bearer ${token}`
    }
    return config
})

// Handle 401 responses — redirect to login
api.interceptors.response.use(
    (response) => response,
    (error) => {
        if (error.response?.status === 401) {
            localStorage.removeItem('token')
            window.location.href = '/login'
        }
        return Promise.reject(error)
    }
)

// ============ Auth ============
export const authAPI = {
    needSetup: () => api.get('/auth/need-setup'),
    setup: (data) => api.post('/auth/setup', data),
    login: (data) => api.post('/auth/login', data),
    me: () => api.get('/auth/me'),
    setup2FA: () => api.post('/auth/2fa/setup'),
    verify2FA: (code) => api.post('/auth/2fa/verify', { code }),
    disable2FA: (code) => api.post('/auth/2fa/disable', { code }),
}

// ============ Hosts ============
export const hostAPI = {
    list: (params) => api.get('/hosts', { params }),
    get: (id) => api.get(`/hosts/${id}`),
    create: (data) => api.post('/hosts', data),
    update: (id, data) => api.put(`/hosts/${id}`, data),
    delete: (id) => api.delete(`/hosts/${id}`),
    toggle: (id) => api.patch(`/hosts/${id}/toggle`),
    clone: (id, data) => api.post(`/hosts/${id}/clone`, data),
    uploadCert: (id, formData) => api.post(`/hosts/${id}/cert`, formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
    }),
    deleteCert: (id) => api.delete(`/hosts/${id}/cert`),
}

// ============ DNS Check ============
export const dnsCheckAPI = {
    check: (domain) => api.get('/dns-check', { params: { domain } }),
}

// ============ Groups ============
export const groupAPI = {
    list: () => api.get('/groups'),
    create: (data) => api.post('/groups', data),
    update: (id, data) => api.put(`/groups/${id}`, data),
    delete: (id) => api.delete(`/groups/${id}`),
    batchEnable: (id) => api.post(`/groups/${id}/batch-enable`),
    batchDisable: (id) => api.post(`/groups/${id}/batch-disable`),
}

// ============ Tags ============
export const tagAPI = {
    list: () => api.get('/tags'),
    create: (data) => api.post('/tags', data),
    update: (id, data) => api.put(`/tags/${id}`, data),
    delete: (id) => api.delete(`/tags/${id}`),
}

// ============ Caddy ============
export const caddyAPI = {
    status: () => api.get('/caddy/status'),
    start: () => api.post('/caddy/start'),
    stop: () => api.post('/caddy/stop'),
    reload: () => api.post('/caddy/reload'),
    caddyfile: () => api.get('/caddy/caddyfile'),
    saveCaddyfile: (content, reload = false) => api.post('/caddy/caddyfile', { content, reload }),
    format: (content) => api.post('/caddy/fmt', { content }),
    validate: (content) => api.post('/caddy/validate', { content }),
}

// ============ Logs ============
export const logAPI = {
    get: (params) => api.get('/logs', { params }),
    files: () => api.get('/logs/files'),
    downloadUrl: (type) => `/api/logs/download?type=${type}`,
    system: (params) => api.get('/logs/system', { params }),
}

// ============ Config ============
export const configAPI = {
    export: () => api.get('/config/export'),
    import: (data) => api.post('/config/import', data),
}

// ============ Dashboard ============
export const dashboardAPI = {
    stats: () => api.get('/dashboard/stats'),
}

// ============ Users ============
export const userAPI = {
    list: () => api.get('/users'),
    create: (data) => api.post('/users', data),
    update: (id, data) => api.put(`/users/${id}`, data),
    delete: (id) => api.delete(`/users/${id}`),
}

// ============ Audit ============
export const auditAPI = {
    list: (params) => api.get('/audit/logs', { params }),
}

// ============ Templates ============
export const templateAPI = {
    list: () => api.get('/templates'),
    create: (data) => api.post('/templates', data),
    update: (id, data) => api.put(`/templates/${id}`, data),
    delete: (id) => api.delete(`/templates/${id}`),
    import: (formData) => api.post('/templates/import', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
    }),
    export: (id) => api.get(`/templates/${id}/export`, { responseType: 'blob' }),
    createHost: (id, data) => api.post(`/templates/${id}/create-host`, data),
    saveAsTemplate: (hostId, data) => api.post(`/hosts/${hostId}/save-as-template`, data),
}

// ============ Settings ============
export const settingAPI = {
    getAll: () => api.get('/settings/all'),
    update: (key, value) => api.put('/settings', { key, value }),
}

// ============ Certificates ============
export const certificateAPI = {
    list: () => api.get('/certificates'),
    upload: (formData) => api.post('/certificates', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
    }),
    delete: (id) => api.delete(`/certificates/${id}`),
}

// ============ Docker (plugin) ============
export const dockerAPI = {
    // System
    info: () => api.get('/plugins/docker/info'),
    status: () => api.get('/plugins/docker/status'),

    // Stacks
    listStacks: () => api.get('/plugins/docker/stacks'),
    getStack: (id) => api.get(`/plugins/docker/stacks/${id}`),
    createStack: (data) => api.post('/plugins/docker/stacks', data),
    updateStack: (id, data) => api.put(`/plugins/docker/stacks/${id}`, data),
    deleteStack: (id) => api.delete(`/plugins/docker/stacks/${id}`),
    stackUp: (id) => api.post(`/plugins/docker/stacks/${id}/up`),
    stackDown: (id) => api.post(`/plugins/docker/stacks/${id}/down`),
    stackRestart: (id) => api.post(`/plugins/docker/stacks/${id}/restart`),
    stackPull: (id) => api.post(`/plugins/docker/stacks/${id}/pull`),
    stackLogs: (id, tail) => api.get(`/plugins/docker/stacks/${id}/logs`, { params: { tail } }),

    // Daemon config
    getDaemonConfig: () => api.get('/plugins/docker/daemon-config'),
    updateDaemonConfig: (data) => api.put('/plugins/docker/daemon-config', data, { timeout: 60000 }),

    // Containers
    listContainers: (all = true) => api.get('/plugins/docker/containers', { params: { all } }),
    runContainer: (data) => api.post('/plugins/docker/containers/run', data, { timeout: 300000 }),
    startContainer: (id) => api.post(`/plugins/docker/containers/${id}/start`),
    stopContainer: (id) => api.post(`/plugins/docker/containers/${id}/stop`),
    restartContainer: (id) => api.post(`/plugins/docker/containers/${id}/restart`),
    removeContainer: (id) => api.delete(`/plugins/docker/containers/${id}`),
    containerLogs: (id, tail) => api.get(`/plugins/docker/containers/${id}/logs`, { params: { tail } }),
    containerStats: (id) => api.get(`/plugins/docker/containers/${id}/stats`),

    // Images
    listImages: () => api.get('/plugins/docker/images'),
    pullImage: (image) => api.post('/plugins/docker/images/pull', { image }, { timeout: 300000 }),
    removeImage: (id) => api.delete(`/plugins/docker/images/${id}`),
    pruneImages: () => api.post('/plugins/docker/images/prune'),
    searchImages: (q, limit) => api.get('/plugins/docker/images/search', { params: { q, limit } }),

    // Networks
    listNetworks: () => api.get('/plugins/docker/networks'),
    createNetwork: (name) => api.post('/plugins/docker/networks', { name }),
    removeNetwork: (id) => api.delete(`/plugins/docker/networks/${id}`),

    // Volumes
    listVolumes: () => api.get('/plugins/docker/volumes'),
    createVolume: (name) => api.post('/plugins/docker/volumes', { name }),
    removeVolume: (id) => api.delete(`/plugins/docker/volumes/${id}`),
}

// ============ Deploy (plugin) ============
export const deployAPI = {
    // Frameworks
    frameworks: () => api.get('/plugins/deploy/frameworks'),
    detect: (url, branch) => api.get('/plugins/deploy/detect', { params: { url, branch } }),

    // Projects
    listProjects: () => api.get('/plugins/deploy/projects'),
    getProject: (id) => api.get(`/plugins/deploy/projects/${id}`),
    createProject: (data) => api.post('/plugins/deploy/projects', data),
    updateProject: (id, data) => api.put(`/plugins/deploy/projects/${id}`, data),
    deleteProject: (id) => api.delete(`/plugins/deploy/projects/${id}`),

    // Actions
    build: (id) => api.post(`/plugins/deploy/projects/${id}/build`),
    start: (id) => api.post(`/plugins/deploy/projects/${id}/start`),
    stop: (id) => api.post(`/plugins/deploy/projects/${id}/stop`),
    rollback: (id, buildNum) => api.post(`/plugins/deploy/projects/${id}/rollback`, { build_num: buildNum }),

    // Deployments & Logs
    deployments: (id) => api.get(`/plugins/deploy/projects/${id}/deployments`),
    logs: (id, params) => api.get(`/plugins/deploy/projects/${id}/logs`, { params }),
}

// ============ AI (plugin) ============
export const aiAPI = {
    // Config
    getConfig: () => api.get('/plugins/ai/config'),
    updateConfig: (data) => api.put('/plugins/ai/config', data),
    testConnection: () => api.post('/plugins/ai/config/test'),
    getPresets: () => api.get('/plugins/ai/presets'),

    // Conversations
    listConversations: () => api.get('/plugins/ai/conversations'),
    getConversation: (id) => api.get(`/plugins/ai/conversations/${id}`),
    deleteConversation: (id) => api.delete(`/plugins/ai/conversations/${id}`),

    // Tools
    generateCompose: (description) => api.post('/plugins/ai/generate-compose', { description }),
    diagnose: (logs, context) => api.post('/plugins/ai/diagnose', { logs, context }),
}

// ============ File Manager (plugin) ============
export const fileManagerAPI = {
    list: (path) => api.get('/plugins/filemanager/list', { params: { path } }),
    read: (path) => api.get('/plugins/filemanager/read', { params: { path } }),
    write: (path, content) => api.post('/plugins/filemanager/write', { path, content }),
    upload: (formData) => api.post('/plugins/filemanager/upload', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
        timeout: 300000,
    }),
    download: (path) => `/api/plugins/filemanager/download?path=${encodeURIComponent(path)}`,
    mkdir: (path) => api.post('/plugins/filemanager/mkdir', { path }),
    delete: (paths) => api.delete('/plugins/filemanager/delete', { data: { paths } }),
    rename: (old_path, new_path) => api.post('/plugins/filemanager/rename', { old_path, new_path }),
    chmod: (path, mode) => api.post('/plugins/filemanager/chmod', { path, mode }),
    info: (path) => api.get('/plugins/filemanager/info', { params: { path } }),
    compress: (paths, dest, format) => api.post('/plugins/filemanager/compress', { paths, dest, format }),
    extract: (path, dest) => api.post('/plugins/filemanager/extract', { path, dest }),
    terminalWsUrl: (cols, rows) => {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        const token = localStorage.getItem('token')
        return `${proto}//${window.location.host}/api/plugins/filemanager/terminal/ws?cols=${cols}&rows=${rows}&token=${token}`
    },
}

// ============ Database (plugin) ============
export const databaseAPI = {
    engines: () => api.get('/plugins/database/engines'),
    presets: () => api.get('/plugins/database/presets'),

    listInstances: () => api.get('/plugins/database/instances'),
    getInstance: (id) => api.get(`/plugins/database/instances/${id}`),
    createInstance: (data) => api.post('/plugins/database/instances', data),
    deleteInstance: (id) => api.delete(`/plugins/database/instances/${id}`),
    startInstance: (id) => api.post(`/plugins/database/instances/${id}/start`),
    stopInstance: (id) => api.post(`/plugins/database/instances/${id}/stop`),
    restartInstance: (id) => api.post(`/plugins/database/instances/${id}/restart`),
    instanceLogs: (id, tail) => api.get(`/plugins/database/instances/${id}/logs`, { params: { tail } }),
    connectionInfo: (id) => api.get(`/plugins/database/instances/${id}/connection`),
    rootPassword: (id) => api.get(`/plugins/database/instances/${id}/password`),

    listDatabases: (id) => api.get(`/plugins/database/instances/${id}/databases`),
    createDatabase: (id, data) => api.post(`/plugins/database/instances/${id}/databases`, data),
    deleteDatabase: (id, dbname) => api.delete(`/plugins/database/instances/${id}/databases/${dbname}`),

    listUsers: (id) => api.get(`/plugins/database/instances/${id}/users`),
    createUser: (id, data) => api.post(`/plugins/database/instances/${id}/users`, data),
    deleteUser: (id, username) => api.delete(`/plugins/database/instances/${id}/users/${username}`),

    sqliteTables: (path) => api.get('/plugins/database/sqlite/tables', { params: { path } }),
    sqliteSchema: (path, table) => api.get('/plugins/database/sqlite/schema', { params: { path, table } }),
    sqliteQuery: (path, query, limit) => api.post('/plugins/database/sqlite/query', { path, query, limit }),

    executeQuery: (id, data) => api.post(`/plugins/database/instances/${id}/query`, data, { timeout: 35000 }),

    instanceLogsWsUrl: (id) => {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        const token = localStorage.getItem('token')
        return `${proto}//${window.location.host}/api/plugins/database/instances/${id}/logs/ws?tail=100&token=${token}`
    },
}

// ============ Monitoring (plugin) ============
export const monitoringAPI = {
    getCurrent: () => api.get('/plugins/monitoring/metrics/current'),
    getHistory: (period) => api.get('/plugins/monitoring/metrics/history', { params: { period } }),
    getContainers: () => api.get('/plugins/monitoring/metrics/containers'),
    metricsWsUrl: () => {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        const token = localStorage.getItem('token')
        return `${proto}//${window.location.host}/api/plugins/monitoring/metrics/ws?token=${token}`
    },
    listAlertRules: () => api.get('/plugins/monitoring/alerts'),
    createAlertRule: (data) => api.post('/plugins/monitoring/alerts', data),
    updateAlertRule: (id, data) => api.put(`/plugins/monitoring/alerts/${id}`, data),
    deleteAlertRule: (id) => api.delete(`/plugins/monitoring/alerts/${id}`),
    listAlertHistory: (limit) => api.get('/plugins/monitoring/alerts/history', { params: { limit } }),
}

// ============ Backup (plugin) ============
export const backupAPI = {
    checkDependency: () => api.get('/plugins/backup/dependency-check'),
    getConfig: () => api.get('/plugins/backup/config'),
    updateConfig: (data) => api.put('/plugins/backup/config', data),
    testConnection: () => api.post('/plugins/backup/config/test', {}, { timeout: 60000 }),
    listSnapshots: () => api.get('/plugins/backup/snapshots'),
    createSnapshot: () => api.post('/plugins/backup/snapshots', {}, { timeout: 600000 }),
    restoreSnapshot: (id) => api.post(`/plugins/backup/snapshots/${id}/restore`, {}, { timeout: 600000 }),
    deleteSnapshot: (id) => api.delete(`/plugins/backup/snapshots/${id}`),
    getStatus: () => api.get('/plugins/backup/status'),
    listLogs: (params) => api.get('/plugins/backup/logs', { params }),
}

// ============ App Store (plugin) ============
export const appstoreAPI = {
    // Catalog
    listApps: (params) => api.get('/plugins/appstore/apps', { params }),
    getApp: (id) => api.get(`/plugins/appstore/apps/${id}`),
    appLogoUrl: (id) => `/api/plugins/appstore/apps/${id}/logo`,
    listCategories: () => api.get('/plugins/appstore/categories'),
    // Sources
    listSources: () => api.get('/plugins/appstore/sources'),
    addSource: (data) => api.post('/plugins/appstore/sources', data),
    syncSource: (id) => api.post(`/plugins/appstore/sources/${id}/sync`),
    syncSourceStreamUrl: (id) => `/api/plugins/appstore/sources/${id}/sync/stream`,
    removeSource: (id) => api.delete(`/plugins/appstore/sources/${id}`),
    // Installed
    listInstalled: () => api.get('/plugins/appstore/installed'),
    getInstalled: (id) => api.get(`/plugins/appstore/installed/${id}`),
    install: (data) => api.post('/plugins/appstore/install', data, { timeout: 300000 }),
    startApp: (id) => api.post(`/plugins/appstore/installed/${id}/start`),
    stopApp: (id) => api.post(`/plugins/appstore/installed/${id}/stop`),
    updateApp: (id) => api.post(`/plugins/appstore/installed/${id}/update`, {}, { timeout: 300000 }),
    uninstall: (id, removeData) => api.delete(`/plugins/appstore/installed/${id}`, { params: { remove_data: removeData } }),
    checkUpdates: () => api.get('/plugins/appstore/updates'),
    // Templates
    listTemplates: (params) => api.get('/plugins/appstore/templates', { params }),
    getTemplate: (id) => api.get(`/plugins/appstore/templates/${id}`),
    deployTemplate: (data) => api.post('/plugins/appstore/templates/deploy', data),
}

// ============ Version Check ============
export const versionCheckAPI = {
    check: () => api.get('/version-check'),
}

// ============ Plugins ============
export const pluginAPI = {
    list: () => api.get('/plugins'),
    enable: (id) => api.post(`/plugins/${id}/enable`),
    disable: (id) => api.post(`/plugins/${id}/disable`),
    frontendManifests: () => api.get('/plugins/frontend-manifests'),
    setSidebarVisible: (id, visible) => api.post(`/plugins/${id}/sidebar`, { visible }),
    installUrl: (id) => `/api/plugins/${id}/install`,
}

// ============ DNS Providers ============
export const dnsProviderAPI = {
    list: () => api.get('/dns-providers'),
    get: (id) => api.get(`/dns-providers/${id}`),
    create: (data) => api.post('/dns-providers', data),
    update: (id, data) => api.put(`/dns-providers/${id}`, data),
    delete: (id) => api.delete(`/dns-providers/${id}`),
}

// ============ MCP Server / API Tokens ============
export const mcpAPI = {
    listTokens: () => api.get('/plugins/mcpserver/tokens'),
    createToken: (data) => api.post('/plugins/mcpserver/tokens', data),
    deleteToken: (id) => api.delete(`/plugins/mcpserver/tokens/${id}`),
}

export default api
