import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Heading, Text, Card, Button, TextField, Badge, Tabs, Dialog, Select, Separator, IconButton, Tooltip } from '@radix-ui/themes'
import { Store, Search, RefreshCw, Settings2, Plus, Trash2, Play, Square, ArrowUpCircle, ExternalLink, Package, Copy, Check } from 'lucide-react'
import { useNavigate } from 'react-router'
import { appstoreAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

export default function AppStore() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const [loading, setLoading] = useState(true)
    const [apps, setApps] = useState([])
    const [installed, setInstalled] = useState([])
    const [categories, setCategories] = useState([])
    const [updates, setUpdates] = useState([])
    const [search, setSearch] = useState('')
    const [category, setCategory] = useState('')
    const [page, setPage] = useState(1)
    const [total, setTotal] = useState(0)
    const [tab, setTab] = useState('apps')
    const [sourcesOpen, setSourcesOpen] = useState(false)
    const [sources, setSources] = useState([])
    const [newSource, setNewSource] = useState({ name: '', url: '', branch: 'main' })
    const [actionLoading, setActionLoading] = useState({})
    const [syncDialog, setSyncDialog] = useState(false)
    const [syncLogs, setSyncLogs] = useState([])
    const [syncDone, setSyncDone] = useState(false)
    const [syncError, setSyncError] = useState(false)
    const [syncCopied, setSyncCopied] = useState(false)
    const syncLogsEndRef = useRef(null)
    const autoSyncTriggered = useRef(false)

    const pageSize = 24

    const fetchApps = useCallback(async () => {
        try {
            const res = await appstoreAPI.listApps({ category, search, page, page_size: pageSize })
            setApps(res.data?.apps || [])
            setTotal(res.data?.total || 0)
        } catch { /* ignore */ }
    }, [category, search, page])

    const fetchInstalled = useCallback(async () => {
        try {
            const res = await appstoreAPI.listInstalled()
            setInstalled(res.data?.apps || [])
        } catch { /* ignore */ }
    }, [])

    const fetchCategories = useCallback(async () => {
        try {
            const res = await appstoreAPI.listCategories()
            setCategories(res.data?.categories || [])
        } catch { /* ignore */ }
    }, [])

    const fetchUpdates = useCallback(async () => {
        try {
            const res = await appstoreAPI.checkUpdates()
            setUpdates(res.data?.updates || [])
        } catch { /* ignore */ }
    }, [])

    const fetchSources = useCallback(async () => {
        try {
            const res = await appstoreAPI.listSources()
            setSources(res.data?.sources || [])
        } catch { /* ignore */ }
    }, [])

    useEffect(() => {
        Promise.allSettled([fetchApps(), fetchInstalled(), fetchCategories(), fetchUpdates(), fetchSources()])
            .finally(() => setLoading(false))
    }, [fetchApps, fetchInstalled, fetchCategories, fetchUpdates, fetchSources])

    useEffect(() => { fetchApps() }, [fetchApps])

    // Auto-sync sources on first visit when no apps exist
    useEffect(() => {
        if (loading || autoSyncTriggered.current) return
        if (apps.length > 0 || sources.length === 0) return
        const unsynced = sources.find(s => s.sync_status !== 'synced')
        if (!unsynced) return
        autoSyncTriggered.current = true
        handleSyncSource(unsynced.id)
    }, [loading, apps.length, sources])

    const handleSearch = (e) => {
        setSearch(e.target.value)
        setPage(1)
    }

    const handleCategoryChange = (val) => {
        setCategory(val === 'all' ? '' : val)
        setPage(1)
    }

    const isInstalled = (appId) => installed.some(a => a.app_id === appId)

    // Installed app actions
    const doAction = async (id, action, name) => {
        const key = `${id}-${action}`
        setActionLoading(prev => ({ ...prev, [key]: true }))
        try {
            if (action === 'start') await appstoreAPI.startApp(id)
            else if (action === 'stop') await appstoreAPI.stopApp(id)
            else if (action === 'update') await appstoreAPI.updateApp(id)
            else if (action === 'uninstall') await appstoreAPI.uninstall(id, false)
            await fetchInstalled()
            await fetchUpdates()
        } catch { /* ignore */ }
        finally { setActionLoading(prev => ({ ...prev, [key]: false })) }
    }

    // Source management
    const handleAddSource = async () => {
        if (!newSource.name || !newSource.url) return
        try {
            await appstoreAPI.addSource(newSource)
            setNewSource({ name: '', url: '', branch: 'main' })
            await fetchSources()
        } catch { /* ignore */ }
    }

    const handleSyncSource = (id) => {
        setSyncLogs([])
        setSyncDone(false)
        setSyncError(false)
        setSyncDialog(true)

        const token = localStorage.getItem('token')
        fetch(appstoreAPI.syncSourceStreamUrl(id), {
            headers: { 'Authorization': `Bearer ${token}` },
        }).then(async (response) => {
            const reader = response.body.getReader()
            const decoder = new TextDecoder()
            let buffer = ''

            while (true) {
                const { done, value } = await reader.read()
                if (done) break
                buffer += decoder.decode(value, { stream: true })

                const lines = buffer.split('\n')
                buffer = lines.pop() || ''

                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        setSyncLogs((prev) => [...prev, line.slice(6)])
                    } else if (line.startsWith('event: done')) {
                        setSyncDone(true)
                    } else if (line.startsWith('event: error')) {
                        setSyncError(true)
                    }
                }
            }

            setSyncDone((prev) => prev || true)
            fetchSources()
            fetchApps()
            fetchCategories()
        }).catch((err) => {
            setSyncLogs((prev) => [...prev, `ERROR: ${err.message}`])
            setSyncError(true)
        })
    }

    const handleCopySyncLogs = () => {
        navigator.clipboard.writeText(syncLogs.join('\n'))
        setSyncCopied(true)
        setTimeout(() => setSyncCopied(false), 2000)
    }

    const handleRemoveSource = async (id) => {
        try {
            await appstoreAPI.removeSource(id)
            await fetchSources()
            await fetchApps()
        } catch { /* ignore */ }
    }

    if (loading) {
        return (
            <Flex align="center" justify="center" style={{ minHeight: 200 }}>
                <RefreshCw size={20} className="spin" />
                <Text ml="2">{t('common.loading')}</Text>
            </Flex>
        )
    }

    const totalPages = Math.ceil(total / pageSize)
    const hasUpdate = (appId) => updates.some(u => u.app_id === appId)

    return (
        <Box>
            {/* Header */}
            <Flex align="center" justify="between" mb="4" wrap="wrap" gap="3">
                <Flex align="center" gap="2">
                    <Store size={24} />
                    <Box>
                        <Heading size="5">{t('appstore.title')}</Heading>
                        <Text size="2" color="gray">{t('appstore.subtitle')}</Text>
                    </Box>
                </Flex>
                <Flex gap="2">
                    {updates.length > 0 && (
                        <Badge color="orange" size="2">{t('appstore.updates_available', { count: updates.length })}</Badge>
                    )}
                    <Button size="2" variant="soft" onClick={() => { setSourcesOpen(true); fetchSources() }}>
                        <Settings2 size={14} /> {t('appstore.sources')}
                    </Button>
                </Flex>
            </Flex>

            {/* Tabs */}
            <Tabs.Root value={tab} onValueChange={setTab}>
                <Tabs.List>
                    <Tabs.Trigger value="apps">{t('appstore.tab_apps')} ({total})</Tabs.Trigger>
                    <Tabs.Trigger value="installed">{t('appstore.tab_installed')} ({installed.length})</Tabs.Trigger>
                </Tabs.List>

                {/* ─── Apps Tab ─── */}
                <Tabs.Content value="apps">
                    <Flex gap="3" mt="4" mb="4" wrap="wrap">
                        <Box style={{ flex: 1, minWidth: 200 }}>
                            <TextField.Root
                                placeholder={t('appstore.search_placeholder')}
                                value={search}
                                onChange={handleSearch}
                            >
                                <TextField.Slot><Search size={14} /></TextField.Slot>
                            </TextField.Root>
                        </Box>
                        <Select.Root value={category || 'all'} onValueChange={handleCategoryChange}>
                            <Select.Trigger placeholder={t('appstore.categories')} />
                            <Select.Content>
                                <Select.Item value="all">{t('appstore.all_categories')}</Select.Item>
                                {categories.map(c => (
                                    <Select.Item key={c} value={c}>{c}</Select.Item>
                                ))}
                            </Select.Content>
                        </Select.Root>
                        <Button size="2" variant="soft" onClick={() => { fetchApps(); fetchCategories() }}>
                            <RefreshCw size={14} /> {t('common.refresh')}
                        </Button>
                    </Flex>

                    {apps.length === 0 ? (
                        <Card>
                            <Flex align="center" justify="center" direction="column" gap="2" p="6">
                                <Package size={40} style={{ opacity: 0.3 }} />
                                <Text size="3" color="gray">
                                    {search ? t('appstore.no_results') : t('appstore.no_apps')}
                                </Text>
                            </Flex>
                        </Card>
                    ) : (
                        <>
                            <Box style={{
                                display: 'grid',
                                gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
                                gap: 'var(--space-3)',
                            }}>
                                {apps.map(app => {
                                    const cats = (() => { try { return JSON.parse(app.categories || '[]') } catch { return [] } })()
                                    return (
                                        <Card key={app.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/store/app/${app.id}`)}>
                                            <Flex direction="column" gap="2">
                                                <Flex align="center" gap="3">
                                                    <img
                                                        src={appstoreAPI.appLogoUrl(app.id)}
                                                        alt={app.name}
                                                        style={{ width: 48, height: 48, borderRadius: 8, objectFit: 'cover', background: 'var(--gray-3)' }}
                                                        onError={(e) => { e.target.style.display = 'none' }}
                                                    />
                                                    <Box style={{ flex: 1 }}>
                                                        <Text size="3" weight="bold">{app.name}</Text>
                                                        {app.version && <Text size="1" color="gray"> v{app.version}</Text>}
                                                        {isInstalled(app.app_id) && (
                                                            <Badge color="green" size="1" ml="2">{t('appstore.installed_badge')}</Badge>
                                                        )}
                                                    </Box>
                                                </Flex>
                                                <Text size="2" color="gray" style={{
                                                    display: '-webkit-box',
                                                    WebkitLineClamp: 2,
                                                    WebkitBoxOrient: 'vertical',
                                                    overflow: 'hidden',
                                                    minHeight: 36,
                                                }}>
                                                    {app.short_desc || app.name}
                                                </Text>
                                                <Flex gap="1" wrap="wrap">
                                                    {cats.slice(0, 3).map(c => (
                                                        <Badge key={c} variant="soft" size="1">{c}</Badge>
                                                    ))}
                                                </Flex>
                                            </Flex>
                                        </Card>
                                    )
                                })}
                            </Box>

                            {/* Pagination */}
                            {totalPages > 1 && (
                                <Flex justify="center" gap="2" mt="4">
                                    <Button size="1" variant="soft" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>
                                        {t('common.prev_page')}
                                    </Button>
                                    <Text size="2" style={{ lineHeight: '32px' }}>{page} / {totalPages}</Text>
                                    <Button size="1" variant="soft" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>
                                        {t('common.next_page')}
                                    </Button>
                                </Flex>
                            )}
                        </>
                    )}
                </Tabs.Content>

                {/* ─── Installed Tab ─── */}
                <Tabs.Content value="installed">
                    <Box mt="4">
                        {installed.length === 0 ? (
                            <Card>
                                <Flex align="center" justify="center" direction="column" gap="2" p="6">
                                    <Package size={40} style={{ opacity: 0.3 }} />
                                    <Text size="3" color="gray">{t('appstore.no_installed')}</Text>
                                </Flex>
                            </Card>
                        ) : (
                            <Flex direction="column" gap="3">
                                {installed.map(app => (
                                    <Card key={app.id}>
                                        <Flex align="center" justify="between" wrap="wrap" gap="3">
                                            <Flex align="center" gap="3">
                                                <Box>
                                                    <Flex align="center" gap="2">
                                                        <Text size="3" weight="bold">{app.name}</Text>
                                                        <Badge
                                                            color={app.status === 'running' ? 'green' : app.status === 'error' ? 'red' : 'gray'}
                                                            size="1"
                                                        >
                                                            {app.status}
                                                        </Badge>
                                                        {hasUpdate(app.app_id) && (
                                                            <Badge color="orange" size="1">{t('appstore.update_available')}</Badge>
                                                        )}
                                                    </Flex>
                                                    <Text size="2" color="gray">
                                                        {app.app_name} v{app.version}
                                                        {app.domain && <> &middot; <a href={`https://${app.domain}${app.url_suffix || ''}`} target="_blank" rel="noopener noreferrer" style={{ color: 'var(--accent-11)' }}>{app.domain}{app.url_suffix || ''}</a></>}
                                                    </Text>
                                                </Box>
                                            </Flex>
                                            <Flex gap="2">
                                                {app.status === 'stopped' && (
                                                    <Button size="1" variant="soft" color="green"
                                                        loading={actionLoading[`${app.id}-start`]}
                                                        onClick={() => doAction(app.id, 'start', app.name)}>
                                                        <Play size={12} /> {t('appstore.start')}
                                                    </Button>
                                                )}
                                                {app.status === 'running' && (
                                                    <Button size="1" variant="soft"
                                                        loading={actionLoading[`${app.id}-stop`]}
                                                        onClick={() => doAction(app.id, 'stop', app.name)}>
                                                        <Square size={12} /> {t('appstore.stop')}
                                                    </Button>
                                                )}
                                                {hasUpdate(app.app_id) && (
                                                    <Button size="1" variant="soft" color="orange"
                                                        loading={actionLoading[`${app.id}-update`]}
                                                        onClick={() => doAction(app.id, 'update', app.name)}>
                                                        <ArrowUpCircle size={12} /> {t('appstore.update')}
                                                    </Button>
                                                )}
                                                <Button size="1" variant="soft" color="red"
                                                    loading={actionLoading[`${app.id}-uninstall`]}
                                                    onClick={() => { if (confirm(t('appstore.confirm_uninstall', { name: app.name }))) doAction(app.id, 'uninstall', app.name) }}>
                                                    <Trash2 size={12} /> {t('appstore.uninstall')}
                                                </Button>
                                            </Flex>
                                        </Flex>
                                    </Card>
                                ))}
                            </Flex>
                        )}
                    </Box>
                </Tabs.Content>
            </Tabs.Root>

            {/* ─── Sources Dialog ─── */}
            <Dialog.Root open={sourcesOpen} onOpenChange={setSourcesOpen}>
                <Dialog.Content maxWidth="600px">
                    <Dialog.Title>{t('appstore.sources')}</Dialog.Title>

                    <Flex direction="column" gap="3" mt="3">
                        {sources.map(src => (
                            <Card key={src.id} variant="surface">
                                <Flex align="center" justify="between" wrap="wrap" gap="2">
                                    <Box>
                                        <Flex align="center" gap="2">
                                            <Text weight="bold">{src.name}</Text>
                                            <Badge size="1" variant="soft" color={src.is_default ? 'blue' : 'gray'}>
                                                {src.is_default ? t('appstore.official') : t('appstore.custom')}
                                            </Badge>
                                            <Badge size="1" variant="soft"
                                                color={src.sync_status === 'synced' ? 'green' : src.sync_status === 'error' ? 'red' : src.sync_status === 'syncing' ? 'orange' : 'gray'}>
                                                {src.sync_status === 'synced' ? t('appstore.synced') : src.sync_status === 'error' ? t('appstore.sync_error') : src.sync_status === 'syncing' ? t('appstore.syncing') : 'pending'}
                                            </Badge>
                                        </Flex>
                                        <Text size="1" color="gray" style={{ wordBreak: 'break-all' }}>{src.url}</Text>
                                        {src.last_sync_at && (
                                            <Text size="1" color="gray"> &middot; {t('appstore.last_synced')}: {new Date(src.last_sync_at).toLocaleString()}</Text>
                                        )}
                                        {src.sync_error && (
                                            <Text size="1" color="red">{src.sync_error}</Text>
                                        )}
                                    </Box>
                                    <Flex gap="2">
                                        <Button size="1" variant="soft"
                                            loading={actionLoading[`sync-${src.id}`]}
                                            onClick={() => handleSyncSource(src.id)}>
                                            <RefreshCw size={12} /> {t('appstore.sync')}
                                        </Button>
                                        {!src.is_default && (
                                            <IconButton size="1" variant="soft" color="red" onClick={() => handleRemoveSource(src.id)}>
                                                <Trash2 size={12} />
                                            </IconButton>
                                        )}
                                    </Flex>
                                </Flex>
                            </Card>
                        ))}
                    </Flex>

                    <Separator size="4" my="3" />

                    <Text size="2" weight="bold" mb="2">{t('appstore.add_source')}</Text>
                    <Flex direction="column" gap="2">
                        <TextField.Root
                            placeholder={t('appstore.source_name')}
                            value={newSource.name}
                            onChange={e => setNewSource(prev => ({ ...prev, name: e.target.value }))}
                        />
                        <TextField.Root
                            placeholder={t('appstore.source_url')}
                            value={newSource.url}
                            onChange={e => setNewSource(prev => ({ ...prev, url: e.target.value }))}
                        />
                        <Flex gap="2">
                            <TextField.Root
                                placeholder={t('appstore.source_branch')}
                                value={newSource.branch}
                                onChange={e => setNewSource(prev => ({ ...prev, branch: e.target.value }))}
                                style={{ flex: 1 }}
                            />
                            <Button onClick={handleAddSource} disabled={!newSource.name || !newSource.url}>
                                <Plus size={14} /> {t('appstore.add_source')}
                            </Button>
                        </Flex>
                    </Flex>

                    <Flex justify="end" mt="4">
                        <Dialog.Close>
                            <Button variant="soft">{t('common.close')}</Button>
                        </Dialog.Close>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Sync Log Dialog */}
            <Dialog.Root open={syncDialog} onOpenChange={(open) => { if (!open) setSyncDialog(false) }}>
                <Dialog.Content maxWidth="600px">
                    <Dialog.Title>
                        {syncError ? t('appstore.sync_error') : syncDone ? t('appstore.synced') : t('appstore.syncing')}
                    </Dialog.Title>
                    <Box
                        style={{
                            background: 'var(--cp-surface)',
                            border: '1px solid var(--cp-border)',
                            borderRadius: 8,
                            padding: 12,
                            maxHeight: 300,
                            overflowY: 'auto',
                            fontFamily: 'monospace',
                            fontSize: '0.8rem',
                            lineHeight: 1.6,
                        }}
                    >
                        {syncLogs.map((log, i) => (
                            <Text
                                key={i}
                                size="1"
                                style={{
                                    display: 'block',
                                    color: log.startsWith('ERROR') ? 'var(--red-11)' : 'var(--cp-text)',
                                    whiteSpace: 'pre-wrap',
                                    wordBreak: 'break-all',
                                }}
                            >
                                {log}
                            </Text>
                        ))}
                        {!syncDone && !syncError && (
                            <Flex align="center" gap="1" mt="1">
                                <RefreshCw size={12} className="spin" />
                                <Text size="1" color="gray">{t('appstore.syncing')}</Text>
                            </Flex>
                        )}
                        <div ref={syncLogsEndRef} />
                    </Box>
                    <Flex justify="between" mt="3">
                        <Button variant="soft" size="1" onClick={handleCopySyncLogs}>
                            {syncCopied ? <Check size={14} /> : <Copy size={14} />}
                            {t('plugins.copy_logs')}
                        </Button>
                        <Dialog.Close>
                            <Button variant="solid" size="1">{t('common.close')}</Button>
                        </Dialog.Close>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
