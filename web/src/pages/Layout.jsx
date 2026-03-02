import { NavLink, Outlet, useNavigate, useLocation } from 'react-router'
import { Box, Flex, Text, DropdownMenu, Separator } from '@radix-ui/themes'
import { useState, useEffect, useCallback, useMemo } from 'react'
import * as LucideIcons from 'lucide-react'
import {
    LayoutDashboard,
    Globe,
    Settings,
    LogOut,
    User,
    ChevronDown,
    Sun,
    Moon,
    Languages,
    Menu,
    X,
    Puzzle,
    Box as BoxIcon,
} from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'
import { useThemeStore } from '../stores/theme.js'
import { usePluginNavStore } from '../stores/pluginNav.js'
import { dashboardAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import logoImg from '../assets/logo.png'

// Fixed navigation items (always shown)
const fixedNavItems = [
    { to: '/', icon: LayoutDashboard, labelKey: 'nav.dashboard', end: true },
    { to: '/hosts', icon: Globe, labelKey: 'nav.hosts' },
]

const bottomFixedNavItems = [
    { to: '/plugins', icon: Puzzle, labelKey: 'nav.plugins' },
    { to: '/settings', icon: Settings, labelKey: 'nav.settings' },
]

// Resolve lucide icon name to component
function resolveIcon(iconName) {
    if (!iconName) return BoxIcon
    const component = LucideIcons[iconName]
    return component || BoxIcon
}

function SidebarLink({ to, icon: Icon, label, end, onClick }) {
    return (
        <NavLink to={to} end={end} className="sidebar-link" onClick={onClick}>
            <Icon size={18} />
            <span>{label}</span>
        </NavLink>
    )
}

export default function Layout() {
    const navigate = useNavigate()
    const location = useLocation()
    const { t, i18n } = useTranslation()
    const { user, logout } = useAuthStore()
    const { theme, toggle: toggleTheme } = useThemeStore()
    const { navItems: pluginNavItems, refresh: refreshPluginNav } = usePluginNavStore()
    const [version, setVersion] = useState('')
    const [isMobile, setIsMobile] = useState(() =>
        typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches
    )
    const [sidebarOpen, setSidebarOpen] = useState(false)

    const currentLang = i18n.language?.startsWith('zh') ? 'zh' : 'en'

    const toggleLang = () => {
        const next = currentLang === 'zh' ? 'en' : 'zh'
        i18n.changeLanguage(next)
    }

    // Load plugin nav items on mount
    useEffect(() => {
        refreshPluginNav()
    }, [])

    // Listen for viewport changes
    useEffect(() => {
        const mql = window.matchMedia('(max-width: 767px)')
        const handler = (e) => {
            setIsMobile(e.matches)
            if (!e.matches) setSidebarOpen(false)
        }
        mql.addEventListener('change', handler)
        return () => mql.removeEventListener('change', handler)
    }, [])

    useEffect(() => {
        dashboardAPI.stats().then(res => {
            setVersion(res.data?.system?.panel_version || '')
        }).catch(() => { })
    }, [])

    // Build dynamic nav items from plugin manifests
    const dynamicNavItems = useMemo(() => {
        return pluginNavItems.map((item) => ({
            to: item.to,
            icon: resolveIcon(item.icon),
            label: currentLang === 'zh' && item.labelZh ? item.labelZh : item.label,
        }))
    }, [pluginNavItems, currentLang])

    // Close sidebar on navigation (mobile)
    const handleNavClick = useCallback(() => {
        if (isMobile) setSidebarOpen(false)
    }, [isMobile])

    const handleLogout = () => {
        if (isMobile) setSidebarOpen(false)
        logout()
        navigate('/login', { replace: true })
    }

    const sidebarContent = (
        <>
            {/* Logo */}
            <Flex align="center" gap="2" p="4" pb="2">
                <img src={logoImg} alt="WebCasa" style={{ width: 32, height: 32, borderRadius: 8 }} />
                <Text size="4" weight="bold" style={{ color: 'var(--cp-text)' }}>
                    WebCasa
                </Text>
                {isMobile && (
                    <button
                        className="sidebar-btn"
                        style={{ marginLeft: 'auto', padding: 4, width: 'auto' }}
                        onClick={() => setSidebarOpen(false)}
                        aria-label={t('mobile.close_menu')}
                    >
                        <X size={18} />
                    </button>
                )}
            </Flex>

            <Separator size="4" style={{ background: 'var(--cp-border)' }} />

            {/* Nav items */}
            <Box style={{ flex: 1, padding: '8px 12px', overflowY: 'auto' }}>
                <Flex direction="column" gap="1" mt="2">
                    {/* Fixed top items */}
                    {fixedNavItems.map((item) => (
                        <SidebarLink
                            key={item.to}
                            to={item.to}
                            icon={item.icon}
                            label={t(item.labelKey)}
                            end={item.end}
                            onClick={handleNavClick}
                        />
                    ))}

                    {/* Dynamic plugin items */}
                    {dynamicNavItems.length > 0 && (
                        <>
                            <Separator size="4" my="1" style={{ background: 'var(--cp-border)', opacity: 0.5 }} />
                            {dynamicNavItems.map((item) => (
                                <SidebarLink
                                    key={item.to}
                                    to={item.to}
                                    icon={item.icon}
                                    label={item.label}
                                    onClick={handleNavClick}
                                />
                            ))}
                        </>
                    )}

                    {/* Fixed bottom items */}
                    <Separator size="4" my="1" style={{ background: 'var(--cp-border)', opacity: 0.5 }} />
                    {bottomFixedNavItems.map((item) => (
                        <SidebarLink
                            key={item.to}
                            to={item.to}
                            icon={item.icon}
                            label={t(item.labelKey)}
                            end={item.end}
                            onClick={handleNavClick}
                        />
                    ))}
                </Flex>
            </Box>

            {/* Bottom: lang toggle + theme toggle + user menu */}
            <Box p="3" style={{ borderTop: '1px solid var(--cp-border)' }}>
                {/* Language toggle */}
                <button
                    onClick={toggleLang}
                    className="sidebar-btn"
                    style={{ marginBottom: 4 }}
                >
                    <Languages size={16} />
                    <span>{currentLang === 'zh' ? 'EN' : '中文'}</span>
                </button>

                {/* Theme toggle */}
                <button
                    onClick={toggleTheme}
                    className="sidebar-btn"
                    style={{ marginBottom: 4 }}
                >
                    {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
                    <span>{theme === 'dark' ? t('nav.light_mode') : t('nav.dark_mode')}</span>
                </button>

                {/* User menu */}
                <DropdownMenu.Root>
                    <DropdownMenu.Trigger asChild>
                        <button className="sidebar-btn">
                            <User size={16} />
                            <span style={{ flex: 1, textAlign: 'left' }}>
                                {user?.username || 'Admin'}
                            </span>
                            <ChevronDown size={14} />
                        </button>
                    </DropdownMenu.Trigger>
                    <DropdownMenu.Content side="top" align="start">
                        <DropdownMenu.Item color="red" onClick={handleLogout}>
                            <LogOut size={14} />
                            {t('nav.sign_out')}
                        </DropdownMenu.Item>
                    </DropdownMenu.Content>
                </DropdownMenu.Root>
            </Box>
        </>
    )

    return (
        <Flex style={{ minHeight: '100vh' }}>
            {/* Mobile: top bar with hamburger */}
            {isMobile && (
                <Box className="mobile-topbar">
                    <button
                        className="hamburger-btn"
                        onClick={() => setSidebarOpen(true)}
                        aria-label={t('mobile.open_menu')}
                    >
                        <Menu size={22} />
                    </button>
                    <Flex align="center" gap="2">
                        <img src={logoImg} alt="WebCasa" style={{ width: 24, height: 24, borderRadius: 6 }} />
                        <Text size="3" weight="bold" style={{ color: 'var(--cp-text)' }}>
                            WebCasa
                        </Text>
                    </Flex>
                    <Box style={{ width: 22 }} /> {/* Spacer for centering */}
                </Box>
            )}

            {/* Mobile: backdrop overlay */}
            {isMobile && sidebarOpen && (
                <Box
                    className="sidebar-backdrop"
                    onClick={() => setSidebarOpen(false)}
                />
            )}

            {/* Sidebar */}
            <Box
                className={isMobile ? `sidebar-mobile ${sidebarOpen ? 'sidebar-mobile-open' : ''}` : ''}
                style={!isMobile ? {
                    width: 220,
                    minWidth: 220,
                    background: 'var(--cp-sidebar)',
                    borderRight: '1px solid var(--cp-border)',
                    display: 'flex',
                    flexDirection: 'column',
                } : undefined}
            >
                {sidebarContent}
            </Box>

            {/* Main content */}
            <Box
                style={{
                    flex: 1,
                    background: 'var(--cp-bg)',
                    overflow: 'auto',
                    position: 'relative',
                    ...(isMobile ? { paddingTop: 56 } : {}),
                }}
            >
                <Box p="5" style={{ maxWidth: 1200, margin: '0 auto', paddingBottom: 48, ...(isMobile ? { padding: '16px' } : {}) }}>
                    <Outlet />
                </Box>
                {version && (
                    <Text
                        size="1"
                        style={{
                            position: 'fixed',
                            bottom: 8,
                            right: 12,
                            color: 'var(--cp-text-muted)',
                            userSelect: 'none',
                            fontFamily: 'monospace',
                            fontSize: '0.7rem',
                        }}
                    >
                        WebCasa v{version}
                    </Text>
                )}
            </Box>
        </Flex>
    )
}
