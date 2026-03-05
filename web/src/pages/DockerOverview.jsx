import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Text, Card, Badge, Heading, Button, Separator, Dialog, TextArea, TextField, Tabs } from '@radix-ui/themes'
import { Container, Play, Square, RefreshCw, Trash2, FileText, Plus, Download, Server, Search, Radio, Upload, X, Loader2, Star, Settings } from 'lucide-react'
import { useNavigate } from 'react-router'
import { dockerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import DockerRequired from '../components/DockerRequired.jsx'
// Docker — Compose Stacks management (simplified view)

const statusColors = { running: 'green', stopped: 'gray', partial: 'orange', error: 'red', unknown: 'gray' }

export default function DockerOverview() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const [dockerStatus, setDockerStatus] = useState(null)
    const [dockerChecking, setDockerChecking] = useState(true)
    const [stacks, setStacks] = useState([])
    const [containers, setContainers] = useState([])
    const [info, setInfo] = useState(null)
    const [loading, setLoading] = useState(true)
    const [actionLoading, setActionLoading] = useState(null)
    const [showCreate, setShowCreate] = useState(false)
    const [showRunContainer, setShowRunContainer] = useState(false)
    const [showLogs, setShowLogs] = useState(null)
    const [logs, setLogs] = useState('')
    const [logFilter, setLogFilter] = useState('')
    const [logStreaming, setLogStreaming] = useState(false)
    const logRef = useRef(null)
    const wsRef = useRef(null)

    const fetchData = useCallback(async () => {
        try {
            const [stackRes, infoRes, containerRes] = await Promise.allSettled([
                dockerAPI.listStacks(),
                dockerAPI.info(),
                dockerAPI.listContainers(true),
            ])
            if (stackRes.status === 'fulfilled') setStacks(stackRes.value.data?.stacks || [])
            if (infoRes.status === 'fulfilled') setInfo(infoRes.value.data)
            if (containerRes.status === 'fulfilled') {
                const all = containerRes.value.data?.containers || []
                setContainers(all.filter(c => !c.labels?.['com.docker.compose.project']))
            }
        } catch { /* ignore */ } finally { setLoading(false) }
    }, [])

    useEffect(() => { fetchData() }, [fetchData])

    // Check Docker availability first
    const checkDocker = useCallback(async () => {
        setDockerChecking(true)
        try {
            const res = await dockerAPI.status()
            setDockerStatus(res.data)
        } catch {
            setDockerStatus({ installed: false, daemon_running: false })
        } finally { setDockerChecking(false) }
    }, [])

    useEffect(() => { checkDocker() }, [checkDocker])

    const doAction = async (id, action, label) => {
        setActionLoading(`${id}-${action}`)
        try {
            await dockerAPI[action](id)
            await fetchData()
        } catch (e) {
            alert(`${label} failed: ${e.response?.data?.error || e.message}`)
        } finally { setActionLoading(null) }
    }

    const doContainerAction = async (id, action) => {
        setActionLoading(`ctr-${id}-${action}`)
        try {
            await dockerAPI[action](id)
            await fetchData()
        } catch (e) {
            alert(`Failed: ${e.response?.data?.error || e.message}`)
        } finally { setActionLoading(null) }
    }

    const viewContainerLogs = async (id, name) => {
        setShowLogs(`ctr-${name || id}`)
        setLogs('')
        setLogFilter('')
        setLogStreaming(false)
        try {
            const res = await dockerAPI.containerLogs(id, '200')
            setLogs(res.data?.logs || 'No logs available')
        } catch { setLogs('Failed to fetch logs') }
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

    if (dockerChecking || loading) {
        return <Flex align="center" justify="center" style={{ minHeight: 200 }}><RefreshCw size={20} className="spin" /><Text ml="2">{t('common.loading')}</Text></Flex>
    }

    // Show Docker required screen if Docker is not available
    if (dockerStatus && (!dockerStatus.installed || !dockerStatus.daemon_running)) {
        return (
            <Box>
                <Flex align="center" gap="2" mb="4">
                    <Container size={24} />
                    <Heading size="5">Docker</Heading>
                </Flex>
                <DockerRequired
                    installed={dockerStatus.installed}
                    daemonRunning={dockerStatus.daemon_running}
                    error={dockerStatus.error}
                    onRetry={checkDocker}
                />
            </Box>
        )
    }

    return (
        <Box>
            <Flex align="center" justify="between" mb="4">
                <Flex align="center" gap="2">
                    <Container size={24} />
                    <Heading size="5">Docker</Heading>
                </Flex>
                <Flex gap="2">
                    <Button size="2" variant="soft" color="gray" onClick={() => navigate('/docker/settings')}><Settings size={16} /> {t('docker.settings')}</Button>
                    <Button size="2" variant="soft" onClick={() => setShowRunContainer(true)}><Play size={16} /> {t('docker.run_container')}</Button>
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
                                        {s.managed_by === 'appstore' && <Badge color="blue" variant="soft" size="1">{t('docker.managed_by_appstore')}</Badge>}
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
                                    {!s.managed_by && (
                                        <Button size="1" variant="soft" color="red" disabled={!!actionLoading}
                                            onClick={() => { if (confirm(t('docker.confirm_delete'))) doAction(s.id, 'deleteStack', 'Delete') }}>
                                            <Trash2 size={14} />
                                        </Button>
                                    )}
                                </Flex>
                            </Flex>
                        </Card>
                    ))}
                </Flex>
            )}

            {/* Standalone Containers */}
            {containers.length > 0 && (
                <>
                    <Separator size="4" my="4" />
                    <Flex align="center" gap="2" mb="3">
                        <Container size={18} />
                        <Heading size="4">{t('docker.standalone_containers')}</Heading>
                        <Badge variant="soft" size="1">{containers.length}</Badge>
                    </Flex>
                    <Flex direction="column" gap="3">
                        {containers.map((c) => (
                            <Card key={c.id} style={{ padding: 16 }}>
                                <Flex align="center" justify="between" wrap="wrap" gap="2">
                                    <Flex direction="column" gap="1" style={{ flex: 1, minWidth: 200 }}>
                                        <Flex align="center" gap="2">
                                            <Text weight="bold" size="3">{c.name || c.id}</Text>
                                            <Badge color={c.state === 'running' ? 'green' : c.state === 'paused' ? 'orange' : 'gray'} variant="soft" size="1">{c.state}</Badge>
                                        </Flex>
                                        <Flex gap="3">
                                            <Text size="1" color="gray">{c.image}</Text>
                                            {c.ports && c.ports.length > 0 && (
                                                <Text size="1" color="gray">
                                                    {c.ports.filter(p => p.host_port).map(p => `${p.host_port}:${p.container_port}`).join(', ')}
                                                </Text>
                                            )}
                                        </Flex>
                                        <Text size="1" color="gray">{c.status}</Text>
                                    </Flex>
                                    <Flex gap="2" wrap="wrap">
                                        {c.state !== 'running' && (
                                            <Button size="1" variant="soft" color="green" disabled={!!actionLoading}
                                                onClick={() => doContainerAction(c.id, 'startContainer')}>
                                                <Play size={14} /> {t('docker.start')}
                                            </Button>
                                        )}
                                        {c.state === 'running' && (
                                            <Button size="1" variant="soft" color="orange" disabled={!!actionLoading}
                                                onClick={() => doContainerAction(c.id, 'stopContainer')}>
                                                <Square size={14} /> {t('docker.stop')}
                                            </Button>
                                        )}
                                        <Button size="1" variant="soft" disabled={!!actionLoading}
                                            onClick={() => doContainerAction(c.id, 'restartContainer')}>
                                            <RefreshCw size={14} /> {t('docker.restart')}
                                        </Button>
                                        <Button size="1" variant="soft" onClick={() => viewContainerLogs(c.id, c.name)}>
                                            <FileText size={14} /> {t('docker.logs')}
                                        </Button>
                                        <Button size="1" variant="soft" color="red" disabled={!!actionLoading}
                                            onClick={() => { if (confirm(t('docker.confirm_remove_container'))) doContainerAction(c.id, 'removeContainer') }}>
                                            <Trash2 size={14} />
                                        </Button>
                                    </Flex>
                                </Flex>
                            </Card>
                        ))}
                    </Flex>
                </>
            )}

            {/* Create Stack Dialog */}
            <CreateStackDialog open={showCreate} onClose={() => setShowCreate(false)} onCreated={fetchData} />

            {/* Run Container Dialog */}
            <RunContainerDialog open={showRunContainer} onClose={() => setShowRunContainer(false)} onCreated={fetchData} />

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

function RunContainerDialog({ open, onClose, onCreated }) {
    const { t } = useTranslation()
    const [image, setImage] = useState('')
    const [name, setName] = useState('')
    const [ports, setPorts] = useState([])
    const [volumes, setVolumes] = useState([])
    const [envVars, setEnvVars] = useState([])
    const [network, setNetwork] = useState('')
    const [restartPolicy, setRestartPolicy] = useState('no')
    const [command, setCommand] = useState('')
    const [memoryLimit, setMemoryLimit] = useState('')
    const [cpuLimit, setCpuLimit] = useState('')
    const [networks, setNetworks] = useState([])
    const [creating, setCreating] = useState(false)
    const [error, setError] = useState('')

    // Image search state
    const [searchTerm, setSearchTerm] = useState('')
    const [searchResults, setSearchResults] = useState([])
    const [searching, setSearching] = useState(false)
    const [showSearch, setShowSearch] = useState(false)

    useEffect(() => {
        if (open) {
            dockerAPI.listNetworks().then(res => {
                setNetworks(res.data?.networks || [])
            }).catch(() => {})
        }
    }, [open])

    const resetForm = () => {
        setImage(''); setName(''); setPorts([]); setVolumes([]); setEnvVars([])
        setNetwork(''); setRestartPolicy('no'); setCommand('')
        setMemoryLimit(''); setCpuLimit(''); setError('')
        setSearchTerm(''); setSearchResults([]); setShowSearch(false)
    }

    const handleClose = () => {
        resetForm()
        onClose()
    }

    const handleSearch = async () => {
        if (!searchTerm.trim()) return
        setSearching(true)
        try {
            const res = await dockerAPI.searchImages(searchTerm.trim(), 10)
            setSearchResults(res.data?.results || [])
            setShowSearch(true)
        } catch { setSearchResults([]) }
        finally { setSearching(false) }
    }

    const selectImage = (name) => {
        setImage(name)
        setShowSearch(false)
        setSearchResults([])
    }

    const handleRun = async () => {
        if (!image.trim()) return
        setCreating(true)
        setError('')
        try {
            const data = {
                image: image.trim(),
                name: name.trim(),
                ports: ports.filter(p => p.container_port),
                volumes: volumes.filter(v => v.host_path && v.container_path),
                env: envVars.filter(e => e.key).map(e => `${e.key}=${e.value}`),
                restart_policy: restartPolicy,
                network: network,
                command: command.trim(),
                memory_limit: memoryLimit ? parseInt(memoryLimit) * 1024 * 1024 : 0,
                cpu_limit: cpuLimit ? parseFloat(cpuLimit) : 0,
            }
            await dockerAPI.runContainer(data)
            onCreated()
            handleClose()
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        } finally { setCreating(false) }
    }

    const addPort = () => setPorts([...ports, { host_port: '', container_port: '', protocol: 'tcp' }])
    const removePort = (i) => setPorts(ports.filter((_, idx) => idx !== i))
    const updatePort = (i, field, val) => {
        const next = [...ports]
        next[i] = { ...next[i], [field]: val }
        setPorts(next)
    }

    const addVolume = () => setVolumes([...volumes, { host_path: '', container_path: '', read_only: false }])
    const removeVolume = (i) => setVolumes(volumes.filter((_, idx) => idx !== i))
    const updateVolume = (i, field, val) => {
        const next = [...volumes]
        next[i] = { ...next[i], [field]: val }
        setVolumes(next)
    }

    const addEnv = () => setEnvVars([...envVars, { key: '', value: '' }])
    const removeEnv = (i) => setEnvVars(envVars.filter((_, idx) => idx !== i))
    const updateEnv = (i, field, val) => {
        const next = [...envVars]
        next[i] = { ...next[i], [field]: val }
        setEnvVars(next)
    }

    return (
        <Dialog.Root open={open} onOpenChange={(v) => { if (!v) handleClose() }}>
            <Dialog.Content maxWidth="700px" style={{ maxHeight: '85vh', overflow: 'auto' }}>
                <Dialog.Title>{t('docker.run_container')}</Dialog.Title>
                <Flex direction="column" gap="3" mt="3">
                    {/* Image */}
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.image')} *</Text>
                        <Flex gap="2">
                            <Box style={{ flex: 1 }}>
                                <TextField.Root placeholder="nginx:latest" value={image} onChange={(e) => setImage(e.target.value)} />
                            </Box>
                            <TextField.Root
                                placeholder={t('docker.search_placeholder')}
                                value={searchTerm}
                                onChange={(e) => setSearchTerm(e.target.value)}
                                onKeyDown={(e) => { if (e.key === 'Enter') handleSearch() }}
                                style={{ width: 180 }}
                            >
                                <TextField.Slot side="right">
                                    <Button variant="ghost" size="1" onClick={handleSearch} disabled={searching}>
                                        {searching ? <Loader2 size={14} className="spin" /> : <Search size={14} />}
                                    </Button>
                                </TextField.Slot>
                            </TextField.Root>
                        </Flex>
                        {showSearch && searchResults.length > 0 && (
                            <Box mt="2" style={{ border: '1px solid var(--gray-5)', borderRadius: 8, maxHeight: 200, overflow: 'auto' }}>
                                {searchResults.map((r, i) => (
                                    <Flex key={i} align="center" gap="2" px="3" py="2"
                                        style={{ cursor: 'pointer', borderBottom: i < searchResults.length - 1 ? '1px solid var(--gray-4)' : 'none' }}
                                        onClick={() => selectImage(r.name)}
                                    >
                                        <Text size="2" weight="bold" style={{ flex: 1 }}>{r.name}</Text>
                                        {r.is_official && <Badge color="blue" size="1">Official</Badge>}
                                        <Flex align="center" gap="1"><Star size={12} /><Text size="1">{r.star_count}</Text></Flex>
                                    </Flex>
                                ))}
                            </Box>
                        )}
                    </Box>

                    {/* Container Name */}
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.container_name')}</Text>
                        <TextField.Root placeholder={t('docker.container_name_placeholder')} value={name} onChange={(e) => setName(e.target.value)} />
                    </Box>

                    {/* Port Mappings */}
                    <Box>
                        <Flex align="center" justify="between" mb="1">
                            <Text size="2" weight="bold">{t('docker.port_mappings')}</Text>
                            <Button variant="ghost" size="1" onClick={addPort}><Plus size={14} /> {t('docker.add_port')}</Button>
                        </Flex>
                        {ports.map((p, i) => (
                            <Flex key={i} gap="2" align="center" mb="1">
                                <TextField.Root placeholder={t('docker.host_port')} value={p.host_port}
                                    onChange={(e) => updatePort(i, 'host_port', e.target.value)} style={{ width: 100 }} />
                                <Text size="2">:</Text>
                                <TextField.Root placeholder={t('docker.container_port')} value={p.container_port}
                                    onChange={(e) => updatePort(i, 'container_port', e.target.value)} style={{ width: 100 }} />
                                <select value={p.protocol} onChange={(e) => updatePort(i, 'protocol', e.target.value)}
                                    style={{ padding: '6px 8px', borderRadius: 6, border: '1px solid var(--gray-6)', fontSize: 13, background: 'var(--color-background)' }}>
                                    <option value="tcp">TCP</option>
                                    <option value="udp">UDP</option>
                                </select>
                                <Button variant="ghost" size="1" color="red" onClick={() => removePort(i)}><X size={14} /></Button>
                            </Flex>
                        ))}
                    </Box>

                    {/* Volume Mounts */}
                    <Box>
                        <Flex align="center" justify="between" mb="1">
                            <Text size="2" weight="bold">{t('docker.volume_mounts')}</Text>
                            <Button variant="ghost" size="1" onClick={addVolume}><Plus size={14} /> {t('docker.add_volume')}</Button>
                        </Flex>
                        {volumes.map((v, i) => (
                            <Flex key={i} gap="2" align="center" mb="1">
                                <TextField.Root placeholder={t('docker.host_path')} value={v.host_path}
                                    onChange={(e) => updateVolume(i, 'host_path', e.target.value)} style={{ flex: 1 }} />
                                <Text size="2">:</Text>
                                <TextField.Root placeholder={t('docker.container_path')} value={v.container_path}
                                    onChange={(e) => updateVolume(i, 'container_path', e.target.value)} style={{ flex: 1 }} />
                                <Flex align="center" gap="1">
                                    <input type="checkbox" checked={v.read_only}
                                        onChange={(e) => updateVolume(i, 'read_only', e.target.checked)} />
                                    <Text size="1">RO</Text>
                                </Flex>
                                <Button variant="ghost" size="1" color="red" onClick={() => removeVolume(i)}><X size={14} /></Button>
                            </Flex>
                        ))}
                    </Box>

                    {/* Environment Variables */}
                    <Box>
                        <Flex align="center" justify="between" mb="1">
                            <Text size="2" weight="bold">{t('docker.env_vars')}</Text>
                            <Button variant="ghost" size="1" onClick={addEnv}><Plus size={14} /> {t('docker.add_env')}</Button>
                        </Flex>
                        {envVars.map((e, i) => (
                            <Flex key={i} gap="2" align="center" mb="1">
                                <TextField.Root placeholder="KEY" value={e.key}
                                    onChange={(ev) => updateEnv(i, 'key', ev.target.value)} style={{ flex: 1 }} />
                                <Text size="2">=</Text>
                                <TextField.Root placeholder="VALUE" value={e.value}
                                    onChange={(ev) => updateEnv(i, 'value', ev.target.value)} style={{ flex: 1 }} />
                                <Button variant="ghost" size="1" color="red" onClick={() => removeEnv(i)}><X size={14} /></Button>
                            </Flex>
                        ))}
                    </Box>

                    {/* Network + Restart Policy */}
                    <Flex gap="3">
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.network')}</Text>
                            <select value={network} onChange={(e) => setNetwork(e.target.value)}
                                style={{ width: '100%', padding: '8px 10px', borderRadius: 6, border: '1px solid var(--gray-6)', fontSize: 13, background: 'var(--color-background)' }}>
                                <option value="">{t('docker.network_default')}</option>
                                {networks.map(n => (
                                    <option key={n.id} value={n.name}>{n.name} ({n.driver})</option>
                                ))}
                            </select>
                        </Box>
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.restart_policy')}</Text>
                            <select value={restartPolicy} onChange={(e) => setRestartPolicy(e.target.value)}
                                style={{ width: '100%', padding: '8px 10px', borderRadius: 6, border: '1px solid var(--gray-6)', fontSize: 13, background: 'var(--color-background)' }}>
                                <option value="no">{t('docker.restart_no')}</option>
                                <option value="always">{t('docker.restart_always')}</option>
                                <option value="unless-stopped">{t('docker.restart_unless_stopped')}</option>
                                <option value="on-failure">{t('docker.restart_on_failure')}</option>
                            </select>
                        </Box>
                    </Flex>

                    {/* Command */}
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.command')}</Text>
                        <TextField.Root placeholder={t('docker.command_placeholder')} value={command} onChange={(e) => setCommand(e.target.value)} />
                    </Box>

                    {/* Resource Limits */}
                    <Flex gap="3">
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.memory_limit')}</Text>
                            <TextField.Root type="number" placeholder="0" value={memoryLimit} onChange={(e) => setMemoryLimit(e.target.value)} />
                        </Box>
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.cpu_limit')}</Text>
                            <TextField.Root type="number" placeholder="0" step="0.1" value={cpuLimit} onChange={(e) => setCpuLimit(e.target.value)} />
                        </Box>
                    </Flex>

                    {error && <Text size="2" color="red">{error}</Text>}
                </Flex>

                <Flex justify="end" gap="2" mt="4">
                    <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                    <Button disabled={creating || !image.trim()} onClick={handleRun}>
                        {creating ? (
                            <><Loader2 size={16} className="spin" /> {t('docker.running_container')}</>
                        ) : (
                            <><Play size={16} /> {t('docker.run')}</>
                        )}
                    </Button>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}
