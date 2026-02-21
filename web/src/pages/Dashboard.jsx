import { useState, useEffect } from 'react'
import { Box, Card, Flex, Grid, Heading, Text, Badge, Spinner } from '@radix-ui/themes'
import { Globe, Activity, Shield, Server } from 'lucide-react'
import { hostAPI, caddyAPI } from '../api/index.js'

function StatCard({ icon: Icon, label, value, color = 'green', loading }) {
    return (
        <Card
            style={{
                background: '#111113',
                border: '1px solid #1e1e22',
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
                        <Text size="5" weight="bold" style={{ color: '#fafafa', display: 'block' }}>
                            {value}
                        </Text>
                    )}
                </Box>
            </Flex>
        </Card>
    )
}

export default function Dashboard() {
    const [stats, setStats] = useState({ total: 0, active: 0 })
    const [caddyStatus, setCaddyStatus] = useState(null)
    const [loading, setLoading] = useState(true)

    useEffect(() => {
        async function fetchData() {
            try {
                const [hostsRes, statusRes] = await Promise.all([
                    hostAPI.list(),
                    caddyAPI.status(),
                ])

                const hosts = hostsRes.data.hosts || []
                setStats({
                    total: hosts.length,
                    active: hosts.filter((h) => h.enabled).length,
                })
                setCaddyStatus(statusRes.data)
            } catch (err) {
                console.error('Failed to fetch dashboard data:', err)
            } finally {
                setLoading(false)
            }
        }
        fetchData()
    }, [])

    return (
        <Box>
            <Heading size="6" mb="1" style={{ color: '#fafafa' }}>
                Dashboard
            </Heading>
            <Text size="2" color="gray" mb="5" as="p">
                Overview of your CaddyPanel instance
            </Text>

            <Grid columns={{ initial: '1', sm: '2', md: '4' }} gap="4" mb="6">
                <StatCard
                    icon={Globe}
                    label="Total Hosts"
                    value={stats.total}
                    color="green"
                    loading={loading}
                />
                <StatCard
                    icon={Activity}
                    label="Active Hosts"
                    value={stats.active}
                    color="blue"
                    loading={loading}
                />
                <StatCard
                    icon={Shield}
                    label="TLS Enabled"
                    value={
                        loading ? '-' : stats.active // simplified for MVP
                    }
                    color="violet"
                    loading={loading}
                />
                <StatCard
                    icon={Server}
                    label="Caddy Status"
                    value={
                        caddyStatus?.running ? (
                            <Badge color="green" size="2">Running</Badge>
                        ) : (
                            <Badge color="red" size="2">Stopped</Badge>
                        )
                    }
                    color="orange"
                    loading={loading}
                />
            </Grid>

            {/* Caddy Info */}
            {caddyStatus && (
                <Card style={{ background: '#111113', border: '1px solid #1e1e22' }}>
                    <Heading size="3" mb="3">
                        Server Info
                    </Heading>
                    <Grid columns="2" gap="3" style={{ maxWidth: 400 }}>
                        <Text size="2" color="gray">Status</Text>
                        <Badge color={caddyStatus.running ? 'green' : 'red'}>
                            {caddyStatus.running ? 'Running' : 'Stopped'}
                        </Badge>

                        {caddyStatus.version && (
                            <>
                                <Text size="2" color="gray">Caddy Version</Text>
                                <Text size="2">{caddyStatus.version}</Text>
                            </>
                        )}

                        <Text size="2" color="gray">Config Path</Text>
                        <Text size="2" style={{ wordBreak: 'break-all' }}>
                            {caddyStatus.caddyfile_path}
                        </Text>
                    </Grid>
                </Card>
            )}
        </Box>
    )
}
