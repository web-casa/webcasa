import { useState, useEffect } from 'react'
import { Box, Card, Flex, Grid, Heading, Text, Badge, Spinner, Separator, Tooltip } from '@radix-ui/themes'
import {
    Globe, Activity, Shield, Server, ArrowRightLeft, Lock,
    ShieldCheck, ShieldOff, KeyRound, Monitor, Cpu,
} from 'lucide-react'
import { dashboardAPI } from '../api/index.js'

function StatCard({ icon: Icon, label, value, color = 'green', loading, tooltip }) {
    const card = (
        <Card
            style={{
                background: 'var(--cp-card)',
                border: '1px solid var(--cp-border)',
            }}
        >
            <Flex align="center" gap="4" p="1">
                <Flex
                    align="center"
                    justify="center"
                    style={{
                        width: 44,
                        height: 44,
                        borderRadius: 10,
                        background: `color-mix(in srgb, var(--${color}-9) 15%, transparent)`,
                        flexShrink: 0,
                    }}
                >
                    <Icon size={22} style={{ color: `var(--${color}-9)` }} />
                </Flex>
                <Box>
                    <Text size="1" color="gray" weight="medium">
                        {label}
                    </Text>
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

    if (tooltip) {
        return <Tooltip content={tooltip}>{card}</Tooltip>
    }
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

export default function Dashboard() {
    const [stats, setStats] = useState(null)
    const [loading, setLoading] = useState(true)

    useEffect(() => {
        async function fetchData() {
            try {
                const res = await dashboardAPI.stats()
                setStats(res.data)
            } catch (err) {
                console.error('Failed to fetch dashboard stats:', err)
            } finally {
                setLoading(false)
            }
        }
        fetchData()
    }, [])

    const hosts = stats?.hosts || {}
    const tls = stats?.tls || {}
    const security = stats?.security || {}
    const system = stats?.system || {}
    const caddy = stats?.caddy || {}

    return (
        <Box>
            <Heading size="6" mb="1" style={{ color: 'var(--cp-text)' }}>
                Dashboard
            </Heading>
            <Text size="2" color="gray" mb="5" as="p">
                CaddyPanel 实例概览
            </Text>

            {/* Primary Stats Row */}
            <Grid columns={{ initial: '1', sm: '2', md: '4' }} gap="4" mb="5">
                <StatCard
                    icon={Globe}
                    label="站点总数"
                    value={hosts.total ?? '-'}
                    color="green"
                    loading={loading}
                    tooltip={`代理: ${hosts.proxy ?? 0} / 跳转: ${hosts.redirect ?? 0}`}
                />
                <StatCard
                    icon={Activity}
                    label="已启用"
                    value={hosts.active ?? '-'}
                    color="blue"
                    loading={loading}
                    tooltip={`停用: ${hosts.disabled ?? 0}`}
                />
                <StatCard
                    icon={Shield}
                    label="HTTPS 保护"
                    value={loading ? '-' : (tls.auto + tls.custom)}
                    color="violet"
                    loading={loading}
                    tooltip={`Let's Encrypt: ${tls.auto ?? 0} / 自定义证书: ${tls.custom ?? 0} / 无 TLS: ${tls.none ?? 0}`}
                />
                <StatCard
                    icon={Server}
                    label="Caddy 状态"
                    value={
                        caddy?.running ? (
                            <Badge color="green" size="2">运行中</Badge>
                        ) : (
                            <Badge color="red" size="2">已停止</Badge>
                        )
                    }
                    color="orange"
                    loading={loading}
                />
            </Grid>

            {/* Detail Cards */}
            <Grid columns={{ initial: '1', md: '3' }} gap="4" mb="5">
                {/* Host Breakdown */}
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2" mb="3">
                        <Globe size={16} style={{ color: 'var(--green-9)' }} />
                        <Heading size="3">站点分布</Heading>
                    </Flex>
                    <InfoRow label="反向代理" value={hosts.proxy ?? 0} />
                    <InfoRow label="域名跳转" value={hosts.redirect ?? 0} />
                    <InfoRow label="已启用" value={hosts.active ?? 0} color="green" />
                    <InfoRow label="已停用" value={hosts.disabled ?? 0} color="gray" />
                </Card>

                {/* TLS Breakdown */}
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2" mb="3">
                        <ShieldCheck size={16} style={{ color: 'var(--violet-9)' }} />
                        <Heading size="3">TLS 状态</Heading>
                    </Flex>
                    <InfoRow
                        label="Let's Encrypt (自动)"
                        value={tls.auto ?? 0}
                        color="green"
                    />
                    <InfoRow
                        label="自定义证书"
                        value={tls.custom ?? 0}
                        color="blue"
                    />
                    <InfoRow
                        label="未启用 TLS"
                        value={tls.none ?? 0}
                        color={tls.none > 0 ? 'orange' : 'gray'}
                    />
                    <InfoRow
                        label="Basic Auth 保护"
                        value={security.with_auth ?? 0}
                        color="violet"
                    />
                </Card>

                {/* System Info */}
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2" mb="3">
                        <Monitor size={16} style={{ color: 'var(--orange-9)' }} />
                        <Heading size="3">系统信息</Heading>
                    </Flex>
                    <InfoRow label="CaddyPanel" value={`v${system.panel_version || '-'}`} />
                    <InfoRow
                        label="Caddy"
                        value={caddy?.version || '未知'}
                    />
                    <InfoRow
                        label="Caddy 状态"
                        value={caddy?.running ? '运行中' : '已停止'}
                        color={caddy?.running ? 'green' : 'red'}
                    />
                    <InfoRow label="Go" value={system.go_version || '-'} />
                    <InfoRow label="平台" value={`${system.go_os || '-'}/${system.go_arch || '-'}`} />
                </Card>
            </Grid>
        </Box>
    )
}
