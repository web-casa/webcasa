import { useState, useEffect } from 'react'
import { Box, Card, Flex, Grid, Heading, Text, Badge, Spinner, Tooltip } from '@radix-ui/themes'
import {
    Globe, Container, Package, Cpu, Monitor, Clock,
    Server, ArrowUpRight, Plus, Terminal, FolderOpen,
    ArrowUp, ArrowDown, ExternalLink,
} from 'lucide-react'
import {
    dashboardAPI, dockerAPI, pluginAPI, monitoringAPI,
    appstoreAPI, auditAPI,
} from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

// ── Helpers ──

function formatBytes(bytes) {
    if (bytes == null) return '-'
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

function formatUptime(seconds) {
    if (!seconds) return '-'
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    if (days > 0) return `${days}d ${hours}h`
    const mins = Math.floor((seconds % 3600) / 60)
    return `${hours}h ${mins}m`
}

function timeAgo(dateStr) {
    const now = new Date()
    const d = new Date(dateStr)
    const diff = Math.floor((now - d) / 1000)
    if (diff < 60) return `${diff}s`
    if (diff < 3600) return `${Math.floor(diff / 60)}m`
    if (diff < 86400) return `${Math.floor(diff / 3600)}h`
    return `${Math.floor(diff / 86400)}d`
}

function progressColor(pct) {
    if (pct < 60) return 'var(--green-9)'
    if (pct < 85) return 'var(--amber-9)'
    return 'var(--red-9)'
}

// ── Sub-components ──

function StatCard({ icon: Icon, label, value, color = 'green', loading, tooltip }) {
    const card = (
        <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
            <Flex align="center" gap="4" p="1">
                <Flex
                    align="center"
                    justify="center"
                    style={{
                        width: 44, height: 44, borderRadius: 10, flexShrink: 0,
                        background: `color-mix(in srgb, var(--${color}-9) 15%, transparent)`,
                    }}
                >
                    <Icon size={22} style={{ color: `var(--${color}-9)` }} />
                </Flex>
                <Box>
                    <Text size="1" color="gray" weight="medium">{label}</Text>
                    {loading ? (
                        <Spinner size="1" />
                    ) : (
                        <Text size="5" weight="bold" style={{ color: 'var(--cp-text)', display: 'block' }}>
                            {value}
                        </Text>
                    )}
                </Box>
            </Flex>
        </Card>
    )
    if (tooltip) return <Tooltip content={tooltip}>{card}</Tooltip>
    return card
}

function InfoRow({ label, value, color }) {
    return (
        <Flex justify="between" align="center" py="2" style={{ borderBottom: '1px solid var(--cp-border-subtle)' }}>
            <Text size="2" color="gray">{label}</Text>
            {color ? (
                <Badge color={color} size="1">{value}</Badge>
            ) : (
                <Text size="2" style={{ color: 'var(--cp-text)' }}>{value}</Text>
            )}
        </Flex>
    )
}

function ProgressBar({ label, percent, detail }) {
    const pct = Math.min(100, Math.max(0, percent || 0))
    return (
        <Box mb="3">
            <Flex justify="between" mb="1">
                <Text size="2" color="gray">{label}</Text>
                <Text size="2" style={{ color: 'var(--cp-text)' }}>{detail || `${pct.toFixed(1)}%`}</Text>
            </Flex>
            <div style={{
                height: 8, borderRadius: 4, background: 'var(--cp-border)',
                overflow: 'hidden',
            }}>
                <div style={{
                    height: '100%', width: `${pct}%`, borderRadius: 4,
                    background: progressColor(pct),
                    transition: 'width 0.3s ease',
                }} />
            </div>
        </Box>
    )
}

function QuickActionCard({ icon: Icon, label, color, onClick }) {
    return (
        <Card
            style={{
                background: 'var(--cp-card)', border: '1px solid var(--cp-border)',
                cursor: 'pointer', transition: 'border-color 0.2s',
            }}
            onMouseEnter={e => e.currentTarget.style.borderColor = `var(--${color}-9)`}
            onMouseLeave={e => e.currentTarget.style.borderColor = 'var(--cp-border)'}
            onClick={onClick}
        >
            <Flex align="center" gap="3" p="1">
                <Flex
                    align="center" justify="center"
                    style={{
                        width: 36, height: 36, borderRadius: 8, flexShrink: 0,
                        background: `color-mix(in srgb, var(--${color}-9) 15%, transparent)`,
                    }}
                >
                    <Icon size={18} style={{ color: `var(--${color}-9)` }} />
                </Flex>
                <Text size="2" weight="medium" style={{ color: 'var(--cp-text)' }}>{label}</Text>
                <Box style={{ marginLeft: 'auto' }}>
                    <ArrowUpRight size={14} style={{ color: 'var(--gray-8)' }} />
                </Box>
            </Flex>
        </Card>
    )
}

// ── Main Component ──

export default function Dashboard() {
    const { t } = useTranslation()
    const navigate = useNavigate()

    const [stats, setStats] = useState(null)
    const [loading, setLoading] = useState(true)
    const [plugins, setPlugins] = useState([])
    const [monitoring, setMonitoring] = useState(null)
    const [containers, setContainers] = useState(null)
    const [installedApps, setInstalledApps] = useState(null)
    const [appUpdates, setAppUpdates] = useState(null)
    const [auditLogs, setAuditLogs] = useState(null)
    const [news, setNews] = useState(null)

    const enabled = (id) => plugins.some(p => p.id === id && p.enabled)

    useEffect(() => {
        async function fetchData() {
            // Batch 1: essential data
            const [statsRes, pluginsRes] = await Promise.all([
                dashboardAPI.stats().catch(() => null),
                pluginAPI.list().catch(() => null),
            ])

            if (statsRes) setStats(statsRes.data)
            setLoading(false)

            const pluginList = pluginsRes?.data?.plugins || []
            setPlugins(pluginList)
            const isEnabled = (id) => pluginList.some(p => p.id === id && p.enabled)

            // Batch 2: plugin data (non-blocking)
            if (isEnabled('monitoring')) {
                monitoringAPI.getCurrent()
                    .then(res => setMonitoring(res.data))
                    .catch(() => {})
            }
            if (isEnabled('docker')) {
                dockerAPI.listContainers(true)
                    .then(res => setContainers(res.data?.containers || []))
                    .catch(() => {})
            }
            if (isEnabled('appstore')) {
                Promise.all([
                    appstoreAPI.listInstalled().catch(() => null),
                    appstoreAPI.checkUpdates().catch(() => null),
                ]).then(([installed, updates]) => {
                    if (installed) setInstalledApps(installed.data?.apps || [])
                    if (updates) setAppUpdates(updates.data?.updates || [])
                })
            }

            // Batch 3: independent data
            dashboardAPI.news().then(res => setNews(res.data || [])).catch(() => setNews([]))
            auditAPI.list({ per_page: 5 }).then(res => setAuditLogs(res.data?.logs || [])).catch(() => setAuditLogs([]))
        }
        fetchData()
    }, [])

    const hosts = stats?.hosts || {}
    const system = stats?.system || {}
    const caddy = stats?.caddy || {}

    // Compute stat card values
    const runningContainers = containers ? containers.filter(c => c.state === 'running').length : null
    const totalContainers = containers ? containers.length : null
    const totalApps = installedApps ? installedApps.length : null
    const updatableApps = appUpdates ? appUpdates.length : null

    // Monitoring data — backend returns flat fields (mem_percent, disk_percent, etc.)
    const mon = monitoring
    const cpuPct = mon?.cpu_percent ?? null
    const memPct = mon?.mem_percent ?? null
    const memUsed = mon?.mem_used
    const memTotal = mon?.mem_total
    const diskPct = mon?.disk_percent ?? null
    const diskUsed = mon?.disk_used
    const diskTotal = mon?.disk_total
    const swapPct = mon?.swap_total > 0 ? (mon?.swap_used / mon?.swap_total * 100) : null
    const swapUsed = mon?.swap_used
    const swapTotal = mon?.swap_total
    const netSent = mon?.net_sent_bytes
    const netRecv = mon?.net_recv_bytes

    const showAppsRow = enabled('appstore') || enabled('docker')
    const showApps = enabled('appstore')
    const showContainers = enabled('docker')

    return (
        <Box>
            <Heading size="6" mb="1" style={{ color: 'var(--cp-text)' }}>
                {t('dashboard.title')}
            </Heading>
            <Text size="2" color="gray" mb="5" as="p">
                {t('dashboard.subtitle')}
            </Text>

            {/* ── Row 1: Core Stats ── */}
            <Grid columns={{ initial: '1', sm: '2', md: '4' }} gap="4" mb="5">
                <StatCard
                    icon={Globe}
                    label={t('dashboard.sites')}
                    value={hosts.total ?? '-'}
                    color="green"
                    loading={loading}
                    tooltip={`${t('dashboard.proxy_count', { count: hosts.proxy ?? 0 })} / ${t('dashboard.redirect_count', { count: hosts.redirect ?? 0 })}`}
                />
                <StatCard
                    icon={Container}
                    label={t('dashboard.containers')}
                    value={runningContainers != null ? `${runningContainers} / ${totalContainers}` : '-'}
                    color="cyan"
                    loading={loading}
                    tooltip={!enabled('docker') ? t('dashboard.plugin_not_enabled', { plugin: 'Docker' }) : undefined}
                />
                <StatCard
                    icon={Package}
                    label={t('dashboard.apps')}
                    value={totalApps != null ? totalApps : '-'}
                    color="violet"
                    loading={loading}
                    tooltip={
                        !enabled('appstore')
                            ? t('dashboard.plugin_not_enabled', { plugin: 'App Store' })
                            : updatableApps > 0
                                ? t('dashboard.updates_available', { count: updatableApps })
                                : undefined
                    }
                />
                <StatCard
                    icon={Cpu}
                    label={t('dashboard.system_load')}
                    value={
                        cpuPct != null && memPct != null
                            ? `${cpuPct.toFixed(0)}% / ${memPct.toFixed(0)}%`
                            : '-'
                    }
                    color="orange"
                    loading={loading}
                    tooltip={
                        !enabled('monitoring')
                            ? t('dashboard.plugin_not_enabled', { plugin: 'Monitoring' })
                            : mon?.load
                                ? `Load: ${mon.load.load1?.toFixed(2)} ${mon.load.load5?.toFixed(2)} ${mon.load.load15?.toFixed(2)}`
                                : undefined
                    }
                />
            </Grid>

            {/* ── Row 2: System Resources + OS Info ── */}
            <Grid columns={{ initial: '1', md: '3' }} gap="4" mb="5">
                {/* System Resources (2/3) */}
                <Box style={{ gridColumn: 'span 2' }}>
                    <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', height: '100%' }}>
                        <Flex align="center" gap="2" mb="4">
                            <Monitor size={16} style={{ color: 'var(--blue-9)' }} />
                            <Heading size="3">{t('dashboard.system_resources')}</Heading>
                        </Flex>
                        {enabled('monitoring') && mon ? (
                            <Box>
                                <ProgressBar
                                    label={t('dashboard.cpu_usage')}
                                    percent={cpuPct}
                                    detail={`${cpuPct?.toFixed(1)}%`}
                                />
                                <ProgressBar
                                    label={t('dashboard.memory_usage')}
                                    percent={memPct}
                                    detail={memPct != null ? `${memPct.toFixed(1)}% — ${formatBytes(memUsed)} / ${formatBytes(memTotal)}` : '-'}
                                />
                                <ProgressBar
                                    label={t('dashboard.disk_usage')}
                                    percent={diskPct}
                                    detail={diskPct != null ? `${diskPct.toFixed(1)}% — ${formatBytes(diskUsed)} / ${formatBytes(diskTotal)}` : '-'}
                                />
                                {swapTotal > 0 && (
                                    <ProgressBar
                                        label={t('dashboard.swap_usage')}
                                        percent={swapPct}
                                        detail={swapPct != null ? `${swapPct.toFixed(1)}% — ${formatBytes(swapUsed)} / ${formatBytes(swapTotal)}` : '-'}
                                    />
                                )}
                                <Flex gap="5" mt="2">
                                    <Flex align="center" gap="2">
                                        <ArrowUp size={14} style={{ color: 'var(--green-9)' }} />
                                        <Text size="2" color="gray">{t('dashboard.sent')}</Text>
                                        <Text size="2" weight="medium" style={{ color: 'var(--cp-text)' }}>{formatBytes(netSent)}</Text>
                                    </Flex>
                                    <Flex align="center" gap="2">
                                        <ArrowDown size={14} style={{ color: 'var(--blue-9)' }} />
                                        <Text size="2" color="gray">{t('dashboard.received')}</Text>
                                        <Text size="2" weight="medium" style={{ color: 'var(--cp-text)' }}>{formatBytes(netRecv)}</Text>
                                    </Flex>
                                </Flex>
                            </Box>
                        ) : (
                            <Flex align="center" justify="center" direction="column" gap="2" py="6">
                                <Monitor size={32} style={{ color: 'var(--gray-7)' }} />
                                <Text size="2" color="gray">{t('dashboard.enable_monitoring_hint')}</Text>
                            </Flex>
                        )}
                    </Card>
                </Box>

                {/* OS Info (1/3) */}
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2" mb="3">
                        <Server size={16} style={{ color: 'var(--orange-9)' }} />
                        <Heading size="3">{t('dashboard.os_info')}</Heading>
                    </Flex>
                    <InfoRow label={t('dashboard.hostname')} value={system.hostname || '-'} />
                    <InfoRow
                        label={t('dashboard.os')}
                        value={system.os_name ? `${system.os_name} ${system.os_version || ''}`.trim() : `${system.go_os || '-'}/${system.go_arch || '-'}`}
                    />
                    <InfoRow label={t('dashboard.kernel')} value={system.kernel || '-'} />
                    <InfoRow label={t('dashboard.cpu_model')} value={system.cpu_model || '-'} />
                    <InfoRow label={t('dashboard.cpu_cores')} value={system.cpu_cores ?? '-'} />
                    <InfoRow label={t('dashboard.uptime')} value={formatUptime(system.uptime)} />
                    <InfoRow label="WebCasa" value={`v${system.panel_version || '-'}`} />
                    <InfoRow
                        label="Caddy"
                        value={
                            <Flex align="center" gap="2">
                                <Text size="2">{caddy?.version || '-'}</Text>
                                {caddy?.running
                                    ? <Badge color="green" size="1">{t('dashboard.running')}</Badge>
                                    : <Badge color="red" size="1">{t('dashboard.stopped')}</Badge>
                                }
                            </Flex>
                        }
                    />
                </Card>
            </Grid>

            {/* ── Row 3: Installed Apps + Container Overview ── */}
            {showAppsRow && (
                <Grid columns={{ initial: '1', md: '2' }} gap="4" mb="5">
                    {/* Installed Apps */}
                    {showApps && (
                        <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                            <Flex align="center" justify="between" mb="3">
                                <Flex align="center" gap="2">
                                    <Package size={16} style={{ color: 'var(--violet-9)' }} />
                                    <Heading size="3">{t('dashboard.installed_apps')}</Heading>
                                </Flex>
                                <Text
                                    size="1" color="blue" weight="medium"
                                    style={{ cursor: 'pointer' }}
                                    onClick={() => navigate('/store?tab=installed')}
                                >
                                    {t('dashboard.view_all')} →
                                </Text>
                            </Flex>
                            {installedApps && installedApps.length > 0 ? (
                                <Grid columns="3" gap="3">
                                    {installedApps.slice(0, 6).map(app => (
                                        <Flex key={app.id} align="center" gap="2" py="2" px="2"
                                            style={{
                                                borderRadius: 8, border: '1px solid var(--cp-border-subtle)',
                                                cursor: 'pointer',
                                            }}
                                            onClick={() => navigate(`/store?tab=installed&app=${app.id}`)}
                                        >
                                            <img
                                                src={`/api/plugins/appstore/apps/${app.app_id}/logo`}
                                                alt={app.name}
                                                style={{ width: 24, height: 24, borderRadius: 4, objectFit: 'contain' }}
                                                onError={e => { e.target.style.display = 'none' }}
                                            />
                                            <Box style={{ minWidth: 0, flex: 1 }}>
                                                <Text size="1" weight="medium" style={{ color: 'var(--cp-text)', display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                                    {app.name}
                                                </Text>
                                            </Box>
                                            <Badge
                                                size="1"
                                                color={app.status === 'running' ? 'green' : app.status === 'stopped' ? 'red' : 'gray'}
                                            >
                                                {app.status === 'running' ? '●' : '○'}
                                            </Badge>
                                        </Flex>
                                    ))}
                                </Grid>
                            ) : installedApps ? (
                                <Flex align="center" justify="center" direction="column" gap="2" py="4">
                                    <Text size="2" color="gray">{t('dashboard.no_apps_installed')}</Text>
                                    <Text
                                        size="1" color="blue" weight="medium"
                                        style={{ cursor: 'pointer' }}
                                        onClick={() => navigate('/store')}
                                    >
                                        {t('dashboard.explore_appstore')} →
                                    </Text>
                                </Flex>
                            ) : (
                                <Flex align="center" justify="center" py="4"><Spinner size="2" /></Flex>
                            )}
                        </Card>
                    )}

                    {/* Container Overview */}
                    {showContainers && (
                        <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                            <Flex align="center" justify="between" mb="3">
                                <Flex align="center" gap="2">
                                    <Container size={16} style={{ color: 'var(--cyan-9)' }} />
                                    <Heading size="3">{t('dashboard.container_overview')}</Heading>
                                </Flex>
                                <Text
                                    size="1" color="blue" weight="medium"
                                    style={{ cursor: 'pointer' }}
                                    onClick={() => navigate('/docker?tab=containers')}
                                >
                                    {t('dashboard.view_all')} →
                                </Text>
                            </Flex>
                            {containers ? (
                                containers.length > 0 ? (
                                    <Box>
                                        {containers.slice(0, 5).map(c => {
                                            const stateColor = {
                                                running: 'green', exited: 'red', paused: 'amber', created: 'gray',
                                            }[c.state] || 'gray'
                                            const name = (c.names?.[0] || c.name || c.id?.slice(0, 12) || '').replace(/^\//, '')
                                            return (
                                                <Flex key={c.id} justify="between" align="center" py="2"
                                                    style={{ borderBottom: '1px solid var(--cp-border-subtle)' }}
                                                >
                                                    <Text size="2" style={{ color: 'var(--cp-text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '70%' }}>
                                                        {name}
                                                    </Text>
                                                    <Badge color={stateColor} size="1">{c.state}</Badge>
                                                </Flex>
                                            )
                                        })}
                                    </Box>
                                ) : (
                                    <Text size="2" color="gray" style={{ display: 'block', textAlign: 'center', padding: '16px 0' }}>
                                        {t('dashboard.not_available')}
                                    </Text>
                                )
                            ) : (
                                <Flex align="center" justify="center" py="4"><Spinner size="2" /></Flex>
                            )}
                        </Card>
                    )}
                </Grid>
            )}

            {/* ── Row 4: Recent Activity + News ── */}
            <Grid columns={{ initial: '1', md: '2' }} gap="4" mb="5">
                {/* Recent Activity */}
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2" mb="3">
                        <Clock size={16} style={{ color: 'var(--blue-9)' }} />
                        <Heading size="3">{t('dashboard.recent_activity')}</Heading>
                    </Flex>
                    {auditLogs ? (
                        auditLogs.length > 0 ? (
                            <Box>
                                {auditLogs.map((log, i) => (
                                    <Flex key={log.id || i} justify="between" align="center" py="2"
                                        style={{ borderBottom: '1px solid var(--cp-border-subtle)' }}
                                    >
                                        <Box style={{ minWidth: 0, flex: 1 }}>
                                            <Text size="2" style={{ color: 'var(--cp-text)' }}>
                                                {log.action}
                                            </Text>
                                            <Text size="1" color="gray"> — {log.username}</Text>
                                        </Box>
                                        <Text size="1" color="gray" style={{ flexShrink: 0, marginLeft: 8 }}>
                                            {t('dashboard.time_ago', { time: timeAgo(log.created_at) })}
                                        </Text>
                                    </Flex>
                                ))}
                            </Box>
                        ) : (
                            <Text size="2" color="gray" style={{ display: 'block', textAlign: 'center', padding: '16px 0' }}>
                                {t('dashboard.no_recent_activity')}
                            </Text>
                        )
                    ) : (
                        <Flex align="center" justify="center" py="4"><Spinner size="2" /></Flex>
                    )}
                </Card>

                {/* Official News */}
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2" mb="3">
                        <ExternalLink size={16} style={{ color: 'var(--green-9)' }} />
                        <Heading size="3">{t('dashboard.official_news')}</Heading>
                    </Flex>
                    {news ? (
                        news.length > 0 ? (
                            <Box>
                                {news.slice(0, 5).map((item, i) => {
                                    const typeColor = {
                                        release: 'green', announcement: 'blue', security: 'red',
                                    }[item.type] || 'gray'
                                    return (
                                        <Flex key={i} justify="between" align="center" py="2"
                                            style={{ borderBottom: '1px solid var(--cp-border-subtle)' }}
                                        >
                                            <Box style={{ minWidth: 0, flex: 1 }}>
                                                <Flex align="center" gap="2">
                                                    <Badge color={typeColor} size="1">{item.type}</Badge>
                                                    {item.url ? (
                                                        <a href={item.url} target="_blank" rel="noopener noreferrer"
                                                            style={{ color: 'var(--cp-text)', textDecoration: 'none', fontSize: 'var(--font-size-2)' }}
                                                            onMouseEnter={e => e.target.style.textDecoration = 'underline'}
                                                            onMouseLeave={e => e.target.style.textDecoration = 'none'}
                                                        >
                                                            {item.title}
                                                        </a>
                                                    ) : (
                                                        <Text size="2" style={{ color: 'var(--cp-text)' }}>{item.title}</Text>
                                                    )}
                                                </Flex>
                                            </Box>
                                            <Text size="1" color="gray" style={{ flexShrink: 0, marginLeft: 8 }}>
                                                {item.date}
                                            </Text>
                                        </Flex>
                                    )
                                })}
                            </Box>
                        ) : (
                            <Text size="2" color="gray" style={{ display: 'block', textAlign: 'center', padding: '16px 0' }}>
                                {t('dashboard.news_unavailable')}
                            </Text>
                        )
                    ) : (
                        <Flex align="center" justify="center" py="4"><Spinner size="2" /></Flex>
                    )}
                </Card>
            </Grid>

            {/* ── Row 5: Quick Actions ── */}
            <Grid columns={{ initial: '2', md: '4' }} gap="4">
                <QuickActionCard
                    icon={Plus}
                    label={t('dashboard.new_site')}
                    color="green"
                    onClick={() => navigate('/hosts')}
                />
                <QuickActionCard
                    icon={Package}
                    label={t('dashboard.install_app')}
                    color="violet"
                    onClick={() => navigate('/store')}
                />
                <QuickActionCard
                    icon={Terminal}
                    label={t('dashboard.open_terminal')}
                    color="amber"
                    onClick={() => navigate('/terminal')}
                />
                <QuickActionCard
                    icon={FolderOpen}
                    label={t('dashboard.file_manager')}
                    color="blue"
                    onClick={() => navigate('/files')}
                />
            </Grid>
        </Box>
    )
}
