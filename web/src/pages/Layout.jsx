import { NavLink, Outlet, useNavigate } from 'react-router'
import { Box, Flex, Text, Button, DropdownMenu, Separator, Tooltip } from '@radix-ui/themes'
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
} from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'
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
        <NavLink
            to={to}
            end={end}
            style={({ isActive }) => ({
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '10px 14px',
                borderRadius: 8,
                textDecoration: 'none',
                fontSize: '0.875rem',
                fontWeight: isActive ? 600 : 400,
                color: isActive ? '#10b981' : '#a1a1aa',
                background: isActive ? 'rgba(16, 185, 129, 0.08)' : 'transparent',
                transition: 'all 0.15s ease',
            })}
            onMouseEnter={(e) => {
                if (!e.currentTarget.classList.contains('active')) {
                    e.currentTarget.style.background = 'rgba(255,255,255,0.04)'
                    e.currentTarget.style.color = '#d4d4d8'
                }
            }}
            onMouseLeave={(e) => {
                const isActive = e.currentTarget.getAttribute('aria-current') === 'page'
                if (!isActive) {
                    e.currentTarget.style.background = 'transparent'
                    e.currentTarget.style.color = '#a1a1aa'
                }
            }}
        >
            <Icon size={18} />
            <span>{label}</span>
        </NavLink>
    )
}

export default function Layout() {
    const navigate = useNavigate()
    const { user, logout } = useAuthStore()
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
                    background: '#111113',
                    borderRight: '1px solid #27272a',
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
                    <Text size="4" weight="bold" style={{ color: '#fafafa' }}>
                        CaddyPanel
                    </Text>
                </Flex>

                <Separator size="4" style={{ background: '#27272a' }} />

                {/* Nav items */}
                <Box style={{ flex: 1, padding: '8px 12px' }}>
                    <Flex direction="column" gap="1" mt="2">
                        {navItems.map((item) => (
                            <SidebarLink key={item.to} {...item} />
                        ))}
                    </Flex>
                </Box>

                {/* User menu */}
                <Box p="3" style={{ borderTop: '1px solid #27272a' }}>
                    <DropdownMenu.Root>
                        <DropdownMenu.Trigger>
                            <button
                                style={{
                                    width: '100%',
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 8,
                                    padding: '8px 10px',
                                    borderRadius: 6,
                                    border: 'none',
                                    background: 'transparent',
                                    color: '#d4d4d8',
                                    cursor: 'pointer',
                                    fontSize: 13,
                                }}
                            >
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
                    background: '#09090b',
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
                            color: '#3f3f46',
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
