import { create } from 'zustand'

export const useAIChatStore = create((set) => ({
    isOpen: false,
    toggle: () => set((s) => ({ isOpen: !s.isOpen })),
    open: () => set({ isOpen: true }),
    close: () => set({ isOpen: false }),
}))
