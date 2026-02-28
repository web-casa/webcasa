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

    // Containers
    listContainers: (all = true) => api.get('/plugins/docker/containers', { params: { all } }),
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

// ============ Plugins ============
export const pluginAPI = {
    list: () => api.get('/plugins'),
    enable: (id) => api.post(`/plugins/${id}/enable`),
    disable: (id) => api.post(`/plugins/${id}/disable`),
    frontendManifests: () => api.get('/plugins/frontend-manifests'),
}

// ============ DNS Providers ============
export const dnsProviderAPI = {
    list: () => api.get('/dns-providers'),
    get: (id) => api.get(`/dns-providers/${id}`),
    create: (data) => api.post('/dns-providers', data),
    update: (id, data) => api.put(`/dns-providers/${id}`, data),
    delete: (id) => api.delete(`/dns-providers/${id}`),
}

export default api
