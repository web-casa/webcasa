import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Text, Heading, Badge, Button, Table, Dialog, TextField, Tabs } from '@radix-ui/themes'
import { Box as BoxIcon, Play, Square, RefreshCw, Trash2, FileText, BarChart3, ArrowLeft, Search, Download, Radio } from 'lucide-react'
import { dockerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

const stateColors = { running: 'green', exited: 'gray', paused: 'yellow', restarting: 'blue', dead: 'red', created: 'gray' }

export default function DockerContainers() {
    const { t } = useTranslation()
    const [containers, setContainers] = useState([])
    const [loading, setLoading] = useState(true)
    const [actionLoading, setActionLoading] = useState(null)
    const [showLogs, setShowLogs] = useState(null)
    const [logs, setLogs] = useState('')
    const [logFilter, setLogFilter] = useState('')
    const [logStreaming, setLogStreaming] = useState(false)
    const [showStats, setShowStats] = useState(null)
    const [stats, setStats] = useState(null)
    const logRef = useRef(null)
    const wsRef = useRef(null)

    const fetchData = useCallback(async () => {
        try {
            const res = await dockerAPI.listContainers(true)
            setContainers(res.data?.containers || [])
        } catch { /* ignore */ } finally { setLoading(false) }
    }, [])

    useEffect(() => { fetchData() }, [fetchData])

    const doAction = async (id, action) => {
        setActionLoading(`${id}-${action}`)
        try {
            await dockerAPI[action](id)
            await fetchData()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setActionLoading(null) }
    }

    const viewLogs = async (id, streaming = false) => {
        setShowLogs(id)
        setLogs('')
        setLogStreaming(streaming)

        if (streaming) {
            startLogStream(id)
        } else {
            try {
                const res = await dockerAPI.containerLogs(id, '500')
                setLogs(res.data?.logs || 'No logs')
            } catch { setLogs('Failed to fetch logs') }
        }
    }

    const startLogStream = (id) => {
        if (wsRef.current) wsRef.current.close()
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        const token = localStorage.getItem('token')
        const ws = new WebSocket(`${proto}//${window.location.host}/api/plugins/docker/containers/${id}/logs/ws?tail=100&token=${token}`)
        wsRef.current = ws
        let buffer = ''
        ws.onmessage = (e) => {
            buffer += e.data + '\n'
            setLogs(buffer)
        }
        ws.onerror = () => setLogs(prev => prev + '\n[WebSocket error]\n')
        ws.onclose = () => setLogStreaming(false)
    }

    const closeLogs = () => {
        if (wsRef.current) { wsRef.current.close(); wsRef.current = null }
        setShowLogs(null)
        setLogs('')
        setLogFilter('')
        setLogStreaming(false)
    }

    const downloadLogs = () => {
        const blob = new Blob([logs], { type: 'text/plain' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `container-${showLogs}-logs.txt`
        a.click()
        URL.revokeObjectURL(url)
    }

    // Auto-scroll log
    useEffect(() => {
        if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
    }, [logs])

    const viewStats = async (id) => {
        setShowStats(id)
        try {
            const res = await dockerAPI.containerStats(id)
            setStats(res.data)
        } catch { setStats(null) }
    }

    const formatBytes = (bytes) => {
        if (!bytes) return '0 B'
        const units = ['B', 'KB', 'MB', 'GB']
        let i = 0; let val = bytes
        while (val >= 1024 && i < units.length - 1) { val /= 1024; i++ }
        return `${val.toFixed(1)} ${units[i]}`
    }

    const filteredLogs = logFilter
        ? logs.split('\n').filter(line => line.toLowerCase().includes(logFilter.toLowerCase())).join('\n')
        : logs

    if (loading) {
        return <Flex align="center" justify="center" style={{ minHeight: 200 }}><RefreshCw size={20} className="spin" /><Text ml="2">{t('common.loading')}</Text></Flex>
    }

    return (
        <Box>
            <Flex align="center" justify="between" mb="4">
                <Flex align="center" gap="2">
                    <Link to="/docker"><Button variant="ghost" size="1"><ArrowLeft size={16} /></Button></Link>
                    <BoxIcon size={24} />
                    <Heading size="5">{t('docker.containers')}</Heading>
                    <Badge variant="soft" size="2">{containers.length}</Badge>
                </Flex>
                <Flex gap="2">
                    <Link to="/docker/images"><Button variant="soft" size="2">{t('docker.images')}</Button></Link>
                    <Link to="/docker/networks"><Button variant="soft" size="2">{t('docker.networks')}</Button></Link>
                    <Link to="/docker/volumes"><Button variant="soft" size="2">{t('docker.volumes')}</Button></Link>
                    <Button variant="soft" size="2" onClick={fetchData}><RefreshCw size={14} /></Button>
                </Flex>
            </Flex>

            {containers.length === 0 ? (
                <Text color="gray">{t('docker.no_containers')}</Text>
            ) : (
                <Table.Root variant="surface">
                    <Table.Header>
                        <Table.Row>
                            <Table.ColumnHeaderCell>{t('docker.name')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('docker.image')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('docker.ports')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                        </Table.Row>
                    </Table.Header>
                    <Table.Body>
                        {containers.map((c) => (
                            <Table.Row key={c.id}>
                                <Table.Cell>
                                    <Text weight="bold" size="2">{c.name}</Text>
                                    <Text size="1" color="gray" style={{ display: 'block' }}>{c.id}</Text>
                                </Table.Cell>
                                <Table.Cell><Text size="2">{c.image}</Text></Table.Cell>
                                <Table.Cell>
                                    <Badge color={stateColors[c.state] || 'gray'} variant="soft" size="1">{c.state}</Badge>
                                    <Text size="1" color="gray" style={{ display: 'block' }}>{c.status}</Text>
                                </Table.Cell>
                                <Table.Cell>
                                    {c.ports?.filter(p => p.host_port).map((p, i) => (
                                        <Badge key={i} variant="outline" size="1" mr="1">
                                            {p.host_port}:{p.container_port}/{p.protocol}
                                        </Badge>
                                    ))}
                                </Table.Cell>
                                <Table.Cell>
                                    <Flex gap="1" wrap="wrap">
                                        {c.state !== 'running' && (
                                            <Button size="1" variant="ghost" color="green" disabled={!!actionLoading}
                                                onClick={() => doAction(c.id, 'startContainer')}><Play size={12} /></Button>
                                        )}
                                        {c.state === 'running' && (
                                            <Button size="1" variant="ghost" color="orange" disabled={!!actionLoading}
                                                onClick={() => doAction(c.id, 'stopContainer')}><Square size={12} /></Button>
                                        )}
                                        <Button size="1" variant="ghost" disabled={!!actionLoading}
                                            onClick={() => doAction(c.id, 'restartContainer')}><RefreshCw size={12} /></Button>
                                        <Button size="1" variant="ghost" onClick={() => viewLogs(c.id)}>
                                            <FileText size={12} />
                                        </Button>
                                        {c.state === 'running' && (
                                            <>
                                                <Button size="1" variant="ghost" color="green" onClick={() => viewLogs(c.id, true)}>
                                                    <Radio size={12} />
                                                </Button>
                                                <Button size="1" variant="ghost" onClick={() => viewStats(c.id)}><BarChart3 size={12} /></Button>
                                            </>
                                        )}
                                        <Button size="1" variant="ghost" color="red" disabled={!!actionLoading}
                                            onClick={() => { if (confirm(t('docker.confirm_delete'))) doAction(c.id, 'removeContainer') }}>
                                            <Trash2 size={12} />
                                        </Button>
                                    </Flex>
                                </Table.Cell>
                            </Table.Row>
                        ))}
                    </Table.Body>
                </Table.Root>
            )}

            {/* Logs Dialog */}
            <Dialog.Root open={!!showLogs} onOpenChange={(v) => { if (!v) closeLogs() }}>
                <Dialog.Content maxWidth="900px">
                    <Dialog.Title>
                        <Flex justify="between" align="center">
                            <Flex align="center" gap="2">
                                {t('docker.container_logs')}
                                {logStreaming && <Badge color="green" variant="soft"><Radio size={10} /> Live</Badge>}
                            </Flex>
                            <Flex gap="2">
                                <Button variant="ghost" size="1" onClick={downloadLogs}><Download size={14} /></Button>
                                {!logStreaming && showLogs && (
                                    <Button variant="ghost" size="1" color="green" onClick={() => viewLogs(showLogs, true)}>
                                        <Radio size={14} /> {t('docker.live')}
                                    </Button>
                                )}
                            </Flex>
                        </Flex>
                    </Dialog.Title>
                    <TextField.Root placeholder={t('docker.filter_logs')} value={logFilter} onChange={e => setLogFilter(e.target.value)} mb="2">
                        <TextField.Slot><Search size={14} /></TextField.Slot>
                    </TextField.Root>
                    <Box ref={logRef} style={{ background: 'var(--gray-2)', borderRadius: 8, padding: 12, maxHeight: 500, overflow: 'auto', fontFamily: 'monospace', fontSize: '0.75rem', whiteSpace: 'pre-wrap', lineHeight: 1.5 }}>
                        {filteredLogs || t('docker.no_logs')}
                    </Box>
                    <Flex justify="end" mt="3"><Dialog.Close><Button variant="soft">{t('common.close')}</Button></Dialog.Close></Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Stats Dialog */}
            <Dialog.Root open={!!showStats} onOpenChange={() => { setShowStats(null); setStats(null) }}>
                <Dialog.Content maxWidth="400px">
                    <Dialog.Title>{t('docker.container_stats')}</Dialog.Title>
                    {stats ? (
                        <Flex direction="column" gap="2" mt="2">
                            <Flex justify="between"><Text size="2">CPU</Text><Text size="2" weight="bold">{stats.cpu_percent.toFixed(1)}%</Text></Flex>
                            <Flex justify="between"><Text size="2">{t('docker.memory')}</Text><Text size="2" weight="bold">{formatBytes(stats.mem_usage)} / {formatBytes(stats.mem_limit)} ({stats.mem_percent.toFixed(1)}%)</Text></Flex>
                            <Flex justify="between"><Text size="2">{t('docker.net_rx')}</Text><Text size="2" weight="bold">{formatBytes(stats.net_rx)}</Text></Flex>
                            <Flex justify="between"><Text size="2">{t('docker.net_tx')}</Text><Text size="2" weight="bold">{formatBytes(stats.net_tx)}</Text></Flex>
                        </Flex>
                    ) : <Text color="gray">{t('common.loading')}</Text>}
                    <Flex justify="end" mt="3"><Dialog.Close><Button variant="soft">{t('common.close')}</Button></Dialog.Close></Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
