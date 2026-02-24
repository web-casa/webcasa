import { useState, useEffect, useRef } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Badge, Callout, Separator, Code,
    Tabs, Switch, TextField,
} from '@radix-ui/themes'
import {
    Play, Square, RefreshCw, Download, Upload, Server, FileCode,
    AlertCircle, CheckCircle2,
} from 'lucide-react'
import { caddyAPI, configAPI, settingAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

export default function Settings() {
    const { t } = useTranslation()
    const [caddyStatus, setCaddyStatus] = useState(null)
    const [caddyfile, setCaddyfile] = useState('')
    const [actionLoading, setActionLoading] = useState(null)
    const [message, setMessage] = useState(null)
    const fileInputRef = useRef(null)
    const [autoReload, setAutoReload] = useState(true)
    const [serverIpv4, setServerIpv4] = useState('')
    const [serverIpv6, setServerIpv6] = useState('')

    const fetchStatus = async () => {
        try {
            const res = await caddyAPI.status()
            setCaddyStatus(res.data)
        } catch (err) {
            console.error('Failed to fetch Caddy status:', err)
        }
    }

    const fetchCaddyfile = async () => {
        try {
            const res = await caddyAPI.caddyfile()
            setCaddyfile(res.data.content || '')
        } catch {
            setCaddyfile('# Caddyfile not found')
        }
    }

    const fetchSettings = async () => {
        try {
            const res = await settingAPI.getAll()
            const settings = res.data.settings || {}
            setAutoReload(settings.auto_reload !== 'false')
            setServerIpv4(settings.server_ipv4 || '')
            setServerIpv6(settings.server_ipv6 || '')
        } catch { /* ignore */ }
    }

    useEffect(() => {
        fetchStatus()
        fetchCaddyfile()
        fetchSettings()
    }, [])

    const showMessage = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 5000)
    }

    const handleCaddyAction = async (action) => {
        setActionLoading(action)
        try {
            let res
            switch (action) {
                case 'start':
                    res = await caddyAPI.start()
                    break
                case 'stop':
                    res = await caddyAPI.stop()
                    break
                case 'reload':
                    res = await caddyAPI.reload()
                    break
            }
            showMessage('success', res.data.message)
            await fetchStatus()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('settings.action_failed', { action }))
        } finally {
            setActionLoading(null)
        }
    }

    const handleToggleAutoReload = async (value) => {
        setAutoReload(value)
        try {
            await settingAPI.update('auto_reload', value ? 'true' : 'false')
            showMessage('success', value ? t('settings.auto_reload_on') : t('settings.auto_reload_off'))
        } catch {
            setAutoReload(!value) // revert on error
            showMessage('error', t('settings.save_failed'))
        }
    }

    const handleSaveIPs = async () => {
        try {
            await Promise.all([
                settingAPI.update('server_ipv4', serverIpv4),
                settingAPI.update('server_ipv6', serverIpv6),
            ])
            showMessage('success', t('settings.ip_saved'))
        } catch {
            showMessage('error', t('settings.save_ip_failed'))
        }
    }

    const handleExport = async () => {
        setActionLoading('export')
        try {
            const res = await configAPI.export()
            const blob = new Blob([JSON.stringify(res.data, null, 2)], {
                type: 'application/json',
            })
            const url = URL.createObjectURL(blob)
            const link = document.createElement('a')
            link.href = url
            link.download = `caddypanel-export-${new Date().toISOString().slice(0, 10)}.json`
            link.click()
            URL.revokeObjectURL(url)
            showMessage('success', t('settings.export_success'))
        } catch (err) {
            showMessage('error', t('settings.export_failed'))
        } finally {
            setActionLoading(null)
        }
    }

    const handleImport = async (e) => {
        const file = e.target.files?.[0]
        if (!file) return

        setActionLoading('import')
        try {
            const text = await file.text()
            const data = JSON.parse(text)
            const res = await configAPI.import(data)
            showMessage('success', res.data.message)
            await fetchStatus()
            await fetchCaddyfile()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('settings.import_invalid'))
        } finally {
            setActionLoading(null)
            e.target.value = '' // reset file input
        }
    }

    const running = caddyStatus?.running

    return (
        <Box>
            <Heading size="6" mb="1" style={{ color: 'var(--cp-text)' }}>{t('settings.title')}</Heading>
            <Text size="2" color="gray" mb="5" as="p">
                {t('settings.subtitle')}
            </Text>

            {/* Status message */}
            {message && (
                <Callout.Root
                    color={message.type === 'success' ? 'green' : 'red'}
                    size="1"
                    mb="4"
                >
                    <Callout.Icon>
                        {message.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Tabs.Root defaultValue="caddy">
                <Tabs.List>
                    <Tabs.Trigger value="caddy">
                        <Server size={14} style={{ marginRight: 6 }} /> {t('settings.caddy_server')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="caddyfile">
                        <FileCode size={14} style={{ marginRight: 6 }} /> {t('settings.caddyfile')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="backup">
                        <Download size={14} style={{ marginRight: 6 }} /> {t('settings.backup')}
                    </Tabs.Trigger>
                </Tabs.List>

                {/* ---- Caddy Server Tab ---- */}
                <Tabs.Content value="caddy">
                    <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                        <Heading size="3" mb="4">{t('settings.process_control')}</Heading>

                        <Flex align="center" gap="3" mb="4">
                            <Text size="2" color="gray">{t('common.status')}:</Text>
                            <Badge
                                color={running ? 'green' : 'red'}
                                variant="solid"
                                size="2"
                            >
                                {running ? `● ${t('dashboard.running')}` : `○ ${t('dashboard.stopped')}`}
                            </Badge>
                            {caddyStatus?.version && (
                                <Badge variant="soft" size="1">{caddyStatus.version}</Badge>
                            )}
                        </Flex>

                        <Flex gap="2" wrap="wrap">
                            <Button
                                color="green"
                                disabled={running || actionLoading === 'start'}
                                onClick={() => handleCaddyAction('start')}
                            >
                                <Play size={14} />
                                {actionLoading === 'start' ? t('settings.starting') : t('settings.start')}
                            </Button>
                            <Button
                                color="red"
                                variant="soft"
                                disabled={!running || actionLoading === 'stop'}
                                onClick={() => handleCaddyAction('stop')}
                            >
                                <Square size={14} />
                                {actionLoading === 'stop' ? t('settings.stopping') : t('settings.stop')}
                            </Button>
                            <Button
                                variant="soft"
                                disabled={!running || actionLoading === 'reload'}
                                onClick={() => handleCaddyAction('reload')}
                            >
                                <RefreshCw size={14} />
                                {actionLoading === 'reload' ? t('settings.reloading') : t('settings.reload')}
                            </Button>
                        </Flex>
                    </Card>

                    <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                        <Heading size="3" mb="3">{t('settings.auto_management')}</Heading>

                        <Flex justify="between" align="center">
                            <Flex direction="column" style={{ flex: 1 }}>
                                <Text size="2" weight="medium">{t('settings.auto_reload')}</Text>
                                <Text size="1" color="gray">
                                    {t('settings.auto_reload_hint')}
                                </Text>
                            </Flex>
                            <Switch
                                checked={autoReload}
                                onCheckedChange={handleToggleAutoReload}
                            />
                        </Flex>

                        <Callout.Root color="blue" size="1" mt="3">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>
                                {t('settings.auto_reload_callout')}
                            </Callout.Text>
                        </Callout.Root>
                    </Card>

                    <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                        <Heading size="3" mb="3">{t('settings.server_ip')}</Heading>
                        <Text size="1" color="gray" mb="3" as="p">
                            {t('settings.server_ip_hint')}
                        </Text>

                        <Flex direction="column" gap="2">
                            <Flex align="center" gap="2">
                                <Text size="2" style={{ width: 50 }}>IPv4</Text>
                                <TextField.Root
                                    placeholder={t('common.not_detected')}
                                    value={serverIpv4}
                                    onChange={(e) => setServerIpv4(e.target.value)}
                                    size="2"
                                    style={{ flex: 1 }}
                                />
                            </Flex>
                            <Flex align="center" gap="2">
                                <Text size="2" style={{ width: 50 }}>IPv6</Text>
                                <TextField.Root
                                    placeholder={t('common.not_detected')}
                                    value={serverIpv6}
                                    onChange={(e) => setServerIpv6(e.target.value)}
                                    size="2"
                                    style={{ flex: 1 }}
                                />
                            </Flex>
                            <Flex justify="end">
                                <Button size="1" variant="soft" onClick={handleSaveIPs}>{t('settings.save_ip')}</Button>
                            </Flex>
                        </Flex>
                    </Card>
                </Tabs.Content>

                {/* ---- Caddyfile Tab ---- */}
                <Tabs.Content value="caddyfile">
                    <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                        <Flex justify="between" align="center" mb="3">
                            <Heading size="3">{t('settings.generated_caddyfile')}</Heading>
                            <Button variant="ghost" size="1" onClick={fetchCaddyfile}>
                                <RefreshCw size={12} /> {t('common.refresh')}
                            </Button>
                        </Flex>

                        <Box
                            style={{
                                background: 'var(--cp-code-bg)',
                                border: '1px solid var(--cp-border)',
                                borderRadius: 8,
                                padding: 16,
                                maxHeight: 500,
                                overflow: 'auto',
                            }}
                        >
                            <pre className="log-viewer" style={{ margin: 0, color: 'var(--cp-text)' }}>
                                {caddyfile || t('settings.no_caddyfile_hint')}
                            </pre>
                        </Box>
                    </Card>
                </Tabs.Content>

                {/* ---- Backup Tab ---- */}
                <Tabs.Content value="backup">
                    <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                        <Heading size="3" mb="4">{t('settings.import_export')}</Heading>

                        <Text size="2" color="gray" mb="4" as="p">
                            {t('settings.import_export_hint')}
                        </Text>

                        <Flex gap="3" wrap="wrap">
                            <Button
                                onClick={handleExport}
                                disabled={actionLoading === 'export'}
                            >
                                <Download size={14} />
                                {actionLoading === 'export' ? t('settings.exporting') : t('settings.export_config')}
                            </Button>

                            <Button
                                variant="soft"
                                color="gray"
                                onClick={() => fileInputRef.current?.click()}
                                disabled={actionLoading === 'import'}
                            >
                                <Upload size={14} />
                                {actionLoading === 'import' ? t('settings.importing') : t('settings.import_config')}
                            </Button>

                            <input
                                ref={fileInputRef}
                                type="file"
                                accept=".json"
                                onChange={handleImport}
                                style={{ display: 'none' }}
                            />
                        </Flex>

                        <Callout.Root color="orange" size="1" mt="4">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>
                                {t('settings.import_warning')}
                            </Callout.Text>
                        </Callout.Root>
                    </Card>
                </Tabs.Content>
            </Tabs.Root>
        </Box>
    )
}
