import { NavLink, Outlet, useNavigate } from 'react-router'
import { Box, Flex, Text, Button, DropdownMenu, Separator, Tooltip } from '@radix-ui/themes'
import {
    Zap,
    LayoutDashboard,
    Globe,
    FileText,
    Settings,
    LogOut,
    User,
    ChevronDown,
} from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'

const navItems = [
    { to: '/', icon: LayoutDashboard, label: 'Dashboard', end: true },
    { to: '/hosts', icon: Globe, label: 'Proxy Hosts' },
    { to: '/logs', icon: FileText, label: 'Logs' },
    { to: '/settings', icon: Settings, label: 'Settings' },
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

    const handleLogout = () => {
        logout()
        navigate('/login', { replace: true })
    }

    return (
        <Flex style={{ minHeight: '100vh' }}>
            {/* Sidebar */}
            <Box
                style={{
                    width: 240,
                    minWidth: 240,
                    borderRight: '1px solid #1e1e22',
                    background: '#111113',
                    display: 'flex',
                    flexDirection: 'column',
                }}
            >
                {/* Logo */}
                <Flex align="center" gap="2" p="4" pb="2">
                    <Flex
                        align="center"
                        justify="center"
                        style={{
                            width: 32,
                            height: 32,
                            borderRadius: 8,
                            background: 'linear-gradient(135deg, #10b981, #059669)',
                        }}
                    >
                        <Zap size={16} color="white" />
                    </Flex>
                    <Text size="3" weight="bold" style={{ color: '#fafafa' }}>
                        CaddyPanel
                    </Text>
                </Flex>

                <Separator size="4" style={{ opacity: 0.15 }} />

                {/* Nav links */}
                <Flex direction="column" gap="1" p="3" style={{ flex: 1 }}>
                    {navItems.map((item) => (
                        <SidebarLink key={item.to} {...item} />
                    ))}
                </Flex>

                {/* User section */}
                <Box p="3" style={{ borderTop: '1px solid #1e1e22' }}>
                    <DropdownMenu.Root>
                        <DropdownMenu.Trigger>
                            <button
                                style={{
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 8,
                                    width: '100%',
                                    padding: '8px 12px',
                                    border: 'none',
                                    borderRadius: 8,
                                    background: 'rgba(255,255,255,0.03)',
                                    color: '#a1a1aa',
                                    cursor: 'pointer',
                                    fontSize: '0.85rem',
                                    transition: 'background 0.15s',
                                }}
                                onMouseEnter={(e) => (e.currentTarget.style.background = 'rgba(255,255,255,0.06)')}
                                onMouseLeave={(e) => (e.currentTarget.style.background = 'rgba(255,255,255,0.03)')}
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
                }}
            >
                <Box p="5" style={{ maxWidth: 1200, margin: '0 auto' }}>
                    <Outlet />
                </Box>
            </Box>
        </Flex>
    )
}
