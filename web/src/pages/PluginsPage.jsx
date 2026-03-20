import { useState, useEffect, useRef } from 'react'
import { Box, Flex, Text, Card, Badge, Separator, Button, Dialog, Switch, IconButton } from '@radix-ui/themes'
import { Package, Download, Trash2, RefreshCw, Eye, EyeOff, Copy, Check, X, Power } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { pluginAPI } from '../api/index.js'
import { usePluginNavStore } from '../stores/pluginNav.js'
import { copyToClipboard } from '../utils/clipboard.js'

const categoryColors = { deploy: 'blue', database: 'green', tool: 'orange', monitor: 'purple' }

export default function PluginsPage() {
    const { t } = useTranslation()
    const refreshNav = usePluginNavStore((s) => s.refresh)
    const [plugins, setPlugins] = useState([])
    const [loading, setLoading] = useState(true)
    const [toggling, setToggling] = useState(null)
    const [installDialog, setInstallDialog] = useState(null) // plugin id being installed
    const [installLogs, setInstallLogs] = useState([])
    const [installDone, setInstallDone] = useState(false)
    const [installError, setInstallError] = useState(false)
    const [copied, setCopied] = useState(false)
    const logsEndRef = useRef(null)
    const installLogsRef = useRef([])

    const fetchPlugins = async () => {
        try {
            const res = await pluginAPI.list()
            setPlugins(res.data?.plugins || [])
        } catch { /* ignore */ }
        finally { setLoading(false) }
    }

    useEffect(() => { fetchPlugins() }, [])

    useEffect(() => {
        if (logsEndRef.current) {
            logsEndRef.current.scrollIntoView({ behavior: 'smooth' })
        }
    }, [installLogs])

    const handleInstall = (id) => {
        setInstallDialog(id)
        setInstallLogs([])
        installLogsRef.current = []
        setInstallDone(false)
        setInstallError(false)

        const token = localStorage.getItem('token')
        fetch(`/api/plugins/${id}/install`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${token}`,
            },
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
                        const data = line.slice(6)
                        installLogsRef.current = [...installLogsRef.current, data]
                        setInstallLogs((prev) => [...prev, data])
                    } else if (line.startsWith('event: done')) {
                        setInstallDone(true)
                    } else if (line.startsWith('event: error')) {
                        setInstallError(true)
                    }
                }
            }

            // Process remaining buffer
            if (buffer) {
                const lines = buffer.split('\n')
                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        const data = line.slice(6)
                        installLogsRef.current = [...installLogsRef.current, data]
                        setInstallLogs((prev) => [...prev, data])
                    }
                }
            }

            // If we didn't receive explicit done/error events, mark as done
            setInstallDone((prev) => {
                if (!prev) return true
                return prev
            })
        }).catch((err) => {
            const msg = `ERROR: ${err.message}`
            installLogsRef.current = [...installLogsRef.current, msg]
            setInstallLogs((prev) => [...prev, msg])
            setInstallError(true)
        })
    }

    const handleUninstall = async (id) => {
        setToggling(id)
        try {
            await pluginAPI.disable(id)
            await fetchPlugins()
            refreshNav()
        } catch { /* ignore */ }
        finally { setToggling(null) }
    }

    const handleToggleEnabled = async (id, currentEnabled) => {
        setToggling(id)
        try {
            if (currentEnabled) {
                await pluginAPI.disable(id)
            } else {
                await pluginAPI.enable(id)
            }
            await fetchPlugins()
            refreshNav()
        } catch (e) {
            const msg = e.response?.data?.error || e.message
            if (msg) alert(msg)
        } finally { setToggling(null) }
    }

    const handleSidebarToggle = async (id, visible) => {
        try {
            await pluginAPI.setSidebarVisible(id, visible)
            await fetchPlugins()
            refreshNav()
        } catch { /* ignore */ }
    }

    const handleCloseInstallDialog = () => {
        setInstallDialog(null)
        fetchPlugins()
        refreshNav()
    }

    const handleCopyLogs = () => {
        const text = installLogsRef.current.join('\n')
        copyToClipboard(text, () => {
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
        })
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
            <Flex align="center" justify="between" mb="2">
                <Box>
                    <Text size="5" weight="bold">{t('plugins.title')}</Text>
                    <Text size="2" color="gray" mt="1" style={{ display: 'block' }}>
                        {t('plugins.page_description')}
                    </Text>
                </Box>
                <Badge variant="soft" size="2">{plugins.length} {t('plugins.installed')}</Badge>
            </Flex>
            <Separator size="4" mb="4" />

            {plugins.length === 0 ? (
                <Card style={{ padding: 40, textAlign: 'center' }}>
                    <Package size={48} style={{ margin: '0 auto 12px', opacity: 0.3 }} />
                    <Text size="3" color="gray">{t('plugins.empty')}</Text>
                </Card>
            ) : (
                <Flex direction="column" gap="3">
                    {plugins.map((p) => (
                        <Card key={p.id} style={{ padding: 16 }}>
                            <Flex align="start" justify="between" gap="4">
                                <Flex direction="column" gap="1" style={{ flex: 1 }}>
                                    <Flex align="center" gap="2" wrap="wrap">
                                        <Text weight="bold" size="3">{t(`plugins.names.${p.id}`, { defaultValue: p.name })}</Text>
                                        <Badge variant="soft" size="1">v{p.version}</Badge>
                                        {p.category && (
                                            <Badge color={categoryColors[p.category] || 'gray'} variant="soft" size="1">
                                                {t(`plugins.categories.${p.category}`, { defaultValue: p.category })}
                                            </Badge>
                                        )}
                                        {p.enabled && (
                                            <Badge color="green" variant="soft" size="1">
                                                {t('common.enabled')}
                                            </Badge>
                                        )}
                                    </Flex>
                                    <Text size="2" color="gray">{t(`plugins.descriptions.${p.id}`, { defaultValue: p.description })}</Text>
                                    {p.dependencies?.length > 0 && (
                                        <Flex align="center" gap="1" wrap="wrap" mt="1">
                                            <Text size="1" color="gray">{t('plugins.depends_on')}:</Text>
                                            {p.dependencies.map((dep) => {
                                                const depPlugin = plugins.find((pp) => pp.id === dep)
                                                const isDepEnabled = depPlugin?.enabled
                                                return (
                                                    <Badge
                                                        key={dep}
                                                        color={isDepEnabled ? 'green' : 'red'}
                                                        variant="surface"
                                                        size="1"
                                                        style={{ cursor: 'default' }}
                                                    >
                                                        <Power size={10} />
                                                        {t(`plugins.names.${dep}`, { defaultValue: depPlugin?.name || dep })}
                                                    </Badge>
                                                )
                                            })}
                                        </Flex>
                                    )}
                                </Flex>
                                <Flex align="center" gap="2">
                                    {p.enabled && (
                                        <IconButton
                                            variant="ghost"
                                            size="1"
                                            title={p.show_in_sidebar ? t('plugins.hide_sidebar') : t('plugins.show_sidebar')}
                                            onClick={() => handleSidebarToggle(p.id, !p.show_in_sidebar)}
                                        >
                                            {p.show_in_sidebar ? <Eye size={16} /> : <EyeOff size={16} />}
                                        </IconButton>
                                    )}
                                    <Switch
                                        checked={p.enabled}
                                        disabled={toggling === p.id}
                                        onCheckedChange={() => handleToggleEnabled(p.id, p.enabled)}
                                    />
                                </Flex>
                            </Flex>
                        </Card>
                    ))}
                </Flex>
            )}

            {/* Install log dialog */}
            <Dialog.Root open={!!installDialog} onOpenChange={(open) => { if (!open) handleCloseInstallDialog() }}>
                <Dialog.Content maxWidth="600px">
                    <Dialog.Title>
                        {installError ? t('plugins.install_failed') : installDone ? t('plugins.install_success') : t('plugins.installing')}
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
                        {installLogs.map((log, i) => (
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
                        {!installDone && !installError && (
                            <Flex align="center" gap="1" mt="1">
                                <RefreshCw size={12} className="spin" />
                                <Text size="1" color="gray">{t('plugins.installing')}</Text>
                            </Flex>
                        )}
                        <div ref={logsEndRef} />
                    </Box>
                    <Flex justify="between" mt="3">
                        <Button variant="soft" size="1" onClick={handleCopyLogs}>
                            {copied ? <Check size={14} /> : <Copy size={14} />}
                            {t('plugins.copy_logs')}
                        </Button>
                        <Dialog.Close>
                            <Button variant="solid" size="1">
                                {t('common.close')}
                            </Button>
                        </Dialog.Close>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
