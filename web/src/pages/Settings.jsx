import { useState, useEffect, useRef, useCallback } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Badge, Callout, Separator, Code,
    Tabs, Switch, TextField, IconButton, Dialog, AlertDialog, Spinner,
} from '@radix-ui/themes'
import {
    Play, Square, RefreshCw, Download, Upload, Server, FileCode,
    AlertCircle, CheckCircle2, ShieldCheck, Copy, FolderOpen, Tags,
    Plus, Pencil, Trash2, Power, PowerOff, X,
} from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { caddyAPI, configAPI, settingAPI, authAPI, groupAPI, tagAPI } from '../api/index.js'
import { useAuthStore } from '../stores/auth.js'
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
    const [isMobile, setIsMobile] = useState(() =>
        typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches
    )

    // 2FA state
    const { user, fetchMe } = useAuthStore()
    const [twofaStep, setTwofaStep] = useState('idle') // idle | qr | recovery | disable
    const [twofaUri, setTwofaUri] = useState('')
    const [twofaCode, setTwofaCode] = useState('')
    const [recoveryCodes, setRecoveryCodes] = useState([])
    const [twofaLoading, setTwofaLoading] = useState(false)

    // Groups & Tags state
    const [groups, setGroups] = useState([])
    const [allTags, setAllTags] = useState([])
    const [groupForm, setGroupForm] = useState({ name: '', color: 'gray' })
    const [tagForm, setTagForm] = useState({ name: '', color: 'gray' })
    const [editingGroup, setEditingGroup] = useState(null)
    const [editingTag, setEditingTag] = useState(null)
    const [groupTagLoading, setGroupTagLoading] = useState(false)

    const PRESET_COLORS = ['red', 'orange', 'yellow', 'green', 'blue', 'purple', 'pink', 'gray']

    useEffect(() => {
        const mql = window.matchMedia('(max-width: 767px)')
        const handler = (e) => setIsMobile(e.matches)
        mql.addEventListener('change', handler)
        return () => mql.removeEventListener('change', handler)
    }, [])

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

    const handleSetup2FA = async () => {
        setTwofaLoading(true)
        try {
            const res = await authAPI.setup2FA()
            setTwofaUri(res.data.uri)
            setTwofaStep('qr')
            setTwofaCode('')
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('twofa.setup_failed'))
        } finally {
            setTwofaLoading(false)
        }
    }

    const handleVerify2FA = async () => {
        setTwofaLoading(true)
        try {
            const res = await authAPI.verify2FA(twofaCode)
            setRecoveryCodes(res.data.recovery_codes || [])
            setTwofaStep('recovery')
            setTwofaCode('')
            showMessage('success', t('twofa.setup_success'))
            await fetchMe()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('twofa.setup_failed'))
            setTwofaCode('')
        } finally {
            setTwofaLoading(false)
        }
    }

    const handleDisable2FA = async () => {
        setTwofaLoading(true)
        try {
            await authAPI.disable2FA(twofaCode)
            setTwofaStep('idle')
            setTwofaCode('')
            showMessage('success', t('twofa.disable_success'))
            await fetchMe()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('twofa.disable_failed'))
            setTwofaCode('')
        } finally {
            setTwofaLoading(false)
        }
    }

    const handleCopyRecoveryCodes = () => {
        navigator.clipboard.writeText(recoveryCodes.join('\n'))
        showMessage('success', t('common.copied'))
    }

    // ---- Groups & Tags handlers ----
    const fetchGroupsAndTags = async () => {
        try {
            const [gRes, tRes] = await Promise.all([groupAPI.list(), tagAPI.list()])
            setGroups(gRes.data.groups || [])
            setAllTags(tRes.data.tags || [])
        } catch { /* ignore */ }
    }

    useEffect(() => { fetchGroupsAndTags() }, [])

    const handleSaveGroup = async () => {
        setGroupTagLoading(true)
        try {
            if (editingGroup) {
                await groupAPI.update(editingGroup.id, groupForm)
            } else {
                await groupAPI.create(groupForm)
            }
            setGroupForm({ name: '', color: 'gray' })
            setEditingGroup(null)
            showMessage('success', editingGroup ? t('common.update_success') : t('common.create_success'))
            await fetchGroupsAndTags()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.operation_failed'))
        } finally {
            setGroupTagLoading(false)
        }
    }

    const handleDeleteGroup = async (group) => {
        setGroupTagLoading(true)
        try {
            await groupAPI.delete(group.id)
            showMessage('success', t('common.delete') + ' ✓')
            await fetchGroupsAndTags()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.delete_failed'))
        } finally {
            setGroupTagLoading(false)
        }
    }

    const handleBatchEnable = async (group) => {
        try {
            await groupAPI.batchEnable(group.id)
            showMessage('success', t('group.batch_enable_success'))
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.operation_failed'))
        }
    }

    const handleBatchDisable = async (group) => {
        try {
            await groupAPI.batchDisable(group.id)
            showMessage('success', t('group.batch_disable_success'))
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.operation_failed'))
        }
    }

    const handleSaveTag = async () => {
        setGroupTagLoading(true)
        try {
            if (editingTag) {
                await tagAPI.update(editingTag.id, tagForm)
            } else {
                await tagAPI.create(tagForm)
            }
            setTagForm({ name: '', color: 'gray' })
            setEditingTag(null)
            showMessage('success', editingTag ? t('common.update_success') : t('common.create_success'))
            await fetchGroupsAndTags()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.operation_failed'))
        } finally {
            setGroupTagLoading(false)
        }
    }

    const handleDeleteTag = async (tag) => {
        setGroupTagLoading(true)
        try {
            await tagAPI.delete(tag.id)
            showMessage('success', t('common.delete') + ' ✓')
            await fetchGroupsAndTags()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.delete_failed'))
        } finally {
            setGroupTagLoading(false)
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
            link.download = `webcasa-export-${new Date().toISOString().slice(0, 10)}.json`
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
                    <Tabs.Trigger value="security">
                        <ShieldCheck size={14} style={{ marginRight: 6 }} /> {t('twofa.setup_title')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="groups-tags">
                        <Tags size={14} style={{ marginRight: 6 }} /> {t('group.manage')}
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

                        <Flex gap="2" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                            <Button
                                color="green"
                                disabled={running || actionLoading === 'start'}
                                onClick={() => handleCaddyAction('start')}
                                style={isMobile ? { width: '100%' } : {}}
                            >
                                <Play size={14} />
                                {actionLoading === 'start' ? t('settings.starting') : t('settings.start')}
                            </Button>
                            <Button
                                color="red"
                                variant="soft"
                                disabled={!running || actionLoading === 'stop'}
                                onClick={() => handleCaddyAction('stop')}
                                style={isMobile ? { width: '100%' } : {}}
                            >
                                <Square size={14} />
                                {actionLoading === 'stop' ? t('settings.stopping') : t('settings.stop')}
                            </Button>
                            <Button
                                variant="soft"
                                disabled={!running || actionLoading === 'reload'}
                                onClick={() => handleCaddyAction('reload')}
                                style={isMobile ? { width: '100%' } : {}}
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
                            <Flex align={isMobile ? 'stretch' : 'center'} gap="2" direction={isMobile ? 'column' : 'row'}>
                                <Text size="2" style={isMobile ? {} : { width: 50 }}>IPv4</Text>
                                <TextField.Root
                                    placeholder={t('common.not_detected')}
                                    value={serverIpv4}
                                    onChange={(e) => setServerIpv4(e.target.value)}
                                    size="2"
                                    style={{ flex: 1 }}
                                />
                            </Flex>
                            <Flex align={isMobile ? 'stretch' : 'center'} gap="2" direction={isMobile ? 'column' : 'row'}>
                                <Text size="2" style={isMobile ? {} : { width: 50 }}>IPv6</Text>
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

                        <Flex gap="3" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                            <Button
                                onClick={handleExport}
                                disabled={actionLoading === 'export'}
                                style={isMobile ? { width: '100%' } : {}}
                            >
                                <Download size={14} />
                                {actionLoading === 'export' ? t('settings.exporting') : t('settings.export_config')}
                            </Button>

                            <Button
                                variant="soft"
                                color="gray"
                                onClick={() => fileInputRef.current?.click()}
                                disabled={actionLoading === 'import'}
                                style={isMobile ? { width: '100%' } : {}}
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

                {/* ---- Security (2FA) Tab ---- */}
                <Tabs.Content value="security">
                    <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                        <Flex justify="between" align="center" mb="4">
                            <Flex direction="column" gap="1">
                                <Heading size="3">{t('twofa.setup_title')}</Heading>
                                <Text size="2" color="gray">{t('twofa.setup_description')}</Text>
                            </Flex>
                            <Badge
                                color={user?.totp_enabled ? 'green' : 'gray'}
                                variant="soft"
                                size="2"
                            >
                                {user?.totp_enabled ? t('twofa.enabled_badge') : t('twofa.disabled_badge')}
                            </Badge>
                        </Flex>

                        <Separator size="4" mb="4" />

                        {twofaStep === 'idle' && !user?.totp_enabled && (
                            <Button onClick={handleSetup2FA} disabled={twofaLoading}>
                                <ShieldCheck size={14} />
                                {twofaLoading ? t('common.loading') : t('twofa.enable')}
                            </Button>
                        )}

                        {twofaStep === 'idle' && user?.totp_enabled && (
                            <Flex direction="column" gap="3">
                                <Text size="2" color="gray">{t('twofa.disable_confirm')}</Text>
                                <Flex gap="2" align="end" direction={isMobile ? 'column' : 'row'}>
                                    <TextField.Root
                                        placeholder={t('twofa.code_placeholder')}
                                        value={twofaCode}
                                        onChange={(e) => setTwofaCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                                        size="2"
                                        maxLength={6}
                                        style={isMobile ? { width: '100%' } : { width: 200 }}
                                    />
                                    <Button
                                        color="red"
                                        variant="soft"
                                        onClick={handleDisable2FA}
                                        disabled={twofaLoading || twofaCode.length < 6}
                                        style={isMobile ? { width: '100%' } : {}}
                                    >
                                        {twofaLoading ? t('twofa.disabling') : t('twofa.confirm_disable')}
                                    </Button>
                                </Flex>
                            </Flex>
                        )}

                        {twofaStep === 'qr' && (
                            <Flex direction="column" gap="4" align="center">
                                <Text size="2" weight="medium">{t('twofa.scan_qr')}</Text>
                                <Box style={{
                                    padding: 16,
                                    background: 'white',
                                    borderRadius: 12,
                                    display: 'inline-block',
                                }}>
                                    <QRCodeSVG value={twofaUri} size={200} />
                                </Box>
                                <Text size="2" color="gray">{t('twofa.enter_code')}</Text>
                                <Flex gap="2" align="end" direction={isMobile ? 'column' : 'row'} style={{ width: '100%', maxWidth: 360 }}>
                                    <TextField.Root
                                        placeholder={t('twofa.code_placeholder')}
                                        value={twofaCode}
                                        onChange={(e) => setTwofaCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                                        size="2"
                                        maxLength={6}
                                        autoFocus
                                        style={{ flex: 1, textAlign: 'center', letterSpacing: '0.2em', fontFamily: 'monospace' }}
                                    />
                                    <Button
                                        onClick={handleVerify2FA}
                                        disabled={twofaLoading || twofaCode.length < 6}
                                        style={isMobile ? { width: '100%' } : {}}
                                    >
                                        {twofaLoading ? t('twofa.confirming') : t('twofa.confirm_enable')}
                                    </Button>
                                </Flex>
                                <Text
                                    size="1"
                                    color="gray"
                                    style={{ cursor: 'pointer', textDecoration: 'underline' }}
                                    onClick={() => { setTwofaStep('idle'); setTwofaCode(''); setTwofaUri('') }}
                                >
                                    {t('common.cancel')}
                                </Text>
                            </Flex>
                        )}

                        {twofaStep === 'recovery' && (
                            <Flex direction="column" gap="3">
                                <Heading size="3">{t('twofa.recovery_codes_title')}</Heading>
                                <Text size="2" color="gray">{t('twofa.recovery_codes_hint')}</Text>

                                <Callout.Root color="orange" size="1">
                                    <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                                    <Callout.Text>{t('twofa.recovery_codes_warning')}</Callout.Text>
                                </Callout.Root>

                                <Box style={{
                                    background: 'var(--cp-code-bg)',
                                    border: '1px solid var(--cp-border)',
                                    borderRadius: 8,
                                    padding: 16,
                                }}>
                                    <Flex wrap="wrap" gap="2">
                                        {recoveryCodes.map((code, i) => (
                                            <Code key={i} size="3" style={{ fontFamily: 'monospace' }}>{code}</Code>
                                        ))}
                                    </Flex>
                                </Box>

                                <Flex gap="2" direction={isMobile ? 'column' : 'row'}>
                                    <Button variant="soft" onClick={handleCopyRecoveryCodes} style={isMobile ? { width: '100%' } : {}}>
                                        <Copy size={14} />
                                        {t('twofa.copy_codes')}
                                    </Button>
                                    <Button onClick={() => { setTwofaStep('idle'); setRecoveryCodes([]) }} style={isMobile ? { width: '100%' } : {}}>
                                        {t('twofa.done')}
                                    </Button>
                                </Flex>
                            </Flex>
                        )}
                    </Card>
                </Tabs.Content>

                {/* ---- Groups & Tags Tab ---- */}
                <Tabs.Content value="groups-tags">
                    {/* Groups Section */}
                    <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                        <Flex justify="between" align="center" mb="4">
                            <Heading size="3"><FolderOpen size={16} style={{ marginRight: 6, verticalAlign: 'middle' }} />{t('group.title')}</Heading>
                        </Flex>

                        {/* Group create/edit form */}
                        <Flex gap="2" mb="4" align="end" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                            <Flex direction="column" gap="1" style={{ flex: 1, minWidth: 120 }}>
                                <Text size="1" color="gray">{t('group.name')}</Text>
                                <TextField.Root
                                    placeholder={t('group.name_placeholder')}
                                    value={groupForm.name}
                                    onChange={(e) => setGroupForm({ ...groupForm, name: e.target.value })}
                                    size="2"
                                />
                            </Flex>
                            <Flex direction="column" gap="1">
                                <Text size="1" color="gray">{t('group.color')}</Text>
                                <Flex gap="1">
                                    {PRESET_COLORS.map(c => (
                                        <Box
                                            key={c}
                                            onClick={() => setGroupForm({ ...groupForm, color: c })}
                                            style={{
                                                width: 24, height: 24, borderRadius: '50%',
                                                cursor: 'pointer',
                                                outline: groupForm.color === c ? '2px solid var(--cp-text)' : 'none',
                                                outlineOffset: 2,
                                            }}
                                        >
                                            <Badge color={c} variant="solid" size="1" style={{ width: 24, height: 24, borderRadius: '50%', display: 'block' }}>&nbsp;</Badge>
                                        </Box>
                                    ))}
                                </Flex>
                            </Flex>
                            <Flex gap="2">
                                <Button
                                    size="2"
                                    onClick={handleSaveGroup}
                                    disabled={groupTagLoading || !groupForm.name.trim()}
                                >
                                    {editingGroup ? t('common.save') : <><Plus size={14} /> {t('group.create')}</>}
                                </Button>
                                {editingGroup && (
                                    <Button size="2" variant="soft" color="gray" onClick={() => { setEditingGroup(null); setGroupForm({ name: '', color: 'gray' }) }}>
                                        {t('common.cancel')}
                                    </Button>
                                )}
                            </Flex>
                        </Flex>

                        {/* Group list */}
                        {groups.length === 0 ? (
                            <Text size="2" color="gray">{t('group.no_groups')}</Text>
                        ) : (
                            <Flex direction="column" gap="2">
                                {groups.map(group => (
                                    <Card key={group.id} style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                                        <Flex justify="between" align="center" wrap="wrap" gap="2">
                                            <Flex align="center" gap="2">
                                                <Badge color={group.color || 'gray'} variant="solid" size="2">
                                                    <FolderOpen size={12} /> {group.name}
                                                </Badge>
                                            </Flex>
                                            <Flex gap="2" wrap="wrap">
                                                <Button size="1" variant="soft" color="green" onClick={() => handleBatchEnable(group)}>
                                                    <Power size={12} /> {t('group.batch_enable')}
                                                </Button>
                                                <Button size="1" variant="soft" color="orange" onClick={() => handleBatchDisable(group)}>
                                                    <PowerOff size={12} /> {t('group.batch_disable')}
                                                </Button>
                                                <IconButton size="1" variant="ghost" onClick={() => { setEditingGroup(group); setGroupForm({ name: group.name, color: group.color || 'gray' }) }}>
                                                    <Pencil size={14} />
                                                </IconButton>
                                                <IconButton size="1" variant="ghost" color="red" onClick={() => handleDeleteGroup(group)}>
                                                    <Trash2 size={14} />
                                                </IconButton>
                                            </Flex>
                                        </Flex>
                                    </Card>
                                ))}
                            </Flex>
                        )}
                    </Card>

                    {/* Tags Section */}
                    <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                        <Flex justify="between" align="center" mb="4">
                            <Heading size="3"><Tags size={16} style={{ marginRight: 6, verticalAlign: 'middle' }} />{t('tag.title')}</Heading>
                        </Flex>

                        {/* Tag create/edit form */}
                        <Flex gap="2" mb="4" align="end" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                            <Flex direction="column" gap="1" style={{ flex: 1, minWidth: 120 }}>
                                <Text size="1" color="gray">{t('tag.name')}</Text>
                                <TextField.Root
                                    placeholder={t('tag.name_placeholder')}
                                    value={tagForm.name}
                                    onChange={(e) => setTagForm({ ...tagForm, name: e.target.value })}
                                    size="2"
                                />
                            </Flex>
                            <Flex direction="column" gap="1">
                                <Text size="1" color="gray">{t('tag.color')}</Text>
                                <Flex gap="1">
                                    {PRESET_COLORS.map(c => (
                                        <Box
                                            key={c}
                                            onClick={() => setTagForm({ ...tagForm, color: c })}
                                            style={{
                                                width: 24, height: 24, borderRadius: '50%',
                                                cursor: 'pointer',
                                                outline: tagForm.color === c ? '2px solid var(--cp-text)' : 'none',
                                                outlineOffset: 2,
                                            }}
                                        >
                                            <Badge color={c} variant="solid" size="1" style={{ width: 24, height: 24, borderRadius: '50%', display: 'block' }}>&nbsp;</Badge>
                                        </Box>
                                    ))}
                                </Flex>
                            </Flex>
                            <Flex gap="2">
                                <Button
                                    size="2"
                                    onClick={handleSaveTag}
                                    disabled={groupTagLoading || !tagForm.name.trim()}
                                >
                                    {editingTag ? t('common.save') : <><Plus size={14} /> {t('tag.create')}</>}
                                </Button>
                                {editingTag && (
                                    <Button size="2" variant="soft" color="gray" onClick={() => { setEditingTag(null); setTagForm({ name: '', color: 'gray' }) }}>
                                        {t('common.cancel')}
                                    </Button>
                                )}
                            </Flex>
                        </Flex>

                        {/* Tag list */}
                        {allTags.length === 0 ? (
                            <Text size="2" color="gray">{t('tag.no_tags')}</Text>
                        ) : (
                            <Flex gap="2" wrap="wrap">
                                {allTags.map(tag => (
                                    <Card key={tag.id} style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)', padding: '8px 12px' }}>
                                        <Flex align="center" gap="2">
                                            <Badge color={tag.color || 'gray'} variant="solid" size="2">
                                                {tag.name}
                                            </Badge>
                                            <IconButton size="1" variant="ghost" onClick={() => { setEditingTag(tag); setTagForm({ name: tag.name, color: tag.color || 'gray' }) }}>
                                                <Pencil size={12} />
                                            </IconButton>
                                            <IconButton size="1" variant="ghost" color="red" onClick={() => handleDeleteTag(tag)}>
                                                <Trash2 size={12} />
                                            </IconButton>
                                        </Flex>
                                    </Card>
                                ))}
                            </Flex>
                        )}
                    </Card>
                </Tabs.Content>
            </Tabs.Root>
        </Box>
    )
}
