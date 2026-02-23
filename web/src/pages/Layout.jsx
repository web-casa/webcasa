import { NavLink, Outlet, useNavigate } from 'react-router'
import { Box, Flex, Text, DropdownMenu, Separator } from '@radix-ui/themes'
import { useState, useEffect } from 'react'
import {
    Zap,
    LayoutDashboard,
    Globe,
    FileText,
    FileCode,
    Shield,
    ShieldCheck,
    Settings,
    LogOut,
    User,
    Users,
    ClipboardList,
    ChevronDown,
    Sun,
    Moon,
} from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'
import { useThemeStore } from '../stores/theme.js'
import { dashboardAPI } from '../api/index.js'

const navItems = [
    { to: '/', icon: LayoutDashboard, label: 'Dashboard', end: true },
    { to: '/hosts', icon: Globe, label: '站点管理' },
    { to: '/editor', icon: FileCode, label: 'Caddyfile' },
    { to: '/dns', icon: Shield, label: 'DNS Providers' },
    { to: '/certificates', icon: ShieldCheck, label: '证书管理' },
    { to: '/logs', icon: FileText, label: '日志' },
    { to: '/users', icon: Users, label: '用户管理' },
    { to: '/audit', icon: ClipboardList, label: '审计日志' },
    { to: '/settings', icon: Settings, label: '设置' },
]

function SidebarLink({ to, icon: Icon, label, end }) {
    return (
        <NavLink to={to} end={end} className="sidebar-link">
            <Icon size={18} />
            <span>{label}</span>
        </NavLink>
    )
}

export default function Layout() {
    const navigate = useNavigate()
    const { user, logout } = useAuthStore()
    const { theme, toggle: toggleTheme } = useThemeStore()
    const [version, setVersion] = useState('')

    useEffect(() => {
        dashboardAPI.stats().then(res => {
            setVersion(res.data?.system?.panel_version || '')
        }).catch(() => { })
    }, [])

    const handleLogout = () => {
        logout()
        navigate('/login', { replace: true })
    }

    return (
        <Flex style={{ minHeight: '100vh' }}>
            {/* Sidebar */}
            <Box
                style={{
                    width: 220,
                    minWidth: 220,
                    background: 'var(--cp-sidebar)',
                    borderRight: '1px solid var(--cp-border)',
                    display: 'flex',
                    flexDirection: 'column',
                }}
            >
                {/* Logo */}
                <Flex align="center" gap="2" p="4" pb="2">
                    <Box
                        style={{
                            width: 32,
                            height: 32,
                            borderRadius: 8,
                            background: 'linear-gradient(135deg, #10b981, #059669)',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                        }}
                    >
                        <Zap size={18} color="white" />
                    </Box>
                    <Text size="4" weight="bold" style={{ color: 'var(--cp-text)' }}>
                        CaddyPanel
                    </Text>
                </Flex>

                <Separator size="4" style={{ background: 'var(--cp-border)' }} />

                {/* Nav items */}
                <Box style={{ flex: 1, padding: '8px 12px' }}>
                    <Flex direction="column" gap="1" mt="2">
                        {navItems.map((item) => (
                            <SidebarLink key={item.to} {...item} />
                        ))}
                    </Flex>
                </Box>

                {/* Bottom: theme toggle + user menu */}
                <Box p="3" style={{ borderTop: '1px solid var(--cp-border)' }}>
                    {/* Theme toggle */}
                    <button
                        onClick={toggleTheme}
                        className="sidebar-btn"
                        style={{ marginBottom: 4 }}
                    >
                        {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
                        <span>{theme === 'dark' ? '浅色模式' : '深色模式'}</span>
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
                                Sign Out
                            </DropdownMenu.Item>
                        </DropdownMenu.Content>
                    </DropdownMenu.Root>
                </Box>
            </Box>

            {/* Main content */}
            <Box
                style={{
                    flex: 1,
                    background: 'var(--cp-bg)',
                    overflow: 'auto',
                    position: 'relative',
                }}
            >
                <Box p="5" style={{ maxWidth: 1200, margin: '0 auto', paddingBottom: 48 }}>
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
                        CaddyPanel v{version}
                    </Text>
                )}
            </Box>
        </Flex>
    )
}
