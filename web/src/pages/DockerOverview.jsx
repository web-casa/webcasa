import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Text, Card, Badge, Heading, Button, Separator, Dialog, TextArea, TextField, Tabs } from '@radix-ui/themes'
import { Container, Play, Square, RefreshCw, Trash2, FileText, Plus, Download, Server, Search, Radio, Upload } from 'lucide-react'
import { dockerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

const statusColors = { running: 'green', stopped: 'gray', partial: 'orange', error: 'red', unknown: 'gray' }

export default function DockerOverview() {
    const { t } = useTranslation()
    const [stacks, setStacks] = useState([])
    const [info, setInfo] = useState(null)
    const [loading, setLoading] = useState(true)
    const [actionLoading, setActionLoading] = useState(null)
    const [showCreate, setShowCreate] = useState(false)
    const [showLogs, setShowLogs] = useState(null)
    const [logs, setLogs] = useState('')
    const [logFilter, setLogFilter] = useState('')
    const [logStreaming, setLogStreaming] = useState(false)
    const logRef = useRef(null)
    const wsRef = useRef(null)

    const fetchData = useCallback(async () => {
        try {
            const [stackRes, infoRes] = await Promise.allSettled([
                dockerAPI.listStacks(),
                dockerAPI.info(),
            ])
            if (stackRes.status === 'fulfilled') setStacks(stackRes.value.data?.stacks || [])
            if (infoRes.status === 'fulfilled') setInfo(infoRes.value.data)
        } catch { /* ignore */ } finally { setLoading(false) }
    }, [])

    useEffect(() => { fetchData() }, [fetchData])

    const doAction = async (id, action, label) => {
        setActionLoading(`${id}-${action}`)
        try {
            await dockerAPI[action](id)
            await fetchData()
        } catch (e) {
            alert(`${label} failed: ${e.response?.data?.error || e.message}`)
        } finally { setActionLoading(null) }
    }

    const viewLogs = async (id, streaming = false) => {
        setShowLogs(id)
        setLogs('')
        setLogFilter('')
        setLogStreaming(streaming)

        if (streaming) {
            startLogStream(id)
        } else {
            try {
                const res = await dockerAPI.stackLogs(id, '200')
                setLogs(res.data?.logs || 'No logs available')
            } catch { setLogs('Failed to fetch logs') }
        }
    }

    const startLogStream = (id) => {
        if (wsRef.current) wsRef.current.close()
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        const token = localStorage.getItem('token')
        const ws = new WebSocket(`${proto}//${window.location.host}/api/plugins/docker/stacks/${id}/logs/ws?tail=100&token=${token}`)
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
        a.download = `stack-${showLogs}-logs.txt`
        a.click()
        URL.revokeObjectURL(url)
    }

    useEffect(() => {
        if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
    }, [logs])

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
                    <Container size={24} />
                    <Heading size="5">Docker</Heading>
                </Flex>
                <Flex gap="2">
                    <Link to="/docker/containers"><Button variant="soft" size="2">{t('docker.advanced_mode')}</Button></Link>
                    <Button size="2" onClick={() => setShowCreate(true)}><Plus size={16} /> {t('docker.create_stack')}</Button>
                </Flex>
            </Flex>

            {/* Docker Info Summary */}
            {info && (
                <Flex gap="3" mb="4" wrap="wrap">
                    <Card style={{ padding: '12px 16px', flex: 1, minWidth: 140 }}>
                        <Text size="1" color="gray">{t('docker.server_version')}</Text>
                        <Text size="3" weight="bold" style={{ display: 'block' }}>{info.server_version}</Text>
                    </Card>
                    <Card style={{ padding: '12px 16px', flex: 1, minWidth: 140 }}>
                        <Text size="1" color="gray">{t('docker.containers')}</Text>
                        <Text size="3" weight="bold" style={{ display: 'block' }}>
                            <Text color="green">{info.running}</Text> / {info.containers}
                        </Text>
                    </Card>
                    <Card style={{ padding: '12px 16px', flex: 1, minWidth: 140 }}>
                        <Text size="1" color="gray">{t('docker.images')}</Text>
                        <Text size="3" weight="bold" style={{ display: 'block' }}>{info.images}</Text>
                    </Card>
                </Flex>
            )}

            <Separator size="4" mb="4" />

            {/* Stacks */}
            {stacks.length === 0 ? (
                <Card style={{ padding: 40, textAlign: 'center' }}>
                    <Server size={48} style={{ margin: '0 auto 12px', opacity: 0.3 }} />
                    <Text size="3" color="gray" style={{ display: 'block', marginBottom: 12 }}>{t('docker.no_stacks')}</Text>
                    <Button onClick={() => setShowCreate(true)}><Plus size={16} /> {t('docker.create_stack')}</Button>
                </Card>
            ) : (
                <Flex direction="column" gap="3">
                    {stacks.map((s) => (
                        <Card key={s.id} style={{ padding: 16 }}>
                            <Flex align="center" justify="between" wrap="wrap" gap="2">
                                <Flex direction="column" gap="1" style={{ flex: 1, minWidth: 200 }}>
                                    <Flex align="center" gap="2">
                                        <Text weight="bold" size="3">{s.name}</Text>
                                        <Badge color={statusColors[s.status] || 'gray'} variant="soft" size="1">{s.status}</Badge>
                                    </Flex>
                                    {s.description && <Text size="2" color="gray">{s.description}</Text>}
                                </Flex>
                                <Flex gap="2" wrap="wrap">
                                    {s.status !== 'running' && (
                                        <Button size="1" variant="soft" color="green" disabled={!!actionLoading}
                                            onClick={() => doAction(s.id, 'stackUp', 'Start')}>
                                            <Play size={14} /> {t('docker.start')}
                                        </Button>
                                    )}
                                    {s.status === 'running' && (
                                        <Button size="1" variant="soft" color="orange" disabled={!!actionLoading}
                                            onClick={() => doAction(s.id, 'stackDown', 'Stop')}>
                                            <Square size={14} /> {t('docker.stop')}
                                        </Button>
                                    )}
                                    <Button size="1" variant="soft" disabled={!!actionLoading}
                                        onClick={() => doAction(s.id, 'stackRestart', 'Restart')}>
                                        <RefreshCw size={14} /> {t('docker.restart')}
                                    </Button>
                                    <Button size="1" variant="soft" disabled={!!actionLoading}
                                        onClick={() => doAction(s.id, 'stackPull', 'Pull')}>
                                        <Download size={14} /> {t('docker.pull')}
                                    </Button>
                                    <Button size="1" variant="soft" onClick={() => viewLogs(s.id)}>
                                        <FileText size={14} /> {t('docker.logs')}
                                    </Button>
                                    {s.status === 'running' && (
                                        <Button size="1" variant="soft" color="green" onClick={() => viewLogs(s.id, true)}>
                                            <Radio size={14} /> {t('docker.live')}
                                        </Button>
                                    )}
                                    <Button size="1" variant="soft" color="red" disabled={!!actionLoading}
                                        onClick={() => { if (confirm(t('docker.confirm_delete'))) doAction(s.id, 'deleteStack', 'Delete') }}>
                                        <Trash2 size={14} />
                                    </Button>
                                </Flex>
                            </Flex>
                        </Card>
                    ))}
                </Flex>
            )}

            {/* Create Stack Dialog */}
            <CreateStackDialog open={showCreate} onClose={() => setShowCreate(false)} onCreated={fetchData} />

            {/* Logs Dialog */}
            <Dialog.Root open={!!showLogs} onOpenChange={(v) => { if (!v) closeLogs() }}>
                <Dialog.Content maxWidth="900px">
                    <Dialog.Title>
                        <Flex justify="between" align="center">
                            <Flex align="center" gap="2">
                                {t('docker.stack_logs')}
                                {logStreaming && <Badge color="green" variant="soft"><Radio size={10} /> Live</Badge>}
                            </Flex>
                            <Flex gap="2">
                                <Button variant="ghost" size="1" onClick={downloadLogs}><Download size={14} /></Button>
                                {!logStreaming && showLogs && (
                                    <Button variant="ghost" size="1" color="green" onClick={() => viewLogs(showLogs, true)}>
                                        <Radio size={14} />
                                    </Button>
                                )}
                            </Flex>
                        </Flex>
                    </Dialog.Title>
                    <TextField.Root placeholder={t('docker.filter_logs')} value={logFilter} onChange={e => setLogFilter(e.target.value)} mb="2">
                        <TextField.Slot><Search size={14} /></TextField.Slot>
                    </TextField.Root>
                    <Box ref={logRef} style={{ background: 'var(--gray-2)', borderRadius: 8, padding: 12, maxHeight: 500, overflow: 'auto', fontFamily: 'monospace', fontSize: '0.8rem', whiteSpace: 'pre-wrap', lineHeight: 1.5 }}>
                        {filteredLogs || t('docker.no_logs')}
                    </Box>
                    <Flex justify="end" mt="3">
                        <Dialog.Close><Button variant="soft">{t('common.close')}</Button></Dialog.Close>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}

function CreateStackDialog({ open, onClose, onCreated }) {
    const { t } = useTranslation()
    const [name, setName] = useState('')
    const [description, setDescription] = useState('')
    const [composeFile, setComposeFile] = useState('')
    const [envFile, setEnvFile] = useState('')
    const [autoStart, setAutoStart] = useState(true)
    const [creating, setCreating] = useState(false)
    const composeInputRef = useRef(null)
    const envInputRef = useRef(null)

    const handleCreate = async () => {
        if (!name.trim() || !composeFile.trim()) return
        setCreating(true)
        try {
            await dockerAPI.createStack({
                name: name.trim(),
                description,
                compose_file: composeFile,
                env_file: envFile,
                auto_start: autoStart,
            })
            onCreated()
            onClose()
            setName(''); setDescription(''); setComposeFile(''); setEnvFile('')
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setCreating(false) }
    }

    const handleFileUpload = (setter) => (e) => {
        const file = e.target.files?.[0]
        if (!file) return
        const reader = new FileReader()
        reader.onload = (ev) => setter(ev.target.result)
        reader.readAsText(file)
        e.target.value = '' // reset so same file can be re-uploaded
    }

    return (
        <Dialog.Root open={open} onOpenChange={(v) => { if (!v) onClose() }}>
            <Dialog.Content maxWidth="700px">
                <Dialog.Title>{t('docker.create_stack')}</Dialog.Title>
                <Flex direction="column" gap="3" mt="3">
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.stack_name')}</Text>
                        <TextField.Root placeholder="my-app" value={name} onChange={(e) => setName(e.target.value)} />
                    </Box>
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.description')}</Text>
                        <TextField.Root placeholder={t('docker.description_placeholder')} value={description} onChange={(e) => setDescription(e.target.value)} />
                    </Box>

                    <Tabs.Root defaultValue="compose">
                        <Tabs.List>
                            <Tabs.Trigger value="compose">docker-compose.yml</Tabs.Trigger>
                            <Tabs.Trigger value="env">.env</Tabs.Trigger>
                        </Tabs.List>
                        <Box pt="3">
                            <Tabs.Content value="compose">
                                <Flex justify="end" mb="1">
                                    <input ref={composeInputRef} type="file" accept=".yml,.yaml" hidden onChange={handleFileUpload(setComposeFile)} />
                                    <Button variant="ghost" size="1" onClick={() => composeInputRef.current?.click()}>
                                        <Upload size={14} /> {t('docker.upload_file')}
                                    </Button>
                                </Flex>
                                <TextArea
                                    placeholder={`version: '3.8'\nservices:\n  app:\n    image: nginx:latest\n    ports:\n      - "8080:80"`}
                                    value={composeFile}
                                    onChange={(e) => setComposeFile(e.target.value)}
                                    style={{ minHeight: 250, fontFamily: 'monospace', fontSize: '0.85rem' }}
                                />
                            </Tabs.Content>
                            <Tabs.Content value="env">
                                <Flex justify="end" mb="1">
                                    <input ref={envInputRef} type="file" accept=".env" hidden onChange={handleFileUpload(setEnvFile)} />
                                    <Button variant="ghost" size="1" onClick={() => envInputRef.current?.click()}>
                                        <Upload size={14} /> {t('docker.upload_file')}
                                    </Button>
                                </Flex>
                                <TextArea
                                    placeholder="DB_HOST=localhost\nDB_PORT=5432"
                                    value={envFile}
                                    onChange={(e) => setEnvFile(e.target.value)}
                                    style={{ minHeight: 200, fontFamily: 'monospace', fontSize: '0.85rem' }}
                                />
                            </Tabs.Content>
                        </Box>
                    </Tabs.Root>

                    <Flex align="center" gap="2">
                        <input type="checkbox" id="auto-start" checked={autoStart} onChange={(e) => setAutoStart(e.target.checked)} />
                        <label htmlFor="auto-start"><Text size="2">{t('docker.auto_start')}</Text></label>
                    </Flex>
                </Flex>
                <Flex justify="end" gap="2" mt="4">
                    <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                    <Button disabled={creating || !name.trim() || !composeFile.trim()} onClick={handleCreate}>
                        {creating ? t('common.saving') : t('common.create')}
                    </Button>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}
