import { create } from 'zustand'

export const useThemeStore = create((set) => ({
    // 'light' or 'dark'
    theme: localStorage.getItem('webcasa-theme') || 'light',

    setTheme: (theme) => {
        localStorage.setItem('webcasa-theme', theme)
        document.documentElement.className = theme === 'dark' ? 'dark-theme' : 'light-theme'
        set({ theme })
    },

    toggle: () => {
        set((state) => {
            const next = state.theme === 'dark' ? 'light' : 'dark'
            localStorage.setItem('webcasa-theme', next)
            document.documentElement.className = next === 'dark' ? 'dark-theme' : 'light-theme'
            return { theme: next }
        })
    },

    // Call on app init to sync class
    init: () => {
        const saved = localStorage.getItem('webcasa-theme') || 'light'
        document.documentElement.className = saved === 'dark' ? 'dark-theme' : 'light-theme'
        set({ theme: saved })
    },
}))
