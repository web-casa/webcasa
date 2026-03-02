import { create } from 'zustand'
import { pluginAPI } from '../api/index.js'

export const usePluginNavStore = create((set) => ({
    plugins: [],
    manifests: [],
    navItems: [],
    loading: true,

    refresh: async () => {
        try {
            const [pluginRes, manifestRes] = await Promise.all([
                pluginAPI.list(),
                pluginAPI.frontendManifests(),
            ])
            const plugins = pluginRes.data?.plugins || []
            const manifests = manifestRes.data || []

            // Build dynamic nav items from enabled plugins with sidebar visibility
            const enabledMap = {}
            for (const p of plugins) {
                if (p.enabled && p.show_in_sidebar) {
                    enabledMap[p.id] = p
                }
            }

            const navItems = []
            for (const m of manifests) {
                if (!enabledMap[m.id]) continue
                const routes = m.routes || []
                for (const r of routes) {
                    if (!r.menu) continue
                    navItems.push({
                        to: r.path,
                        icon: r.icon || 'Box',
                        label: r.label,
                        labelZh: r.label_zh,
                        pluginId: m.id,
                        menuGroup: m.menu_group,
                        menuOrder: m.menu_order,
                    })
                }
            }

            // Sort by menu_order
            navItems.sort((a, b) => (a.menuOrder || 0) - (b.menuOrder || 0))

            set({ plugins, manifests, navItems, loading: false })
        } catch {
            set({ loading: false })
        }
    },
}))
