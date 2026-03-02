import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Grid, Card, Button, IconButton, Text, Heading, Badge, Dialog, TextField, Select, Switch, Callout, Separator, Tooltip } from '@radix-ui/themes'
import { Database, Plus, Play, Square, RotateCcw, FileText, Link, Trash2, RefreshCw, AlertCircle, CheckCircle2, Sparkles } from 'lucide-react'
import { databaseAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

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

    const [instances, setInstances] = useState([])
    const [engines, setEngines] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [actionLoading, setActionLoading] = useState(null)
    const [message, setMessage] = useState(null)
    const [form, setForm] = useState({
        name: '',
        engine: '',
        version: '',
        root_password: '',
        port: '',
        memory_limit: '512m',
        auto_start: true,
    })
    const [creating, setCreating] = useState(false)
    const refreshTimerRef = useRef(null)

    const fetchData = useCallback(async () => {
        try {
            const [instRes, engRes] = await Promise.allSettled([
                databaseAPI.listInstances(),
                databaseAPI.engines(),
            ])
            if (instRes.status === 'fulfilled') setInstances(instRes.value.data?.instances || [])
            if (engRes.status === 'fulfilled') setEngines(engRes.value.data?.engines || [])
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
            memory_limit: '512m',
            auto_start: true,
        })
    }

    const handleCreate = async () => {
        if (!form.name.trim() || !form.engine) return
        setCreating(true)
        try {
            await databaseAPI.createInstance({
                name: form.name.trim(),
                engine: form.engine,
                version: form.version,
                root_password: form.root_password,
                port: form.port ? parseInt(form.port, 10) : 0,
                memory_limit: form.memory_limit,
                auto_start: form.auto_start,
            })
            showMessage('success', t('database.instance_created'))
            setDialogOpen(false)
            resetForm()
            await fetchData()
        } catch (e) {
            showMessage('error', `${t('common.operation_failed')}: ${e.response?.data?.error || e.message}`)
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

    if (loading) {
        return (
            <Flex align="center" justify="center" style={{ minHeight: 200 }}>
                <RefreshCw size={20} className="spin" />
                <Text ml="2">{t('common.loading')}</Text>
            </Flex>
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

                        {/* Root Password (not for Redis) */}
                        {form.engine && form.engine.toLowerCase() !== 'redis' && (
                            <Box>
                                <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                    {t('database.root_password')}
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
                            <TextField.Root
                                placeholder="512m"
                                value={form.memory_limit}
                                onChange={(e) => setForm((f) => ({ ...f, memory_limit: e.target.value }))}
                            />
                        </Box>

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
        </Box>
    )
}
