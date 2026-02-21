import { create } from 'zustand'
import { authAPI } from '../api/index.js'

export const useAuthStore = create((set) => ({
    token: localStorage.getItem('token') || null,
    user: null,
    needSetup: false,
    loading: true,

    setToken: (token) => {
        if (token) {
            localStorage.setItem('token', token)
        } else {
            localStorage.removeItem('token')
        }
        set({ token })
    },

    setUser: (user) => set({ user }),

    checkSetup: async () => {
        try {
            const res = await authAPI.needSetup()
            set({ needSetup: res.data.need_setup, loading: false })
        } catch {
            set({ loading: false })
        }
    },

    login: async (username, password) => {
        const res = await authAPI.login({ username, password })
        const { token, user } = res.data
        localStorage.setItem('token', token)
        set({ token, user })
        return res.data
    },

    setup: async (username, password) => {
        const res = await authAPI.setup({ username, password })
        const { token, user } = res.data
        localStorage.setItem('token', token)
        set({ token, user, needSetup: false })
        return res.data
    },

    fetchMe: async () => {
        try {
            const res = await authAPI.me()
            set({ user: res.data })
        } catch {
            localStorage.removeItem('token')
            set({ token: null, user: null })
        }
    },

    logout: () => {
        localStorage.removeItem('token')
        set({ token: null, user: null })
    },
}))
