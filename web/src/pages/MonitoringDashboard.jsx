import { useState, useEffect, useRef, useCallback } from 'react'
import {
    Box, Flex, Text, Card, Badge, Button, Table, Dialog, TextField,
    Select, Switch, Tabs, ScrollArea, Heading,
} from '@radix-ui/themes'
import {
    Cpu, MemoryStick, HardDrive, Network, Plus, Trash2, RefreshCw,
    Bell, BellOff, AlertTriangle, CheckCircle, XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
    AreaChart, Area, LineChart, Line, XAxis, YAxis,
    CartesianGrid, Tooltip, ResponsiveContainer,
} from 'recharts'
import { monitoringAPI } from '../api/index.js'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatBytes(bytes) {
    if (bytes == null || isNaN(bytes)) return '0 B'
    if (bytes === 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024))
    const idx = Math.min(i, units.length - 1)
    return `${(bytes / Math.pow(1024, idx)).toFixed(idx === 0 ? 0 : 2)} ${units[idx]}`
}

function formatGiB(bytes) {
    if (bytes == null || isNaN(bytes)) return '0'
    return (bytes / (1024 * 1024 * 1024)).toFixed(1)
}

function formatTime(ts) {
    if (!ts) return ''
    const d = new Date(ts)
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function formatPercent(v) {
    if (v == null || isNaN(v)) return '0.0%'
    return `${Number(v).toFixed(1)}%`
}

function percentColor(v) {
    if (v >= 90) return 'red'
    if (v >= 70) return 'orange'
    return 'green'
}

// ---------------------------------------------------------------------------
// Metric Card
// ---------------------------------------------------------------------------

function MetricCard({ icon: Icon, title, percent, detail, color }) {
    return (
        <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
            <Flex direction="column" gap="2" p="1">
                <Flex align="center" gap="2">
                    <Flex
                        align="center" justify="center"
                        style={{
                            width: 36, height: 36, borderRadius: 8,
                            background: `color-mix(in srgb, ${color} 15%, transparent)`,
                            flexShrink: 0,
                        }}
                    >
                        <Icon size={18} style={{ color }} />
                    </Flex>
                    <Box style={{ flex: 1 }}>
                        <Text size="1" color="gray">{title}</Text>
                        <Text size="5" weight="bold" style={{ color: 'var(--cp-text)', display: 'block' }}>
                            {formatPercent(percent)}
                        </Text>
                    </Box>
                </Flex>
                {/* Progress bar */}
                <Box style={{
                    height: 6, borderRadius: 3,
                    background: 'var(--cp-border)',
                    overflow: 'hidden',
                }}>
                    <Box style={{
                        height: '100%', borderRadius: 3,
                        width: `${Math.min(percent || 0, 100)}%`,
                        background: color,
                        transition: 'width 0.4s ease',
                    }} />
                </Box>
                {detail && (
                    <Text size="1" style={{ color: 'var(--cp-text-muted)' }}>{detail}</Text>
                )}
            </Flex>
        </Card>
    )
}

// ---------------------------------------------------------------------------
// Chart wrapper
// ---------------------------------------------------------------------------

function ChartCard({ title, children }) {
    return (
        <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
            <Text size="2" weight="bold" mb="2" style={{ color: 'var(--cp-text)', display: 'block' }}>
                {title}
            </Text>
            <Box style={{ width: '100%', height: 220 }}>
                {children}
            </Box>
        </Card>
    )
}

// ---------------------------------------------------------------------------
// Alert form initial state
// ---------------------------------------------------------------------------

const emptyAlert = {
    name: '',
    metric: 'cpu_percent',
    operator: '>',
    threshold: 80,
    duration: 3,
    cooldown: 5,
    notify_type: 'webhook',
    notify_url: '',
    enabled: true,
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function MonitoringDashboard({ embedded }) {
    const { t } = useTranslation()

    // State
    const [period, setPeriod] = useState('1h')
    const [current, setCurrent] = useState(null)
    const [history, setHistory] = useState([])
    const [containers, setContainers] = useState([])
    const [alertRules, setAlertRules] = useState([])
    const [alertHistory, setAlertHistory] = useState([])
    const [dialogOpen, setDialogOpen] = useState(false)
    const [alertForm, setAlertForm] = useState({ ...emptyAlert })
    const [editingId, setEditingId] = useState(null)

    const wsRef = useRef(null)

    // -------------------------------------------------------------------
    // Data fetching
    // -------------------------------------------------------------------

    const fetchCurrent = useCallback(async () => {
        try {
            const res = await monitoringAPI.getCurrent()
            setCurrent(res.data)
        } catch (e) { console.error('monitoring current:', e) }
    }, [])

    const fetchHistory = useCallback(async (p) => {
        try {
            const res = await monitoringAPI.getHistory(p || period)
            setHistory(Array.isArray(res.data) ? res.data : [])
        } catch (e) { console.error('monitoring history:', e) }
    }, [period])

    const fetchContainers = useCallback(async () => {
        try {
            const res = await monitoringAPI.getContainers()
            setContainers(Array.isArray(res.data) ? res.data : [])
        } catch (e) { setContainers([]) }
    }, [])

    const fetchAlertRules = useCallback(async () => {
        try {
            const res = await monitoringAPI.listAlertRules()
            setAlertRules(Array.isArray(res.data) ? res.data : [])
        } catch (e) { console.error('alert rules:', e) }
    }, [])

    const fetchAlertHistory = useCallback(async () => {
        try {
            const res = await monitoringAPI.listAlertHistory(100)
            setAlertHistory(Array.isArray(res.data) ? res.data : [])
        } catch (e) { console.error('alert history:', e) }
    }, [])

    // -------------------------------------------------------------------
    // WebSocket
    // -------------------------------------------------------------------

    useEffect(() => {
        const url = monitoringAPI.metricsWsUrl()
        let ws
        let reconnectTimer

        function connect() {
            ws = new WebSocket(url)
            wsRef.current = ws

            ws.onmessage = (evt) => {
                try {
                    const data = JSON.parse(evt.data)
                    if (data.metrics) setCurrent(data.metrics)
                    if (data.containers) setContainers(data.containers)
                } catch { /* ignore parse errors */ }
            }

            ws.onclose = () => {
                reconnectTimer = setTimeout(connect, 5000)
            }

            ws.onerror = () => { ws.close() }
        }

        connect()

        return () => {
            clearTimeout(reconnectTimer)
            if (wsRef.current) {
                wsRef.current.onclose = null
                wsRef.current.close()
            }
        }
    }, [])

    // -------------------------------------------------------------------
    // Initial load
    // -------------------------------------------------------------------

    useEffect(() => {
        fetchCurrent()
        fetchHistory()
        fetchContainers()
        fetchAlertRules()
        fetchAlertHistory()
    }, []) // eslint-disable-line react-hooks/exhaustive-deps

    // Refetch history when period changes
    useEffect(() => {
        fetchHistory(period)
    }, [period]) // eslint-disable-line react-hooks/exhaustive-deps

    // -------------------------------------------------------------------
    // Alert CRUD
    // -------------------------------------------------------------------

    const handleSaveAlert = async () => {
        try {
            if (editingId) {
                await monitoringAPI.updateAlertRule(editingId, alertForm)
            } else {
                await monitoringAPI.createAlertRule(alertForm)
            }
            setDialogOpen(false)
            setAlertForm({ ...emptyAlert })
            setEditingId(null)
            fetchAlertRules()
        } catch (e) { console.error('save alert:', e) }
    }

    const handleDeleteAlert = async (id) => {
        try {
            await monitoringAPI.deleteAlertRule(id)
            fetchAlertRules()
        } catch (e) { console.error('delete alert:', e) }
    }

    const handleToggleAlert = async (rule) => {
        try {
            await monitoringAPI.updateAlertRule(rule.id, { ...rule, enabled: !rule.enabled })
            fetchAlertRules()
        } catch (e) { console.error('toggle alert:', e) }
    }

    const openEditAlert = (rule) => {
        setAlertForm({
            name: rule.name || '',
            metric: rule.metric || 'cpu_percent',
            operator: rule.operator || '>',
            threshold: rule.threshold ?? 80,
            duration: rule.duration ?? 3,
            cooldown: rule.cooldown ?? 5,
            notify_type: rule.notify_type || 'webhook',
            notify_url: rule.notify_url || '',
            enabled: rule.enabled !== false,
        })
        setEditingId(rule.id)
        setDialogOpen(true)
    }

    // -------------------------------------------------------------------
    // Derived data
    // -------------------------------------------------------------------

    const m = current || {}

    const periods = [
        { value: '1h', label: t('monitoring.period_1h') },
        { value: '6h', label: t('monitoring.period_6h') },
        { value: '24h', label: t('monitoring.period_24h') },
        { value: '7d', label: t('monitoring.period_7d') },
        { value: '30d', label: t('monitoring.period_30d') },
    ]

    const chartData = history.map((h) => ({
        time: formatTime(h.timestamp || h.ts),
        cpu: h.cpu_percent ?? 0,
        mem: h.mem_percent ?? 0,
        disk_read: h.disk_read_bytes ?? 0,
        disk_write: h.disk_write_bytes ?? 0,
        net_recv: h.net_recv_bytes ?? 0,
        net_sent: h.net_sent_bytes ?? 0,
    }))

    const metricOptions = [
        { value: 'cpu_percent', label: 'CPU %' },
        { value: 'mem_percent', label: 'Memory %' },
        { value: 'disk_percent', label: 'Disk %' },
        { value: 'load_1', label: 'Load 1m' },
        { value: 'net_recv_bytes', label: 'Net Recv' },
        { value: 'net_sent_bytes', label: 'Net Sent' },
    ]

    const operatorOptions = [
        { value: '>', label: '>' },
        { value: '>=', label: '>=' },
        { value: '<', label: '<' },
        { value: '<=', label: '<=' },
        { value: '==', label: '==' },
    ]

    // -------------------------------------------------------------------
    // Render
    // -------------------------------------------------------------------

    return (
        <Box>
            {/* Header */}
            {!embedded && (
                <>
                    <Heading size="6" mb="1" style={{ color: 'var(--cp-text)' }}>
                        {t('monitoring.title')}
                    </Heading>
                    <Text size="2" color="gray" mb="4" as="p">
                        {t('monitoring.subtitle')}
                    </Text>
                </>
            )}

            {/* Period selector */}
            <Flex gap="2" mb="4" wrap="wrap">
                {periods.map((p) => (
                    <Button
                        key={p.value}
                        size="1"
                        variant={period === p.value ? 'solid' : 'outline'}
                        onClick={() => setPeriod(p.value)}
                    >
                        {p.label}
                    </Button>
                ))}
                <Button size="1" variant="ghost" onClick={() => { fetchCurrent(); fetchHistory() }}>
                    <RefreshCw size={14} />
                </Button>
            </Flex>

            {/* Real-time metric cards */}
            <Box style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
                gap: 16,
                marginBottom: 24,
            }}>
                <MetricCard
                    icon={Cpu}
                    title={t('monitoring.cpu')}
                    percent={m.cpu_percent}
                    color="#3b82f6"
                />
                <MetricCard
                    icon={MemoryStick}
                    title={t('monitoring.memory')}
                    percent={m.mem_percent}
                    detail={`${formatGiB(m.mem_used)} / ${formatGiB(m.mem_total)} GiB`}
                    color="#10b981"
                />
                <MetricCard
                    icon={HardDrive}
                    title={t('monitoring.disk')}
                    percent={m.disk_percent}
                    detail={`${formatGiB(m.disk_used)} / ${formatGiB(m.disk_total)} GiB`}
                    color="#f59e0b"
                />
                <MetricCard
                    icon={Network}
                    title={t('monitoring.network')}
                    percent={null}
                    detail={`↓ ${formatBytes(m.net_recv_bytes)}/s  ↑ ${formatBytes(m.net_sent_bytes)}/s`}
                    color="#8b5cf6"
                />
            </Box>

            {/* Charts - 2 per row */}
            <Box style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fit, minmax(400px, 1fr))',
                gap: 16,
                marginBottom: 24,
            }}>
                {/* CPU chart */}
                <ChartCard title={t('monitoring.cpu_usage')}>
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData}>
                            <CartesianGrid strokeDasharray="3 3" stroke="var(--cp-border)" />
                            <XAxis dataKey="time" tick={{ fontSize: 11, fill: 'var(--cp-text-muted)' }} />
                            <YAxis domain={[0, 100]} tick={{ fontSize: 11, fill: 'var(--cp-text-muted)' }} unit="%" />
                            <Tooltip
                                contentStyle={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', borderRadius: 6 }}
                                labelStyle={{ color: 'var(--cp-text)' }}
                            />
                            <Area
                                type="monotone" dataKey="cpu" name="CPU"
                                stroke="#3b82f6" fill="#3b82f6" fillOpacity={0.15}
                            />
                        </AreaChart>
                    </ResponsiveContainer>
                </ChartCard>

                {/* Memory chart */}
                <ChartCard title={t('monitoring.mem_usage')}>
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={chartData}>
                            <CartesianGrid strokeDasharray="3 3" stroke="var(--cp-border)" />
                            <XAxis dataKey="time" tick={{ fontSize: 11, fill: 'var(--cp-text-muted)' }} />
                            <YAxis domain={[0, 100]} tick={{ fontSize: 11, fill: 'var(--cp-text-muted)' }} unit="%" />
                            <Tooltip
                                contentStyle={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', borderRadius: 6 }}
                                labelStyle={{ color: 'var(--cp-text)' }}
                            />
                            <Area
                                type="monotone" dataKey="mem" name="Memory"
                                stroke="#10b981" fill="#10b981" fillOpacity={0.15}
                            />
                        </AreaChart>
                    </ResponsiveContainer>
                </ChartCard>

                {/* Disk I/O chart */}
                <ChartCard title={t('monitoring.disk_io')}>
                    <ResponsiveContainer width="100%" height="100%">
                        <LineChart data={chartData}>
                            <CartesianGrid strokeDasharray="3 3" stroke="var(--cp-border)" />
                            <XAxis dataKey="time" tick={{ fontSize: 11, fill: 'var(--cp-text-muted)' }} />
                            <YAxis tick={{ fontSize: 11, fill: 'var(--cp-text-muted)' }} tickFormatter={formatBytes} />
                            <Tooltip
                                contentStyle={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', borderRadius: 6 }}
                                labelStyle={{ color: 'var(--cp-text)' }}
                                formatter={(v) => formatBytes(v)}
                            />
                            <Line
                                type="monotone" dataKey="disk_read" name={t('monitoring.read')}
                                stroke="#3b82f6" dot={false} strokeWidth={2}
                            />
                            <Line
                                type="monotone" dataKey="disk_write" name={t('monitoring.write')}
                                stroke="#f59e0b" dot={false} strokeWidth={2}
                            />
                        </LineChart>
                    </ResponsiveContainer>
                </ChartCard>

                {/* Network I/O chart */}
                <ChartCard title={t('monitoring.net_io')}>
                    <ResponsiveContainer width="100%" height="100%">
                        <LineChart data={chartData}>
                            <CartesianGrid strokeDasharray="3 3" stroke="var(--cp-border)" />
                            <XAxis dataKey="time" tick={{ fontSize: 11, fill: 'var(--cp-text-muted)' }} />
                            <YAxis tick={{ fontSize: 11, fill: 'var(--cp-text-muted)' }} tickFormatter={formatBytes} />
                            <Tooltip
                                contentStyle={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', borderRadius: 6 }}
                                labelStyle={{ color: 'var(--cp-text)' }}
                                formatter={(v) => formatBytes(v)}
                            />
                            <Line
                                type="monotone" dataKey="net_recv" name={t('monitoring.received')}
                                stroke="#10b981" dot={false} strokeWidth={2}
                            />
                            <Line
                                type="monotone" dataKey="net_sent" name={t('monitoring.sent')}
                                stroke="#8b5cf6" dot={false} strokeWidth={2}
                            />
                        </LineChart>
                    </ResponsiveContainer>
                </ChartCard>
            </Box>

            {/* Container metrics */}
            {containers.length > 0 && (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', marginBottom: 24 }}>
                    <Text size="3" weight="bold" mb="3" style={{ color: 'var(--cp-text)', display: 'block' }}>
                        {t('monitoring.containers')}
                    </Text>
                    <ScrollArea style={{ maxHeight: 300 }}>
                        <Table.Root size="1">
                            <Table.Header>
                                <Table.Row>
                                    <Table.ColumnHeaderCell>{t('common.name')}</Table.ColumnHeaderCell>
                                    <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                                    <Table.ColumnHeaderCell>CPU%</Table.ColumnHeaderCell>
                                    <Table.ColumnHeaderCell>{t('monitoring.memory')}</Table.ColumnHeaderCell>
                                    <Table.ColumnHeaderCell>Mem%</Table.ColumnHeaderCell>
                                </Table.Row>
                            </Table.Header>
                            <Table.Body>
                                {containers.map((c, i) => (
                                    <Table.Row key={c.name || i}>
                                        <Table.Cell>
                                            <Text size="2" weight="medium" style={{ color: 'var(--cp-text)' }}>
                                                {c.name}
                                            </Text>
                                        </Table.Cell>
                                        <Table.Cell>
                                            <Badge color={c.status === 'running' ? 'green' : 'gray'} size="1">
                                                {c.status}
                                            </Badge>
                                        </Table.Cell>
                                        <Table.Cell>
                                            <Text size="2" style={{ color: 'var(--cp-text)' }}>
                                                {formatPercent(c.cpu_percent)}
                                            </Text>
                                        </Table.Cell>
                                        <Table.Cell>
                                            <Text size="2" style={{ color: 'var(--cp-text)' }}>
                                                {formatBytes(c.mem_usage)}
                                            </Text>
                                        </Table.Cell>
                                        <Table.Cell>
                                            <Text size="2" style={{ color: 'var(--cp-text)' }}>
                                                {formatPercent(c.mem_percent)}
                                            </Text>
                                        </Table.Cell>
                                    </Table.Row>
                                ))}
                            </Table.Body>
                        </Table.Root>
                    </ScrollArea>
                </Card>
            )}

            {/* Tabs: Alert Rules + Alert History */}
            <Tabs.Root defaultValue="rules">
                <Tabs.List>
                    <Tabs.Trigger value="rules">{t('monitoring.alerts')}</Tabs.Trigger>
                    <Tabs.Trigger value="history">{t('monitoring.alert_history')}</Tabs.Trigger>
                </Tabs.List>

                {/* Alert Rules tab */}
                <Tabs.Content value="rules">
                    <Box pt="3">
                        <Flex justify="end" mb="3">
                            <Button size="2" onClick={() => {
                                setAlertForm({ ...emptyAlert })
                                setEditingId(null)
                                setDialogOpen(true)
                            }}>
                                <Plus size={14} />
                                {t('monitoring.create_alert')}
                            </Button>
                        </Flex>

                        {alertRules.length === 0 ? (
                            <Flex align="center" justify="center" py="6">
                                <Text size="2" color="gray">{t('monitoring.no_alerts')}</Text>
                            </Flex>
                        ) : (
                            <ScrollArea>
                                <Table.Root size="1">
                                    <Table.Header>
                                        <Table.Row>
                                            <Table.ColumnHeaderCell>{t('monitoring.alert_name')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('monitoring.metric')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('monitoring.operator')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('monitoring.threshold')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('common.enabled')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                                        </Table.Row>
                                    </Table.Header>
                                    <Table.Body>
                                        {alertRules.map((rule) => (
                                            <Table.Row key={rule.id}>
                                                <Table.Cell>
                                                    <Text size="2" weight="medium" style={{ color: 'var(--cp-text)' }}>
                                                        {rule.name}
                                                    </Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Badge size="1" variant="soft">{rule.metric}</Badge>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2" style={{ color: 'var(--cp-text)' }}>{rule.operator}</Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2" style={{ color: 'var(--cp-text)' }}>{rule.threshold}</Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Switch
                                                        size="1"
                                                        checked={rule.enabled !== false}
                                                        onCheckedChange={() => handleToggleAlert(rule)}
                                                    />
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Flex gap="2">
                                                        <Button
                                                            size="1" variant="ghost"
                                                            onClick={() => openEditAlert(rule)}
                                                        >
                                                            {t('common.edit')}
                                                        </Button>
                                                        <Button
                                                            size="1" variant="ghost" color="red"
                                                            onClick={() => handleDeleteAlert(rule.id)}
                                                        >
                                                            <Trash2 size={13} />
                                                        </Button>
                                                    </Flex>
                                                </Table.Cell>
                                            </Table.Row>
                                        ))}
                                    </Table.Body>
                                </Table.Root>
                            </ScrollArea>
                        )}
                    </Box>
                </Tabs.Content>

                {/* Alert History tab */}
                <Tabs.Content value="history">
                    <Box pt="3">
                        {alertHistory.length === 0 ? (
                            <Flex align="center" justify="center" py="6">
                                <Text size="2" color="gray">{t('monitoring.no_alert_history')}</Text>
                            </Flex>
                        ) : (
                            <ScrollArea>
                                <Table.Root size="1">
                                    <Table.Header>
                                        <Table.Row>
                                            <Table.ColumnHeaderCell>{t('audit.time')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('monitoring.alert_name')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('monitoring.metric')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>Value</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('monitoring.threshold')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>Notified</Table.ColumnHeaderCell>
                                        </Table.Row>
                                    </Table.Header>
                                    <Table.Body>
                                        {alertHistory.map((h, i) => (
                                            <Table.Row key={h.id || i}>
                                                <Table.Cell>
                                                    <Text size="2" style={{ color: 'var(--cp-text-muted)' }}>
                                                        {h.triggered_at ? new Date(h.triggered_at).toLocaleString() : '-'}
                                                    </Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2" weight="medium" style={{ color: 'var(--cp-text)' }}>
                                                        {h.rule_name}
                                                    </Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Badge size="1" variant="soft">{h.metric}</Badge>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2" style={{ color: 'var(--cp-text)' }}>
                                                        {h.value != null ? Number(h.value).toFixed(1) : '-'}
                                                    </Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2" style={{ color: 'var(--cp-text)' }}>
                                                        {h.threshold}
                                                    </Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    {h.notified ? (
                                                        <Badge color="green" size="1"><CheckCircle size={12} /> {t('common.yes')}</Badge>
                                                    ) : (
                                                        <Badge color="gray" size="1"><XCircle size={12} /> {t('common.no')}</Badge>
                                                    )}
                                                </Table.Cell>
                                            </Table.Row>
                                        ))}
                                    </Table.Body>
                                </Table.Root>
                            </ScrollArea>
                        )}
                    </Box>
                </Tabs.Content>
            </Tabs.Root>

            {/* Create / Edit Alert Dialog */}
            <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
                <Dialog.Content style={{ maxWidth: 480 }}>
                    <Dialog.Title>
                        {editingId ? t('monitoring.edit_alert') : t('monitoring.create_alert')}
                    </Dialog.Title>

                    <Flex direction="column" gap="3" mt="3">
                        {/* Name */}
                        <Box>
                            <Text size="2" weight="medium" mb="1" style={{ display: 'block', color: 'var(--cp-text)' }}>
                                {t('monitoring.alert_name')}
                            </Text>
                            <TextField.Root
                                value={alertForm.name}
                                onChange={(e) => setAlertForm({ ...alertForm, name: e.target.value })}
                                placeholder="CPU High Alert"
                            />
                        </Box>

                        {/* Metric */}
                        <Box>
                            <Text size="2" weight="medium" mb="1" style={{ display: 'block', color: 'var(--cp-text)' }}>
                                {t('monitoring.metric')}
                            </Text>
                            <Select.Root
                                value={alertForm.metric}
                                onValueChange={(v) => setAlertForm({ ...alertForm, metric: v })}
                            >
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    {metricOptions.map((o) => (
                                        <Select.Item key={o.value} value={o.value}>{o.label}</Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </Box>

                        {/* Operator */}
                        <Box>
                            <Text size="2" weight="medium" mb="1" style={{ display: 'block', color: 'var(--cp-text)' }}>
                                {t('monitoring.operator')}
                            </Text>
                            <Select.Root
                                value={alertForm.operator}
                                onValueChange={(v) => setAlertForm({ ...alertForm, operator: v })}
                            >
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    {operatorOptions.map((o) => (
                                        <Select.Item key={o.value} value={o.value}>{o.label}</Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </Box>

                        {/* Threshold */}
                        <Box>
                            <Text size="2" weight="medium" mb="1" style={{ display: 'block', color: 'var(--cp-text)' }}>
                                {t('monitoring.threshold')}
                            </Text>
                            <TextField.Root
                                type="number"
                                value={alertForm.threshold}
                                onChange={(e) => setAlertForm({ ...alertForm, threshold: Number(e.target.value) })}
                            />
                        </Box>

                        {/* Duration */}
                        <Box>
                            <Text size="2" weight="medium" mb="1" style={{ display: 'block', color: 'var(--cp-text)' }}>
                                {t('monitoring.duration')}
                            </Text>
                            <TextField.Root
                                type="number"
                                value={alertForm.duration}
                                onChange={(e) => setAlertForm({ ...alertForm, duration: Number(e.target.value) })}
                            />
                        </Box>

                        {/* Webhook URL */}
                        <Box>
                            <Text size="2" weight="medium" mb="1" style={{ display: 'block', color: 'var(--cp-text)' }}>
                                {t('monitoring.notify_url')}
                            </Text>
                            <TextField.Root
                                value={alertForm.notify_url}
                                onChange={(e) => setAlertForm({ ...alertForm, notify_url: e.target.value })}
                                placeholder="https://hooks.example.com/webhook"
                            />
                        </Box>
                    </Flex>

                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button onClick={handleSaveAlert}>
                            {editingId ? t('common.save') : t('common.create')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
