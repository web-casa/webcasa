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

// ============ DNS Providers ============
export const dnsProviderAPI = {
    list: () => api.get('/dns-providers'),
    get: (id) => api.get(`/dns-providers/${id}`),
    create: (data) => api.post('/dns-providers', data),
    update: (id, data) => api.put(`/dns-providers/${id}`, data),
    delete: (id) => api.delete(`/dns-providers/${id}`),
}

export default api
