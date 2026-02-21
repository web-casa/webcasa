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
}

// ============ Hosts ============
export const hostAPI = {
    list: () => api.get('/hosts'),
    get: (id) => api.get(`/hosts/${id}`),
    create: (data) => api.post('/hosts', data),
    update: (id, data) => api.put(`/hosts/${id}`, data),
    delete: (id) => api.delete(`/hosts/${id}`),
    toggle: (id) => api.patch(`/hosts/${id}/toggle`),
}

// ============ Caddy ============
export const caddyAPI = {
    status: () => api.get('/caddy/status'),
    start: () => api.post('/caddy/start'),
    stop: () => api.post('/caddy/stop'),
    reload: () => api.post('/caddy/reload'),
    caddyfile: () => api.get('/caddy/caddyfile'),
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

export default api
