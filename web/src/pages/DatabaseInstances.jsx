import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Grid, Card, Button, IconButton, Text, Heading, Badge, Dialog, TextField, Select, Switch, Callout, Separator, Tooltip } from '@radix-ui/themes'
import { Database, Plus, Play, Square, RotateCcw, FileText, Link, Trash2, RefreshCw, AlertCircle, CheckCircle2, Sparkles, ChevronDown, ChevronRight } from 'lucide-react'
import { databaseAPI, dockerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import DockerRequired from '../components/DockerRequired.jsx'

const statusColors = { running: 'green', stopped: 'gray', error: 'red', creating: 'orange' }
const engineColors = { mysql: 'blue', postgres: 'indigo', mariadb: 'teal', redis: 'red' }

const engineDescriptions = {
    mysql: 'The most popular open-source relational database',
    postgres: 'Advanced open-source relational database',
    mariadb: 'Community-developed fork of MySQL',
    redis: 'In-memory data structure store',
}

function generatePassword() {
    const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
    const array = new Uint8Array(16)
    crypto.getRandomValues(array)
    return Array.from(array, (b) => chars[b % chars.length]).join('')
}

export default function DatabaseInstances() {
    const { t } = useTranslation()
    const navigate = useNavigate()

    const [dockerStatus, setDockerStatus] = useState(null)
    const [dockerChecking, setDockerChecking] = useState(true)
    const [instances, setInstances] = useState([])
    const [engines, setEngines] = useState([])
    const [presets, setPresets] = useState({})
    const [pgTuningPresets, setPgTuningPresets] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [actionLoading, setActionLoading] = useState(null)
    const [message, setMessage] = useState(null)
    const [showAdvanced, setShowAdvanced] = useState(false)
    const [form, setForm] = useState({
        name: '',
        engine: '',
        version: '',
        root_password: '',
        port: '',
        memory_limit: '0.5',
        auto_start: true,
        tuning_preset: '',
        config: {},
    })
    const [creating, setCreating] = useState(false)
    const [progressOpen, setProgressOpen] = useState(false)
    const [progressLogs, setProgressLogs] = useState([])
    const [progressDone, setProgressDone] = useState(false)
    const [progressError, setProgressError] = useState(null)
    const progressRef = useRef(null)
    const refreshTimerRef = useRef(null)

    const fetchData = useCallback(async () => {
        try {
            const [instRes, engRes, presetRes, pgTuneRes] = await Promise.allSettled([
                databaseAPI.listInstances(),
                databaseAPI.engines(),
                databaseAPI.presets(),
                databaseAPI.postgresTuningPresets(),
            ])
            if (instRes.status === 'fulfilled') setInstances(instRes.value.data?.instances || [])
            if (engRes.status === 'fulfilled') setEngines(engRes.value.data?.engines || [])
            if (presetRes.status === 'fulfilled') setPresets(presetRes.value.data?.presets || {})
            if (pgTuneRes.status === 'fulfilled') setPgTuningPresets(pgTuneRes.value.data?.presets || [])
        } catch { /* ignore */ } finally { setLoading(false) }
    }, [])

    useEffect(() => { fetchData() }, [fetchData])

    // Auto-refresh every 5s if any instance is in "creating" status
    useEffect(() => {
        const hasCreating = instances.some((i) => i.status === 'creating')
        if (hasCreating) {
            refreshTimerRef.current = setInterval(fetchData, 5000)
        } else {
            if (refreshTimerRef.current) clearInterval(refreshTimerRef.current)
        }
        return () => {
            if (refreshTimerRef.current) clearInterval(refreshTimerRef.current)
        }
    }, [instances, fetchData])

    // Auto-scroll progress log
    useEffect(() => {
        if (progressRef.current) progressRef.current.scrollTop = progressRef.current.scrollHeight
    }, [progressLogs])

    // Check Docker availability
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

    const showMessage = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 4000)
    }

    const doAction = async (id, action, label) => {
        setActionLoading(`${id}-${action}`)
        try {
            await databaseAPI[action](id)
            await fetchData()
        } catch (e) {
            showMessage('error', `${label}: ${e.response?.data?.error || e.message}`)
        } finally { setActionLoading(null) }
    }

    const handleDelete = async (inst) => {
        if (!confirm(t('database.confirm_delete', { name: inst.name }))) return
        setActionLoading(`${inst.id}-delete`)
        try {
            await databaseAPI.deleteInstance(inst.id)
            showMessage('success', t('database.instance_deleted'))
            await fetchData()
        } catch (e) {
            showMessage('error', `${t('common.operation_failed')}: ${e.response?.data?.error || e.message}`)
        } finally { setActionLoading(null) }
    }

    const resetForm = () => {
        setForm({
            name: '',
            engine: '',
            version: '',
            root_password: '',
            port: '',
            memory_limit: '0.5',
            auto_start: true,
            tuning_preset: '',
            config: {},
        })
        setShowAdvanced(false)
    }

    const handleCreate = async () => {
        if (!form.name.trim() || !form.engine) return
        setCreating(true)

        const payload = {
            name: form.name.trim(),
            engine: form.engine,
            version: form.version,
            root_password: form.root_password,
            port: form.port ? parseInt(form.port, 10) : 0,
            memory_limit: form.memory_limit ? form.memory_limit + 'g' : '',
            auto_start: form.auto_start,
        }
        if (form.engine === 'postgres' && form.tuning_preset) {
            payload.tuning_preset = form.tuning_preset
        }
        // Skip explicit config when a tuning preset is selected — backend
        // computes it server-side from the preset + memory budget.
        if (Object.keys(form.config).length > 0 && !payload.tuning_preset) {
            payload.config = form.config
        }

        // Close create dialog, open progress dialog
        setDialogOpen(false)
        setProgressOpen(true)
        setProgressLogs([])
        setProgressDone(false)
        setProgressError(null)

        try {
            const token = localStorage.getItem('token')
            const response = await fetch('/api/plugins/database/instances/stream', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': `Bearer ${token}`,
                },
                body: JSON.stringify(payload),
            })

            if (!response.ok) {
                const text = await response.text()
                setProgressError(text || response.statusText)
                setCreating(false)
                return
            }

            const reader = response.body.getReader()
            const decoder = new TextDecoder()
            let buffer = ''
            let gotError = false
            let gotDone = false

            while (true) {
                const { done, value } = await reader.read()
                if (done) break
                buffer += decoder.decode(value, { stream: true })

                const lines = buffer.split('\n')
                buffer = lines.pop() || ''

                for (const line of lines) {
                    if (line.startsWith('event: error')) {
                        gotError = true
                    } else if (line.startsWith('event: done')) {
                        gotDone = true
                    } else if (line.startsWith('data: ')) {
                        const data = line.slice(6)
                        if (gotError) {
                            setProgressError(data)
                            gotError = false
                        } else {
                            setProgressLogs(prev => [...prev, data])
                        }
                    }
                }
            }

            if (gotDone) {
                setProgressDone(true)
                resetForm()
                await fetchData()
            }
        } catch (e) {
            setProgressError(e.message)
        } finally { setCreating(false) }
    }

    const selectedEngine = engines.find((e) => e.engine === form.engine)

    const runningCount = instances.filter((i) => i.status === 'running').length

    const engineBreakdown = () => {
        const counts = {}
        instances.forEach((i) => {
            const eng = i.engine || 'unknown'
            counts[eng] = (counts[eng] || 0) + 1
        })
        return Object.entries(counts)
            .map(([eng, count]) => `${count} ${eng.charAt(0).toUpperCase() + eng.slice(1)}`)
            .join(', ') || '--'
    }

    if (dockerChecking || loading) {
        return (
            <Flex align="center" justify="center" style={{ minHeight: 200 }}>
                <RefreshCw size={20} className="spin" />
                <Text ml="2">{t('common.loading')}</Text>
            </Flex>
        )
    }

    // Show Docker required screen if Docker is not available
    if (dockerStatus && (!dockerStatus.installed || !dockerStatus.daemon_running)) {
        return (
            <Box>
                <Flex align="center" gap="2" mb="4">
                    <Database size={24} />
                    <Heading size="5">{t('database.title')}</Heading>
                </Flex>
                <DockerRequired
                    installed={dockerStatus.installed}
                    daemonRunning={dockerStatus.daemon_running}
                    error={dockerStatus.error}
                    onRetry={checkDocker}
                    extraMessage={t('database.docker_required')}
                />
            </Box>
        )
    }

    return (
        <Box>
            {/* Header */}
            <Flex align="center" justify="between" mb="4">
                <Flex direction="column" gap="1">
                    <Flex align="center" gap="2">
                        <Database size={24} />
                        <Heading size="5">{t('database.title')}</Heading>
                    </Flex>
                    <Text size="2" color="gray">{t('database.subtitle')}</Text>
                </Flex>
                <Button size="2" onClick={() => { resetForm(); setDialogOpen(true) }}>
                    <Plus size={16} /> {t('database.new_instance')}
                </Button>
            </Flex>

            {/* Message Callout */}
            {message && (
                <Callout.Root
                    color={message.type === 'error' ? 'red' : 'green'}
                    mb="4"
                    size="1"
                >
                    <Callout.Icon>
                        {message.type === 'error' ? <AlertCircle size={16} /> : <CheckCircle2 size={16} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            {/* Summary Cards */}
            {instances.length > 0 && (
                <>
                    <Grid columns={{ initial: '1', sm: '3' }} gap="3" mb="4">
                        <Card style={{ padding: '12px 16px', background: 'var(--cp-card)' }}>
                            <Text size="1" color="gray">{t('database.total_instances')}</Text>
                            <Text size="5" weight="bold" style={{ display: 'block' }}>{instances.length}</Text>
                        </Card>
                        <Card style={{ padding: '12px 16px', background: 'var(--cp-card)' }}>
                            <Text size="1" color="gray">{t('database.running')}</Text>
                            <Text size="5" weight="bold" color="green" style={{ display: 'block' }}>{runningCount}</Text>
                        </Card>
                        <Card style={{ padding: '12px 16px', background: 'var(--cp-card)' }}>
                            <Text size="1" color="gray">{t('database.engine')}</Text>
                            <Text size="3" weight="bold" style={{ display: 'block' }}>{engineBreakdown()}</Text>
                        </Card>
                    </Grid>
                    <Separator size="4" mb="4" />
                </>
            )}

            {/* Instance List */}
            {instances.length === 0 ? (
                <Card style={{ padding: 40, textAlign: 'center', background: 'var(--cp-card)' }}>
                    <Database size={48} style={{ margin: '0 auto 12px', opacity: 0.3, color: 'var(--cp-text)' }} />
                    <Text size="3" color="gray" style={{ display: 'block', marginBottom: 12 }}>
                        {t('database.no_instances')}
                    </Text>
                    <Button onClick={() => { resetForm(); setDialogOpen(true) }}>
                        <Plus size={16} /> {t('database.new_instance')}
                    </Button>
                </Card>
            ) : (
                <Flex direction="column" gap="3">
                    {instances.map((inst) => (
                        <Card key={inst.id} style={{ padding: 16, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                            <Flex align="center" justify="between" wrap="wrap" gap="3">
                                {/* Left side: info */}
                                <Flex direction="column" gap="1" style={{ flex: 1, minWidth: 200 }}>
                                    <Flex align="center" gap="2" wrap="wrap">
                                        <Text
                                            weight="bold"
                                            size="3"
                                            style={{ cursor: 'pointer', color: 'var(--cp-text)' }}
                                            onClick={() => navigate(`/database/${inst.id}`)}
                                        >
                                            {inst.name}
                                        </Text>
                                        <Badge
                                            color={engineColors[inst.engine?.toLowerCase()] || 'gray'}
                                            variant="soft"
                                            size="1"
                                        >
                                            {inst.engine}
                                        </Badge>
                                        {inst.version && (
                                            <Badge color="gray" variant="soft" size="1">
                                                {inst.version}
                                            </Badge>
                                        )}
                                        <Badge
                                            color={statusColors[inst.status] || 'gray'}
                                            variant="soft"
                                            size="1"
                                        >
                                            {t(`database.status_${inst.status}`) || inst.status}
                                        </Badge>
                                    </Flex>
                                    {inst.port && (
                                        <Text size="2" color="gray">
                                            {t('database.port')}: {inst.port}
                                        </Text>
                                    )}
                                </Flex>

                                {/* Right side: actions */}
                                <Flex gap="2" align="center" wrap="wrap">
                                    {inst.status !== 'running' && inst.status !== 'creating' && (
                                        <Tooltip content={t('docker.start')}>
                                            <IconButton
                                                size="2"
                                                variant="soft"
                                                color="green"
                                                disabled={!!actionLoading}
                                                onClick={() => doAction(inst.id, 'startInstance', t('docker.start'))}
                                            >
                                                <Play size={14} />
                                            </IconButton>
                                        </Tooltip>
                                    )}
                                    {inst.status === 'running' && (
                                        <Tooltip content={t('docker.stop')}>
                                            <IconButton
                                                size="2"
                                                variant="soft"
                                                color="orange"
                                                disabled={!!actionLoading}
                                                onClick={() => doAction(inst.id, 'stopInstance', t('docker.stop'))}
                                            >
                                                <Square size={14} />
                                            </IconButton>
                                        </Tooltip>
                                    )}
                                    <Tooltip content={t('docker.restart')}>
                                        <IconButton
                                            size="2"
                                            variant="soft"
                                            disabled={!!actionLoading || inst.status === 'creating'}
                                            onClick={() => doAction(inst.id, 'restartInstance', t('docker.restart'))}
                                        >
                                            <RotateCcw size={14} />
                                        </IconButton>
                                    </Tooltip>
                                    <Tooltip content={t('docker.logs')}>
                                        <IconButton
                                            size="2"
                                            variant="soft"
                                            disabled={inst.status === 'creating'}
                                            onClick={() => navigate(`/database/${inst.id}?tab=logs`)}
                                        >
                                            <FileText size={14} />
                                        </IconButton>
                                    </Tooltip>
                                    <Tooltip content={t('database.tab_connection')}>
                                        <IconButton
                                            size="2"
                                            variant="soft"
                                            disabled={inst.status !== 'running'}
                                            onClick={() => navigate(`/database/${inst.id}?tab=connection`)}
                                        >
                                            <Link size={14} />
                                        </IconButton>
                                    </Tooltip>
                                    <Tooltip content={t('common.delete')}>
                                        <IconButton
                                            size="2"
                                            variant="soft"
                                            color="red"
                                            disabled={!!actionLoading}
                                            onClick={() => handleDelete(inst)}
                                        >
                                            <Trash2 size={14} />
                                        </IconButton>
                                    </Tooltip>
                                </Flex>
                            </Flex>
                        </Card>
                    ))}
                </Flex>
            )}

            {/* Create Instance Dialog */}
            <Dialog.Root open={dialogOpen} onOpenChange={(v) => { if (!v) { setDialogOpen(false); resetForm() } }}>
                <Dialog.Content maxWidth="600px">
                    <Dialog.Title>{t('database.create_instance')}</Dialog.Title>
                    <Flex direction="column" gap="4" mt="3">
                        {/* Engine Selection */}
                        <Box>
                            <Text size="2" weight="bold" mb="2" style={{ display: 'block' }}>
                                {t('database.select_engine')}
                            </Text>
                            <Grid columns="2" gap="2">
                                {engines.map((eng) => (
                                    <Card
                                        key={eng.engine}
                                        style={{
                                            padding: '12px 16px',
                                            cursor: 'pointer',
                                            border: form.engine === eng.engine
                                                ? '2px solid var(--accent-9)'
                                                : '2px solid var(--cp-border)',
                                            background: form.engine === eng.engine
                                                ? 'var(--accent-2)'
                                                : 'var(--cp-card)',
                                            transition: 'border-color 0.15s, background 0.15s',
                                        }}
                                        onClick={() => setForm((f) => ({
                                            ...f,
                                            engine: eng.engine,
                                            version: eng.versions?.[0] || '',
                                            port: String(eng.default_port || ''),
                                            config: {},
                                        }))}
                                    >
                                        <Flex direction="column" gap="1">
                                            <Flex align="center" gap="2">
                                                <Badge
                                                    color={engineColors[eng.engine] || 'gray'}
                                                    variant="soft"
                                                    size="1"
                                                >
                                                    {eng.name}
                                                </Badge>
                                            </Flex>
                                            <Text size="1" color="gray">
                                                {engineDescriptions[eng.engine] || eng.description || eng.name}
                                            </Text>
                                        </Flex>
                                    </Card>
                                ))}
                            </Grid>
                        </Box>

                        {/* Instance Name */}
                        <Box>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                {t('database.instance_name')}
                            </Text>
                            <TextField.Root
                                placeholder={t('database.instance_name_placeholder')}
                                value={form.name}
                                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                            />
                        </Box>

                        {/* Version */}
                        {selectedEngine && selectedEngine.versions?.length > 0 && (
                            <Box>
                                <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                    {t('database.version')}
                                </Text>
                                <Select.Root
                                    value={form.version}
                                    onValueChange={(v) => setForm((f) => ({ ...f, version: v }))}
                                >
                                    <Select.Trigger style={{ width: '100%' }} />
                                    <Select.Content>
                                        {selectedEngine.versions.map((v) => (
                                            <Select.Item key={v} value={v}>{v}</Select.Item>
                                        ))}
                                    </Select.Content>
                                </Select.Root>
                            </Box>
                        )}

                        {/* Root Password */}
                        {form.engine && (
                            <Box>
                                <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                    {t('database.root_password')}{form.engine.toLowerCase() !== 'redis' ? ' *' : ` (${t('common.optional')})`}
                                </Text>
                                <Flex gap="2">
                                    <Box style={{ flex: 1 }}>
                                        <TextField.Root
                                            type="password"
                                            value={form.root_password}
                                            onChange={(e) => setForm((f) => ({ ...f, root_password: e.target.value }))}
                                        />
                                    </Box>
                                    <Button
                                        variant="soft"
                                        size="2"
                                        onClick={() => setForm((f) => ({ ...f, root_password: generatePassword() }))}
                                    >
                                        <Sparkles size={14} />
                                        {t('database.generate_password')}
                                    </Button>
                                </Flex>
                            </Box>
                        )}

                        {/* Port */}
                        <Box>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                {t('database.port')}
                            </Text>
                            <TextField.Root
                                type="number"
                                placeholder="Auto"
                                value={form.port}
                                onChange={(e) => setForm((f) => ({ ...f, port: e.target.value }))}
                            />
                        </Box>

                        {/* Memory Limit */}
                        <Box>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                {t('database.memory_limit')}
                            </Text>
                            <Flex align="center" gap="2">
                                <TextField.Root
                                    type="number"
                                    step="0.1"
                                    min="0.1"
                                    placeholder="0.5"
                                    value={form.memory_limit}
                                    onChange={(e) => setForm((f) => ({ ...f, memory_limit: e.target.value }))}
                                    style={{ flex: 1 }}
                                />
                                <Text size="2" color="gray">GB</Text>
                            </Flex>
                        </Box>

                        {/* PostgreSQL workload tuning preset (postgres only).
                            Backend resolves the EngineConfig from the chosen
                            preset + the memory_limit field, so users don't
                            need to touch advanced settings to get a sensible
                            shared_buffers/work_mem layout. */}
                        {form.engine === 'postgres' && pgTuningPresets.length > 0 && (
                            <Box>
                                <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                    {t('database.tuning_preset')}
                                </Text>
                                <Select.Root
                                    value={form.tuning_preset || 'custom'}
                                    onValueChange={(v) => setForm((f) => ({ ...f, tuning_preset: v === 'custom' ? '' : v }))}
                                >
                                    <Select.Trigger style={{ width: '100%' }} />
                                    <Select.Content>
                                        <Select.Item value="custom">{t('database.tuning_preset_custom')}</Select.Item>
                                        {pgTuningPresets.map((p) => (
                                            <Select.Item key={p.id} value={p.id}>{p.name}</Select.Item>
                                        ))}
                                    </Select.Content>
                                </Select.Root>
                                {form.tuning_preset && (
                                    <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>
                                        {pgTuningPresets.find((p) => p.id === form.tuning_preset)?.description}
                                    </Text>
                                )}
                            </Box>
                        )}

                        {/* Advanced Configuration — Collapsible */}
                        {form.engine && (
                            <Box>
                                <Flex
                                    align="center"
                                    gap="1"
                                    style={{ cursor: 'pointer', userSelect: 'none' }}
                                    onClick={() => setShowAdvanced(!showAdvanced)}
                                >
                                    {showAdvanced ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                                    <Text size="2" weight="bold">{t('database.advanced_config')}</Text>
                                </Flex>

                                {showAdvanced && (
                                    <Box mt="3" p="3" style={{ border: '1px solid var(--cp-border)', borderRadius: 'var(--radius-2)' }}>
                                        {/* Preset buttons */}
                                        <Flex gap="2" mb="3" align="center">
                                            <Text size="2" color="gray">{t('database.preset')}:</Text>
                                            <Button
                                                size="1"
                                                variant="soft"
                                                onClick={() => {
                                                    const p = presets[form.engine]?.development
                                                    if (p) setForm((f) => ({ ...f, config: { ...p } }))
                                                }}
                                            >
                                                {t('database.preset_development')}
                                            </Button>
                                            <Button
                                                size="1"
                                                variant="soft"
                                                color="orange"
                                                onClick={() => {
                                                    const p = presets[form.engine]?.production
                                                    if (p) setForm((f) => ({ ...f, config: { ...p } }))
                                                }}
                                            >
                                                {t('database.preset_production')}
                                            </Button>
                                        </Flex>

                                        <Flex direction="column" gap="3">
                                            {/* MySQL / MariaDB config */}
                                            {(form.engine === 'mysql' || form.engine === 'mariadb') && (
                                                <>
                                                    <Flex gap="3" wrap="wrap">
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.innodb_buffer_pool')}</Text>
                                                            <Flex align="center" gap="1">
                                                                <TextField.Root
                                                                    size="1"
                                                                    placeholder="128"
                                                                    value={form.config.innodb_buffer_pool_size?.replace(/[MGmg]/g, '') || ''}
                                                                    onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, innodb_buffer_pool_size: e.target.value ? e.target.value + 'M' : '' } }))}
                                                                    style={{ flex: 1 }}
                                                                />
                                                                <Text size="1" color="gray">MB</Text>
                                                            </Flex>
                                                        </Box>
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.max_connections')}</Text>
                                                            <TextField.Root
                                                                size="1"
                                                                type="number"
                                                                placeholder="151"
                                                                value={form.config.max_connections || ''}
                                                                onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, max_connections: e.target.value ? parseInt(e.target.value, 10) : undefined } }))}
                                                            />
                                                        </Box>
                                                    </Flex>
                                                    <Flex gap="3" wrap="wrap">
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.character_set')}</Text>
                                                            <Select.Root
                                                                size="1"
                                                                value={form.config.character_set_server || 'utf8mb4'}
                                                                onValueChange={(v) => setForm((f) => ({ ...f, config: { ...f.config, character_set_server: v } }))}
                                                            >
                                                                <Select.Trigger style={{ width: '100%' }} />
                                                                <Select.Content>
                                                                    <Select.Item value="utf8mb4">utf8mb4</Select.Item>
                                                                    <Select.Item value="utf8">utf8</Select.Item>
                                                                    <Select.Item value="latin1">latin1</Select.Item>
                                                                </Select.Content>
                                                            </Select.Root>
                                                        </Box>
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.collation')}</Text>
                                                            <Select.Root
                                                                size="1"
                                                                value={form.config.collation_server || 'utf8mb4_unicode_ci'}
                                                                onValueChange={(v) => setForm((f) => ({ ...f, config: { ...f.config, collation_server: v } }))}
                                                            >
                                                                <Select.Trigger style={{ width: '100%' }} />
                                                                <Select.Content>
                                                                    <Select.Item value="utf8mb4_unicode_ci">utf8mb4_unicode_ci</Select.Item>
                                                                    <Select.Item value="utf8mb4_general_ci">utf8mb4_general_ci</Select.Item>
                                                                    <Select.Item value="utf8mb4_bin">utf8mb4_bin</Select.Item>
                                                                    <Select.Item value="utf8_general_ci">utf8_general_ci</Select.Item>
                                                                    <Select.Item value="latin1_swedish_ci">latin1_swedish_ci</Select.Item>
                                                                </Select.Content>
                                                            </Select.Root>
                                                        </Box>
                                                    </Flex>
                                                    <Flex gap="3" wrap="wrap" align="end">
                                                        <Flex align="center" gap="2" style={{ minWidth: 180 }}>
                                                            <Switch
                                                                size="1"
                                                                checked={form.config.slow_query_log ?? true}
                                                                onCheckedChange={(v) => setForm((f) => ({ ...f, config: { ...f.config, slow_query_log: v } }))}
                                                            />
                                                            <Text size="1">{t('database.slow_query_log')}</Text>
                                                        </Flex>
                                                        {(form.config.slow_query_log ?? true) && (
                                                            <Box style={{ flex: 1, minWidth: 180 }}>
                                                                <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.slow_query_time')}</Text>
                                                                <Flex align="center" gap="1">
                                                                    <TextField.Root
                                                                        size="1"
                                                                        type="number"
                                                                        step="0.5"
                                                                        min="0.1"
                                                                        placeholder="2"
                                                                        value={form.config.long_query_time || ''}
                                                                        onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, long_query_time: e.target.value ? parseFloat(e.target.value) : undefined } }))}
                                                                        style={{ flex: 1 }}
                                                                    />
                                                                    <Text size="1" color="gray">s</Text>
                                                                </Flex>
                                                            </Box>
                                                        )}
                                                    </Flex>
                                                </>
                                            )}

                                            {/* PostgreSQL config */}
                                            {form.engine === 'postgres' && (
                                                <>
                                                    <Flex gap="3" wrap="wrap">
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.shared_buffers')}</Text>
                                                            <Flex align="center" gap="1">
                                                                <TextField.Root
                                                                    size="1"
                                                                    placeholder="128"
                                                                    value={form.config.shared_buffers?.replace(/MB/gi, '') || ''}
                                                                    onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, shared_buffers: e.target.value ? e.target.value + 'MB' : '' } }))}
                                                                    style={{ flex: 1 }}
                                                                />
                                                                <Text size="1" color="gray">MB</Text>
                                                            </Flex>
                                                        </Box>
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.max_connections')}</Text>
                                                            <TextField.Root
                                                                size="1"
                                                                type="number"
                                                                placeholder="100"
                                                                value={form.config.max_connections || ''}
                                                                onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, max_connections: e.target.value ? parseInt(e.target.value, 10) : undefined } }))}
                                                            />
                                                        </Box>
                                                    </Flex>
                                                    <Flex gap="3" wrap="wrap">
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.work_mem')}</Text>
                                                            <Flex align="center" gap="1">
                                                                <TextField.Root
                                                                    size="1"
                                                                    placeholder="4"
                                                                    value={form.config.work_mem?.replace(/MB/gi, '') || ''}
                                                                    onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, work_mem: e.target.value ? e.target.value + 'MB' : '' } }))}
                                                                    style={{ flex: 1 }}
                                                                />
                                                                <Text size="1" color="gray">MB</Text>
                                                            </Flex>
                                                        </Box>
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.effective_cache_size')}</Text>
                                                            <Flex align="center" gap="1">
                                                                <TextField.Root
                                                                    size="1"
                                                                    placeholder="384"
                                                                    value={form.config.effective_cache_size?.replace(/MB/gi, '') || ''}
                                                                    onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, effective_cache_size: e.target.value ? e.target.value + 'MB' : '' } }))}
                                                                    style={{ flex: 1 }}
                                                                />
                                                                <Text size="1" color="gray">MB</Text>
                                                            </Flex>
                                                        </Box>
                                                    </Flex>
                                                    <Flex gap="3" wrap="wrap">
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.wal_level')}</Text>
                                                            <Select.Root
                                                                size="1"
                                                                value={form.config.wal_level || 'replica'}
                                                                onValueChange={(v) => setForm((f) => ({ ...f, config: { ...f.config, wal_level: v } }))}
                                                            >
                                                                <Select.Trigger style={{ width: '100%' }} />
                                                                <Select.Content>
                                                                    <Select.Item value="replica">replica</Select.Item>
                                                                    <Select.Item value="logical">logical</Select.Item>
                                                                    <Select.Item value="minimal">minimal</Select.Item>
                                                                </Select.Content>
                                                            </Select.Root>
                                                        </Box>
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.log_slow_queries')}</Text>
                                                            <TextField.Root
                                                                size="1"
                                                                type="number"
                                                                placeholder="2000"
                                                                value={form.config.log_min_duration_statement ?? ''}
                                                                onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, log_min_duration_statement: e.target.value !== '' ? parseInt(e.target.value, 10) : undefined } }))}
                                                            />
                                                            <Text size="1" color="gray">{t('database.log_slow_queries_hint')}</Text>
                                                        </Box>
                                                    </Flex>
                                                </>
                                            )}

                                            {/* Redis config */}
                                            {form.engine === 'redis' && (
                                                <>
                                                    <Flex gap="3" wrap="wrap">
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.maxmemory')}</Text>
                                                            <Flex align="center" gap="1">
                                                                <TextField.Root
                                                                    size="1"
                                                                    placeholder="256"
                                                                    value={form.config.maxmemory?.replace(/mb/gi, '') || ''}
                                                                    onChange={(e) => setForm((f) => ({ ...f, config: { ...f.config, maxmemory: e.target.value ? e.target.value + 'mb' : '' } }))}
                                                                    style={{ flex: 1 }}
                                                                />
                                                                <Text size="1" color="gray">MB</Text>
                                                            </Flex>
                                                        </Box>
                                                        <Box style={{ flex: 1, minWidth: 180 }}>
                                                            <Text size="1" color="gray" mb="1" style={{ display: 'block' }}>{t('database.maxmemory_policy')}</Text>
                                                            <Select.Root
                                                                size="1"
                                                                value={form.config.maxmemory_policy || 'noeviction'}
                                                                onValueChange={(v) => setForm((f) => ({ ...f, config: { ...f.config, maxmemory_policy: v } }))}
                                                            >
                                                                <Select.Trigger style={{ width: '100%' }} />
                                                                <Select.Content>
                                                                    <Select.Item value="noeviction">{t('database.policy_noeviction')}</Select.Item>
                                                                    <Select.Item value="allkeys-lru">{t('database.policy_allkeys_lru')}</Select.Item>
                                                                    <Select.Item value="volatile-lru">{t('database.policy_volatile_lru')}</Select.Item>
                                                                    <Select.Item value="allkeys-random">{t('database.policy_allkeys_random')}</Select.Item>
                                                                    <Select.Item value="volatile-ttl">{t('database.policy_volatile_ttl')}</Select.Item>
                                                                </Select.Content>
                                                            </Select.Root>
                                                        </Box>
                                                    </Flex>
                                                    <Flex gap="3" wrap="wrap">
                                                        <Flex align="center" gap="2" style={{ minWidth: 180 }}>
                                                            <Switch
                                                                size="1"
                                                                checked={form.config.appendonly ?? false}
                                                                onCheckedChange={(v) => setForm((f) => ({ ...f, config: { ...f.config, appendonly: v } }))}
                                                            />
                                                            <Text size="1">{t('database.appendonly')}</Text>
                                                        </Flex>
                                                        <Flex align="center" gap="2" style={{ minWidth: 180 }}>
                                                            <Switch
                                                                size="1"
                                                                checked={form.config.save !== '' && form.config.save !== undefined}
                                                                onCheckedChange={(v) => setForm((f) => ({ ...f, config: { ...f.config, save: v ? '3600 1 300 100' : '' } }))}
                                                            />
                                                            <Text size="1">{t('database.rdb_save')}</Text>
                                                        </Flex>
                                                    </Flex>
                                                </>
                                            )}
                                        </Flex>
                                    </Box>
                                )}
                            </Box>
                        )}

                        {/* Auto Start Switch */}
                        <Flex align="center" gap="2">
                            <Switch
                                checked={form.auto_start}
                                onCheckedChange={(v) => setForm((f) => ({ ...f, auto_start: v }))}
                            />
                            <Text size="2">{t('database.auto_start')}</Text>
                        </Flex>
                    </Flex>

                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button
                            disabled={creating || !form.name.trim() || !form.engine}
                            onClick={handleCreate}
                        >
                            {creating ? t('common.loading') : t('common.create')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Creation Progress Dialog */}
            <Dialog.Root open={progressOpen} onOpenChange={(v) => { if (!v && (progressDone || progressError)) { setProgressOpen(false) } }}>
                <Dialog.Content maxWidth="600px">
                    <Dialog.Title>
                        <Flex align="center" gap="2">
                            {progressDone ? <CheckCircle2 size={18} color="var(--green-9)" /> : progressError ? <AlertCircle size={18} color="var(--red-9)" /> : <RefreshCw size={16} className="spin" />}
                            {progressDone ? t('database.create_success') : progressError ? t('database.create_failed') : t('database.creating_instance')}
                        </Flex>
                    </Dialog.Title>
                    <Box
                        ref={progressRef}
                        style={{
                            background: 'var(--gray-2)',
                            borderRadius: 8,
                            padding: 12,
                            maxHeight: 300,
                            minHeight: 100,
                            overflow: 'auto',
                            fontFamily: 'monospace',
                            fontSize: '0.8rem',
                            whiteSpace: 'pre-wrap',
                            lineHeight: 1.6,
                        }}
                    >
                        {progressLogs.length > 0 ? progressLogs.join('\n') : (progressError || t('database.creating_instance') + '...')}
                    </Box>
                    {progressError && !progressDone && (
                        <Callout.Root color="red" size="1" mt="2">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{progressError}</Callout.Text>
                        </Callout.Root>
                    )}
                    <Flex justify="end" mt="3">
                        <Button
                            variant="soft"
                            disabled={!progressDone && !progressError}
                            onClick={() => setProgressOpen(false)}
                        >
                            {t('common.close')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
