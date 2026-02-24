import { useState, useEffect } from 'react'
import { Box, Card, Flex, Grid, Heading, Text, Badge, Spinner, Separator, Tooltip } from '@radix-ui/themes'
import {
    Globe, Activity, Shield, Server, ArrowRightLeft, Lock,
    ShieldCheck, ShieldOff, KeyRound, Monitor, Cpu,
} from 'lucide-react'
import { dashboardAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

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
    const { t } = useTranslation()
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
                {t('dashboard.title')}
            </Heading>
            <Text size="2" color="gray" mb="5" as="p">
                {t('dashboard.subtitle')}
            </Text>

            {/* Primary Stats Row */}
            <Grid columns={{ initial: '1', sm: '2', md: '4' }} gap="4" mb="5">
                <StatCard
                    icon={Globe}
                    label={t('dashboard.total_hosts')}
                    value={hosts.total ?? '-'}
                    color="green"
                    loading={loading}
                    tooltip={`${t('dashboard.proxy_count', { count: hosts.proxy ?? 0 })} / ${t('dashboard.redirect_count', { count: hosts.redirect ?? 0 })}`}
                />
                <StatCard
                    icon={Activity}
                    label={t('dashboard.active')}
                    value={hosts.active ?? '-'}
                    color="blue"
                    loading={loading}
                    tooltip={t('dashboard.disabled_count', { count: hosts.disabled ?? 0 })}
                />
                <StatCard
                    icon={Shield}
                    label={t('dashboard.https_protected')}
                    value={loading ? '-' : (tls.auto + tls.custom)}
                    color="violet"
                    loading={loading}
                    tooltip={`${t('dashboard.le_count', { count: tls.auto ?? 0 })} / ${t('dashboard.custom_cert_count', { count: tls.custom ?? 0 })} / ${t('dashboard.no_tls_count', { count: tls.none ?? 0 })}`}
                />
                <StatCard
                    icon={Server}
                    label={t('dashboard.caddy_status')}
                    value={
                        caddy?.running ? (
                            <Badge color="green" size="2">{t('dashboard.running')}</Badge>
                        ) : (
                            <Badge color="red" size="2">{t('dashboard.stopped')}</Badge>
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
                        <Heading size="3">{t('dashboard.host_distribution')}</Heading>
                    </Flex>
                    <InfoRow label={t('dashboard.reverse_proxy')} value={hosts.proxy ?? 0} />
                    <InfoRow label={t('dashboard.redirect')} value={hosts.redirect ?? 0} />
                    <InfoRow label={t('dashboard.enabled')} value={hosts.active ?? 0} color="green" />
                    <InfoRow label={t('dashboard.disabled')} value={hosts.disabled ?? 0} color="gray" />
                </Card>

                {/* TLS Breakdown */}
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2" mb="3">
                        <ShieldCheck size={16} style={{ color: 'var(--violet-9)' }} />
                        <Heading size="3">{t('dashboard.tls_status')}</Heading>
                    </Flex>
                    <InfoRow
                        label={t('dashboard.lets_encrypt_auto')}
                        value={tls.auto ?? 0}
                        color="green"
                    />
                    <InfoRow
                        label={t('dashboard.custom_cert')}
                        value={tls.custom ?? 0}
                        color="blue"
                    />
                    <InfoRow
                        label={t('dashboard.no_tls')}
                        value={tls.none ?? 0}
                        color={tls.none > 0 ? 'orange' : 'gray'}
                    />
                    <InfoRow
                        label={t('dashboard.basic_auth_protected')}
                        value={security.with_auth ?? 0}
                        color="violet"
                    />
                </Card>

                {/* System Info */}
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2" mb="3">
                        <Monitor size={16} style={{ color: 'var(--orange-9)' }} />
                        <Heading size="3">{t('dashboard.system_info')}</Heading>
                    </Flex>
                    <InfoRow label="CaddyPanel" value={`v${system.panel_version || '-'}`} />
                    <InfoRow
                        label="Caddy"
                        value={caddy?.version || t('common.unknown')}
                    />
                    <InfoRow
                        label={t('dashboard.caddy_status')}
                        value={caddy?.running ? t('dashboard.running') : t('dashboard.stopped')}
                        color={caddy?.running ? 'green' : 'red'}
                    />
                    <InfoRow label="Go" value={system.go_version || '-'} />
                    <InfoRow label={t('dashboard.platform')} value={`${system.go_os || '-'}/${system.go_arch || '-'}`} />
                </Card>
            </Grid>
        </Box>
    )
}
