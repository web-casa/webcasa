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
    challenge: () => api.get('/auth/challenge'),
}

// ============ Hosts ============
export const hostAPI = {
    list: () => api.get('/hosts'),
    get: (id) => api.get(`/hosts/${id}`),
    create: (data) => api.post('/hosts', data),
    update: (id, data) => api.put(`/hosts/${id}`, data),
    delete: (id) => api.delete(`/hosts/${id}`),
    toggle: (id) => api.patch(`/hosts/${id}/toggle`),
    uploadCert: (id, formData) => api.post(`/hosts/${id}/cert`, formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
    }),
    deleteCert: (id) => api.delete(`/hosts/${id}/cert`),
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
