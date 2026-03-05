import { useState, useEffect, useRef, useCallback } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Badge, Callout, Separator, Code,
    Tabs, Switch, TextField, IconButton, Dialog, AlertDialog, Spinner,
    Select, Table, Tooltip,
} from '@radix-ui/themes'
import {
    Play, Square, RefreshCw, Download, Upload, Server, FileCode,
    AlertCircle, CheckCircle2, ShieldCheck, Copy, FolderOpen, Tags,
    Plus, Pencil, Trash2, Power, PowerOff, X, Shield, Eye,
    ChevronLeft, ChevronRight, ClipboardList, FileText, Search,
    Bot, Save, TestTube, Check, Package, Star, Cpu, Key,
    Activity, HardDrive,
} from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import {
    caddyAPI, configAPI, settingAPI, authAPI, groupAPI, tagAPI,
    userAPI, logAPI, auditAPI, pluginAPI, aiAPI, dnsProviderAPI, certificateAPI, mcpAPI,
    deployAPI, backupAPI, notifyAPI,
} from '../api/index.js'
import { useAuthStore } from '../stores/auth.js'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router'
import MonitoringDashboard from './MonitoringDashboard.jsx'
import BackupManager from './BackupManager.jsx'

// ============================================================
//  Settings — Unified settings page with tabs:
//  General | Users | Logs | AI | DNS | Certificates | Plugins
// ============================================================

const VALID_TABS = ['general', 'users', 'logs', 'ai', 'dns', 'certificates', 'plugins', 'tokens', 'monitoring', 'backup', 'notify']

export default function Settings() {
    const { t } = useTranslation()
    const [searchParams] = useSearchParams()
    const initialTab = VALID_TABS.includes(searchParams.get('tab')) ? searchParams.get('tab') : 'general'
    const [message, setMessage] = useState(null)

    const showMessage = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 5000)
    }

    return (
        <Box>
            <Heading size="6" mb="1" style={{ color: 'var(--cp-text)' }}>{t('settings.title')}</Heading>
            <Text size="2" color="gray" mb="5" as="p">
                {t('settings.subtitle')}
            </Text>

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

            <Tabs.Root defaultValue={initialTab}>
                <Tabs.List style={{ flexWrap: 'wrap' }}>
                    <Tabs.Trigger value="general">
                        <Server size={14} style={{ marginRight: 6 }} /> {t('settings.tab_general')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="users">
                        <Eye size={14} style={{ marginRight: 6 }} /> {t('settings.tab_users')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="logs">
                        <FileText size={14} style={{ marginRight: 6 }} /> {t('settings.tab_logs')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="ai">
                        <Bot size={14} style={{ marginRight: 6 }} /> {t('settings.tab_ai')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="dns">
                        <Shield size={14} style={{ marginRight: 6 }} /> {t('settings.tab_dns')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="certificates">
                        <ShieldCheck size={14} style={{ marginRight: 6 }} /> {t('settings.tab_certificates')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="plugins">
                        <Package size={14} style={{ marginRight: 6 }} /> {t('settings.tab_plugins')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="tokens">
                        <Key size={14} style={{ marginRight: 6 }} /> {t('mcp.title')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="monitoring">
                        <Activity size={14} style={{ marginRight: 6 }} /> {t('nav.monitoring')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="backup">
                        <HardDrive size={14} style={{ marginRight: 6 }} /> {t('nav.backup')}
                    </Tabs.Trigger>
                    <Tabs.Trigger value="notify">
                        <Activity size={14} style={{ marginRight: 6 }} /> {t('notify.title')}
                    </Tabs.Trigger>
                </Tabs.List>

                <Tabs.Content value="general">
                    <GeneralTab showMessage={showMessage} />
                </Tabs.Content>
                <Tabs.Content value="users">
                    <UsersTab showMessage={showMessage} />
                </Tabs.Content>
                <Tabs.Content value="logs">
                    <LogsTab />
                </Tabs.Content>
                <Tabs.Content value="ai">
                    <AITab showMessage={showMessage} />
                </Tabs.Content>
                <Tabs.Content value="dns">
                    <DnsTab />
                </Tabs.Content>
                <Tabs.Content value="certificates">
                    <CertificatesTab />
                </Tabs.Content>
                <Tabs.Content value="plugins">
                    <PluginsTab />
                </Tabs.Content>
                <Tabs.Content value="tokens">
                    <APITokensTab />
                </Tabs.Content>
                <Tabs.Content value="monitoring">
                    <MonitoringDashboard embedded />
                </Tabs.Content>
                <Tabs.Content value="backup">
                    <BackupManager embedded />
                </Tabs.Content>
                <Tabs.Content value="notify">
                    <NotifyTab showMessage={showMessage} />
                </Tabs.Content>
            </Tabs.Root>
        </Box>
    )
}


// ======================== General Tab ========================
function GeneralTab({ showMessage }) {
    const { t } = useTranslation()
    const [caddyStatus, setCaddyStatus] = useState(null)
    const [caddyfile, setCaddyfile] = useState('')
    const [actionLoading, setActionLoading] = useState(null)
    const fileInputRef = useRef(null)
    const [autoReload, setAutoReload] = useState(true)
    const [serverIpv4, setServerIpv4] = useState('')
    const [serverIpv6, setServerIpv6] = useState('')
    const [isMobile, setIsMobile] = useState(() =>
        typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches
    )

    // 2FA state
    const { user, fetchMe } = useAuthStore()
    const [twofaStep, setTwofaStep] = useState('idle')
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

    const handleCaddyAction = async (action) => {
        setActionLoading(action)
        try {
            let res
            switch (action) {
                case 'start': res = await caddyAPI.start(); break
                case 'stop': res = await caddyAPI.stop(); break
                case 'reload': res = await caddyAPI.reload(); break
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
            setAutoReload(!value)
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
            setTwofaUri(res.data.otpauth_uri)
            setTwofaStep('qr')
            setTwofaCode('')
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('twofa.setup_failed'))
        } finally { setTwofaLoading(false) }
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
        } finally { setTwofaLoading(false) }
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
        } finally { setTwofaLoading(false) }
    }

    const handleCopyRecoveryCodes = () => {
        navigator.clipboard.writeText(recoveryCodes.join('\n'))
        showMessage('success', t('common.copied'))
    }

    // Groups & Tags handlers
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
        } finally { setGroupTagLoading(false) }
    }

    const handleDeleteGroup = async (group) => {
        setGroupTagLoading(true)
        try {
            await groupAPI.delete(group.id)
            showMessage('success', t('common.delete') + ' OK')
            await fetchGroupsAndTags()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.delete_failed'))
        } finally { setGroupTagLoading(false) }
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
        } finally { setGroupTagLoading(false) }
    }

    const handleDeleteTag = async (tag) => {
        setGroupTagLoading(true)
        try {
            await tagAPI.delete(tag.id)
            showMessage('success', t('common.delete') + ' OK')
            await fetchGroupsAndTags()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.delete_failed'))
        } finally { setGroupTagLoading(false) }
    }

    const handleExport = async () => {
        setActionLoading('export')
        try {
            const res = await configAPI.export()
            const blob = new Blob([JSON.stringify(res.data, null, 2)], { type: 'application/json' })
            const url = URL.createObjectURL(blob)
            const link = document.createElement('a')
            link.href = url
            link.download = `webcasa-export-${new Date().toISOString().slice(0, 10)}.json`
            link.click()
            URL.revokeObjectURL(url)
            showMessage('success', t('settings.export_success'))
        } catch {
            showMessage('error', t('settings.export_failed'))
        } finally { setActionLoading(null) }
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
            e.target.value = ''
        }
    }

    const running = caddyStatus?.running

    return (
        <Tabs.Root defaultValue="caddy">
            <Tabs.List>
                <Tabs.Trigger value="caddy">
                    <Server size={14} style={{ marginRight: 6 }} /> {t('settings.caddy_server')}
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

            {/* Caddy Server */}
            <Tabs.Content value="caddy">
                <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Heading size="3" mb="4">{t('settings.process_control')}</Heading>
                    <Flex align="center" gap="3" mb="4">
                        <Text size="2" color="gray">{t('common.status')}:</Text>
                        <Badge color={running ? 'green' : 'red'} variant="solid" size="2">
                            {running ? `● ${t('dashboard.running')}` : `○ ${t('dashboard.stopped')}`}
                        </Badge>
                        {caddyStatus?.version && <Badge variant="soft" size="1">{caddyStatus.version}</Badge>}
                    </Flex>
                    <Flex gap="2" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                        <Button color="green" disabled={running || actionLoading === 'start'} onClick={() => handleCaddyAction('start')} style={isMobile ? { width: '100%' } : {}}>
                            <Play size={14} /> {actionLoading === 'start' ? t('settings.starting') : t('settings.start')}
                        </Button>
                        <Button color="red" variant="soft" disabled={!running || actionLoading === 'stop'} onClick={() => handleCaddyAction('stop')} style={isMobile ? { width: '100%' } : {}}>
                            <Square size={14} /> {actionLoading === 'stop' ? t('settings.stopping') : t('settings.stop')}
                        </Button>
                        <Button variant="soft" disabled={!running || actionLoading === 'reload'} onClick={() => handleCaddyAction('reload')} style={isMobile ? { width: '100%' } : {}}>
                            <RefreshCw size={14} /> {actionLoading === 'reload' ? t('settings.reloading') : t('settings.reload')}
                        </Button>
                    </Flex>
                </Card>

                <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Heading size="3" mb="3">{t('settings.auto_management')}</Heading>
                    <Flex justify="between" align="center">
                        <Flex direction="column" style={{ flex: 1 }}>
                            <Text size="2" weight="medium">{t('settings.auto_reload')}</Text>
                            <Text size="1" color="gray">{t('settings.auto_reload_hint')}</Text>
                        </Flex>
                        <Switch checked={autoReload} onCheckedChange={handleToggleAutoReload} />
                    </Flex>
                    <Callout.Root color="blue" size="1" mt="3">
                        <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                        <Callout.Text>{t('settings.auto_reload_callout')}</Callout.Text>
                    </Callout.Root>
                </Card>

                <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Heading size="3" mb="3">{t('settings.server_ip')}</Heading>
                    <Text size="1" color="gray" mb="3" as="p">{t('settings.server_ip_hint')}</Text>
                    <Flex direction="column" gap="2">
                        <Flex align={isMobile ? 'stretch' : 'center'} gap="2" direction={isMobile ? 'column' : 'row'}>
                            <Text size="2" style={isMobile ? {} : { width: 50 }}>IPv4</Text>
                            <TextField.Root placeholder={t('common.not_detected')} value={serverIpv4} onChange={(e) => setServerIpv4(e.target.value)} size="2" style={{ flex: 1 }} />
                        </Flex>
                        <Flex align={isMobile ? 'stretch' : 'center'} gap="2" direction={isMobile ? 'column' : 'row'}>
                            <Text size="2" style={isMobile ? {} : { width: 50 }}>IPv6</Text>
                            <TextField.Root placeholder={t('common.not_detected')} value={serverIpv6} onChange={(e) => setServerIpv6(e.target.value)} size="2" style={{ flex: 1 }} />
                        </Flex>
                        <Flex justify="end">
                            <Button size="1" variant="soft" onClick={handleSaveIPs}>{t('settings.save_ip')}</Button>
                        </Flex>
                    </Flex>
                </Card>

                <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex justify="between" align="center" mb="3">
                        <Heading size="3">{t('settings.generated_caddyfile')}</Heading>
                        <Button variant="ghost" size="1" onClick={fetchCaddyfile}><RefreshCw size={12} /> {t('common.refresh')}</Button>
                    </Flex>
                    <Box style={{ background: 'var(--cp-code-bg)', border: '1px solid var(--cp-border)', borderRadius: 8, padding: 16, maxHeight: 300, overflow: 'auto' }}>
                        <pre className="log-viewer" style={{ margin: 0, color: 'var(--cp-text)' }}>
                            {caddyfile || t('settings.no_caddyfile_hint')}
                        </pre>
                    </Box>
                </Card>
            </Tabs.Content>

            {/* Backup/Import/Export */}
            <Tabs.Content value="backup">
                <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Heading size="3" mb="4">{t('settings.import_export')}</Heading>
                    <Text size="2" color="gray" mb="4" as="p">{t('settings.import_export_hint')}</Text>
                    <Flex gap="3" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                        <Button onClick={handleExport} disabled={actionLoading === 'export'} style={isMobile ? { width: '100%' } : {}}>
                            <Download size={14} /> {actionLoading === 'export' ? t('settings.exporting') : t('settings.export_config')}
                        </Button>
                        <Button variant="soft" color="gray" onClick={() => fileInputRef.current?.click()} disabled={actionLoading === 'import'} style={isMobile ? { width: '100%' } : {}}>
                            <Upload size={14} /> {actionLoading === 'import' ? t('settings.importing') : t('settings.import_config')}
                        </Button>
                        <input ref={fileInputRef} type="file" accept=".json" onChange={handleImport} style={{ display: 'none' }} />
                    </Flex>
                    <Callout.Root color="orange" size="1" mt="4">
                        <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                        <Callout.Text>{t('settings.import_warning')}</Callout.Text>
                    </Callout.Root>
                </Card>
            </Tabs.Content>

            {/* Security (2FA) */}
            <Tabs.Content value="security">
                <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex justify="between" align="center" mb="4">
                        <Flex direction="column" gap="1">
                            <Heading size="3">{t('twofa.setup_title')}</Heading>
                            <Text size="2" color="gray">{t('twofa.setup_description')}</Text>
                        </Flex>
                        <Badge color={user?.totp_enabled ? 'green' : 'gray'} variant="soft" size="2">
                            {user?.totp_enabled ? t('twofa.enabled_badge') : t('twofa.disabled_badge')}
                        </Badge>
                    </Flex>
                    <Separator size="4" mb="4" />

                    {twofaStep === 'idle' && !user?.totp_enabled && (
                        <Button onClick={handleSetup2FA} disabled={twofaLoading}>
                            <ShieldCheck size={14} /> {twofaLoading ? t('common.loading') : t('twofa.enable')}
                        </Button>
                    )}

                    {twofaStep === 'idle' && user?.totp_enabled && (
                        <Flex direction="column" gap="3">
                            <Text size="2" color="gray">{t('twofa.disable_confirm')}</Text>
                            <Flex gap="2" align="end" direction={isMobile ? 'column' : 'row'}>
                                <TextField.Root placeholder={t('twofa.code_placeholder')} value={twofaCode} onChange={(e) => setTwofaCode(e.target.value.replace(/\D/g, '').slice(0, 6))} size="2" maxLength={6} style={isMobile ? { width: '100%' } : { width: 200 }} />
                                <Button color="red" variant="soft" onClick={handleDisable2FA} disabled={twofaLoading || twofaCode.length < 6} style={isMobile ? { width: '100%' } : {}}>
                                    {twofaLoading ? t('twofa.disabling') : t('twofa.confirm_disable')}
                                </Button>
                            </Flex>
                        </Flex>
                    )}

                    {twofaStep === 'qr' && (
                        <Flex direction="column" gap="4" align="center">
                            <Text size="2" weight="medium">{t('twofa.scan_qr')}</Text>
                            <Box style={{ padding: 16, background: 'white', borderRadius: 12, display: 'inline-block' }}>
                                <QRCodeSVG value={twofaUri} size={200} />
                            </Box>
                            <Text size="2" color="gray">{t('twofa.enter_code')}</Text>
                            <Flex gap="2" align="end" direction={isMobile ? 'column' : 'row'} style={{ width: '100%', maxWidth: 360 }}>
                                <TextField.Root placeholder={t('twofa.code_placeholder')} value={twofaCode} onChange={(e) => setTwofaCode(e.target.value.replace(/\D/g, '').slice(0, 6))} size="2" maxLength={6} autoFocus style={{ flex: 1, textAlign: 'center', letterSpacing: '0.2em', fontFamily: 'monospace' }} />
                                <Button onClick={handleVerify2FA} disabled={twofaLoading || twofaCode.length < 6} style={isMobile ? { width: '100%' } : {}}>
                                    {twofaLoading ? t('twofa.confirming') : t('twofa.confirm_enable')}
                                </Button>
                            </Flex>
                            <Text size="1" color="gray" style={{ cursor: 'pointer', textDecoration: 'underline' }} onClick={() => { setTwofaStep('idle'); setTwofaCode(''); setTwofaUri('') }}>
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
                            <Box style={{ background: 'var(--cp-code-bg)', border: '1px solid var(--cp-border)', borderRadius: 8, padding: 16 }}>
                                <Flex wrap="wrap" gap="2">
                                    {recoveryCodes.map((code, i) => (
                                        <Code key={i} size="3" style={{ fontFamily: 'monospace' }}>{code}</Code>
                                    ))}
                                </Flex>
                            </Box>
                            <Flex gap="2" direction={isMobile ? 'column' : 'row'}>
                                <Button variant="soft" onClick={handleCopyRecoveryCodes} style={isMobile ? { width: '100%' } : {}}>
                                    <Copy size={14} /> {t('twofa.copy_codes')}
                                </Button>
                                <Button onClick={() => { setTwofaStep('idle'); setRecoveryCodes([]) }} style={isMobile ? { width: '100%' } : {}}>
                                    {t('twofa.done')}
                                </Button>
                            </Flex>
                        </Flex>
                    )}
                </Card>
            </Tabs.Content>

            {/* Groups & Tags */}
            <Tabs.Content value="groups-tags">
                <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex justify="between" align="center" mb="4">
                        <Heading size="3"><FolderOpen size={16} style={{ marginRight: 6, verticalAlign: 'middle' }} />{t('group.title')}</Heading>
                    </Flex>
                    <Flex gap="2" mb="4" align="end" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                        <Flex direction="column" gap="1" style={{ flex: 1, minWidth: 120 }}>
                            <Text size="1" color="gray">{t('group.name')}</Text>
                            <TextField.Root placeholder={t('group.name_placeholder')} value={groupForm.name} onChange={(e) => setGroupForm({ ...groupForm, name: e.target.value })} size="2" />
                        </Flex>
                        <Flex direction="column" gap="1">
                            <Text size="1" color="gray">{t('group.color')}</Text>
                            <Flex gap="1">
                                {PRESET_COLORS.map(c => (
                                    <Box key={c} onClick={() => setGroupForm({ ...groupForm, color: c })} style={{ width: 24, height: 24, borderRadius: '50%', cursor: 'pointer', outline: groupForm.color === c ? '2px solid var(--cp-text)' : 'none', outlineOffset: 2 }}>
                                        <Badge color={c} variant="solid" size="1" style={{ width: 24, height: 24, borderRadius: '50%', display: 'block' }}>&nbsp;</Badge>
                                    </Box>
                                ))}
                            </Flex>
                        </Flex>
                        <Flex gap="2">
                            <Button size="2" onClick={handleSaveGroup} disabled={groupTagLoading || !groupForm.name.trim()}>
                                {editingGroup ? t('common.save') : <><Plus size={14} /> {t('group.create')}</>}
                            </Button>
                            {editingGroup && (
                                <Button size="2" variant="soft" color="gray" onClick={() => { setEditingGroup(null); setGroupForm({ name: '', color: 'gray' }) }}>
                                    {t('common.cancel')}
                                </Button>
                            )}
                        </Flex>
                    </Flex>

                    {groups.length === 0 ? (
                        <Text size="2" color="gray">{t('group.no_groups')}</Text>
                    ) : (
                        <Flex direction="column" gap="2">
                            {groups.map(group => (
                                <Card key={group.id} style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                                    <Flex justify="between" align="center" wrap="wrap" gap="2">
                                        <Badge color={group.color || 'gray'} variant="solid" size="2">
                                            <FolderOpen size={12} /> {group.name}
                                        </Badge>
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

                <Card mt="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex justify="between" align="center" mb="4">
                        <Heading size="3"><Tags size={16} style={{ marginRight: 6, verticalAlign: 'middle' }} />{t('tag.title')}</Heading>
                    </Flex>
                    <Flex gap="2" mb="4" align="end" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                        <Flex direction="column" gap="1" style={{ flex: 1, minWidth: 120 }}>
                            <Text size="1" color="gray">{t('tag.name')}</Text>
                            <TextField.Root placeholder={t('tag.name_placeholder')} value={tagForm.name} onChange={(e) => setTagForm({ ...tagForm, name: e.target.value })} size="2" />
                        </Flex>
                        <Flex direction="column" gap="1">
                            <Text size="1" color="gray">{t('tag.color')}</Text>
                            <Flex gap="1">
                                {PRESET_COLORS.map(c => (
                                    <Box key={c} onClick={() => setTagForm({ ...tagForm, color: c })} style={{ width: 24, height: 24, borderRadius: '50%', cursor: 'pointer', outline: tagForm.color === c ? '2px solid var(--cp-text)' : 'none', outlineOffset: 2 }}>
                                        <Badge color={c} variant="solid" size="1" style={{ width: 24, height: 24, borderRadius: '50%', display: 'block' }}>&nbsp;</Badge>
                                    </Box>
                                ))}
                            </Flex>
                        </Flex>
                        <Flex gap="2">
                            <Button size="2" onClick={handleSaveTag} disabled={groupTagLoading || !tagForm.name.trim()}>
                                {editingTag ? t('common.save') : <><Plus size={14} /> {t('tag.create')}</>}
                            </Button>
                            {editingTag && (
                                <Button size="2" variant="soft" color="gray" onClick={() => { setEditingTag(null); setTagForm({ name: '', color: 'gray' }) }}>
                                    {t('common.cancel')}
                                </Button>
                            )}
                        </Flex>
                    </Flex>

                    {allTags.length === 0 ? (
                        <Text size="2" color="gray">{t('tag.no_tags')}</Text>
                    ) : (
                        <Flex gap="2" wrap="wrap">
                            {allTags.map(tag => (
                                <Card key={tag.id} style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)', padding: '8px 12px' }}>
                                    <Flex align="center" gap="2">
                                        <Badge color={tag.color || 'gray'} variant="solid" size="2">{tag.name}</Badge>
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
    )
}


// ======================== Users Tab ========================
function UsersTab({ showMessage }) {
    const { t } = useTranslation()
    const [users, setUsers] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editUser, setEditUser] = useState(null)
    const [form, setForm] = useState({ username: '', password: '', role: 'viewer' })

    const fetchUsers = async () => {
        try { const res = await userAPI.list(); setUsers(res.data.users || []) }
        catch { /* ignore */ }
        finally { setLoading(false) }
    }

    useEffect(() => { fetchUsers() }, [])

    const openCreate = () => { setEditUser(null); setForm({ username: '', password: '', role: 'viewer' }); setDialogOpen(true) }
    const openEdit = (user) => { setEditUser(user); setForm({ username: user.username, password: '', role: user.role }); setDialogOpen(true) }

    const handleSubmit = async () => {
        try {
            if (editUser) {
                const data = {}
                if (form.password) data.password = form.password
                if (form.role !== editUser.role) data.role = form.role
                await userAPI.update(editUser.id, data)
                showMessage('success', t('user.update_success'))
            } else {
                await userAPI.create(form)
                showMessage('success', t('user.create_success'))
            }
            setDialogOpen(false)
            fetchUsers()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.operation_failed'))
        }
    }

    const handleDelete = async (user) => {
        if (!confirm(t('user.confirm_delete', { username: user.username }))) return
        try { await userAPI.delete(user.id); showMessage('success', t('user.delete_success')); fetchUsers() }
        catch (err) { showMessage('error', err.response?.data?.error || t('common.delete_failed')) }
    }

    return (
        <Box mt="4">
            <Flex justify="between" align="center" mb="4">
                <Text size="3" weight="bold">{t('user.title')}</Text>
                <Button size="2" onClick={openCreate}><Plus size={14} /> {t('user.add_user')}</Button>
            </Flex>

            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Table.Root>
                    <Table.Header>
                        <Table.Row>
                            <Table.ColumnHeaderCell>ID</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('user.username')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('user.role')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('user.created_at')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                        </Table.Row>
                    </Table.Header>
                    <Table.Body>
                        {users.map((user) => (
                            <Table.Row key={user.id}>
                                <Table.Cell>{user.id}</Table.Cell>
                                <Table.Cell><Text weight="medium">{user.username}</Text></Table.Cell>
                                <Table.Cell>
                                    <Badge color={user.role === 'admin' ? 'blue' : 'gray'} size="1">
                                        {user.role === 'admin' ? <Flex align="center" gap="1"><Shield size={12} /> {t('user.admin')}</Flex> : <Flex align="center" gap="1"><Eye size={12} /> {t('user.viewer')}</Flex>}
                                    </Badge>
                                </Table.Cell>
                                <Table.Cell><Text size="1" color="gray">{new Date(user.created_at).toLocaleDateString()}</Text></Table.Cell>
                                <Table.Cell>
                                    <Flex gap="2">
                                        <IconButton size="1" variant="ghost" onClick={() => openEdit(user)}><Pencil size={14} /></IconButton>
                                        <IconButton size="1" variant="ghost" color="red" onClick={() => handleDelete(user)}><Trash2 size={14} /></IconButton>
                                    </Flex>
                                </Table.Cell>
                            </Table.Row>
                        ))}
                        {users.length === 0 && !loading && (
                            <Table.Row>
                                <Table.Cell colSpan={5}><Text color="gray" size="2">{t('common.no_data')}</Text></Table.Cell>
                            </Table.Row>
                        )}
                    </Table.Body>
                </Table.Root>
            </Card>

            <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
                <Dialog.Content style={{ maxWidth: 420 }}>
                    <Dialog.Title>{editUser ? t('user.edit_user') : t('user.add_user')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        {!editUser && (
                            <Box>
                                <Text size="2" mb="1" weight="medium">{t('user.username')}</Text>
                                <TextField.Root value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} placeholder={t('user.username')} />
                            </Box>
                        )}
                        <Box>
                            <Text size="2" mb="1" weight="medium">{editUser ? t('user.new_password_hint') : t('user.password')}</Text>
                            <TextField.Root type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} placeholder={editUser ? t('user.leave_blank_to_keep') : t('user.password_min_length')} />
                        </Box>
                        <Box>
                            <Text size="2" mb="1" weight="medium">{t('user.role')}</Text>
                            <Select.Root value={form.role} onValueChange={(v) => setForm({ ...form, role: v })}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    <Select.Item value="admin">{t('user.admin_full')}</Select.Item>
                                    <Select.Item value="viewer">{t('user.viewer_only')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Box>
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleSubmit}>{editUser ? t('common.save') : t('common.create')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}


// ======================== Logs Tab ========================
function LogsTab() {
    const { t } = useTranslation()
    const [activeTab, setActiveTab] = useState('caddy')

    return (
        <Box mt="4">
            <Tabs.Root value={activeTab} onValueChange={setActiveTab}>
                <Tabs.List>
                    <Tabs.Trigger value="caddy"><FileText size={14} style={{ marginRight: 6 }} /> {t('log.caddy_logs')}</Tabs.Trigger>
                    <Tabs.Trigger value="audit"><ClipboardList size={14} style={{ marginRight: 6 }} /> {t('audit.title')}</Tabs.Trigger>
                    <Tabs.Trigger value="deploy"><Package size={14} style={{ marginRight: 6 }} /> {t('log.deploy_logs')}</Tabs.Trigger>
                    <Tabs.Trigger value="backup"><HardDrive size={14} style={{ marginRight: 6 }} /> {t('log.backup_logs')}</Tabs.Trigger>
                    <Tabs.Trigger value="system"><Server size={14} style={{ marginRight: 6 }} /> {t('log.system_logs')}</Tabs.Trigger>
                </Tabs.List>
                <Tabs.Content value="caddy"><CaddyLogsPanel /></Tabs.Content>
                <Tabs.Content value="audit"><AuditLogsPanel /></Tabs.Content>
                <Tabs.Content value="deploy"><DeployLogsPanel /></Tabs.Content>
                <Tabs.Content value="backup"><BackupLogsPanel /></Tabs.Content>
                <Tabs.Content value="system"><SystemLogsPanel /></Tabs.Content>
            </Tabs.Root>
        </Box>
    )
}

function CaddyLogsPanel() {
    const { t } = useTranslation()
    const [logType, setLogType] = useState('caddy')
    const [lines, setLines] = useState('200')
    const [search, setSearch] = useState('')
    const [logLines, setLogLines] = useState([])
    const [logFiles, setLogFiles] = useState([])
    const [loading, setLoading] = useState(false)
    const logEndRef = useRef(null)

    const fetchLogFiles = async () => { try { const res = await logAPI.files(); setLogFiles(res.data.files || []) } catch { /* ignore */ } }
    const fetchLogs = async () => {
        setLoading(true)
        try {
            const res = await logAPI.get({ type: logType, lines, search })
            setLogLines(res.data.lines || [])
            setTimeout(() => logEndRef.current?.scrollIntoView({ behavior: 'smooth' }), 100)
        } catch { setLogLines([]) }
        finally { setLoading(false) }
    }

    useEffect(() => { fetchLogFiles(); fetchLogs() }, [logType, lines])

    const handleSearch = (e) => { e.preventDefault(); fetchLogs() }
    const handleDownload = () => {
        const token = localStorage.getItem('token')
        const url = logAPI.downloadUrl(logType)
        const link = document.createElement('a'); link.href = `${url}&token=${token}`; link.download = ''; link.click()
    }

    return (
        <Box mt="4">
            <Flex justify="end" gap="2" mb="3">
                <Button variant="soft" size="1" onClick={fetchLogs} disabled={loading}>
                    <RefreshCw size={14} className={loading ? 'animate-spin' : ''} /> {t('common.refresh')}
                </Button>
                <Button variant="soft" color="gray" size="1" onClick={handleDownload}>
                    <Download size={14} /> {t('common.download')}
                </Button>
            </Flex>

            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }} mb="4">
                <Flex gap="3" align="end" wrap="wrap">
                    <Flex direction="column" gap="1" style={{ minWidth: 180 }}>
                        <Text size="1" weight="medium" color="gray">{t('log.select_file')}</Text>
                        <Select.Root value={logType} onValueChange={setLogType}>
                            <Select.Trigger />
                            <Select.Content>
                                <Select.Item value="caddy">{t('log.main_log')}</Select.Item>
                                {logFiles.filter((f) => f.name !== 'caddy.log').map((f) => (
                                    <Select.Item key={f.name} value={f.name}>{f.name}</Select.Item>
                                ))}
                            </Select.Content>
                        </Select.Root>
                    </Flex>
                    <Flex direction="column" gap="1" style={{ minWidth: 100 }}>
                        <Text size="1" weight="medium" color="gray">{t('log.lines')}</Text>
                        <Select.Root value={lines} onValueChange={setLines}>
                            <Select.Trigger />
                            <Select.Content>
                                {['100', '200', '500', '1000', '5000'].map(n => <Select.Item key={n} value={n}>{n}</Select.Item>)}
                            </Select.Content>
                        </Select.Root>
                    </Flex>
                    <form onSubmit={handleSearch} style={{ flex: 1, minWidth: 200 }}>
                        <Flex direction="column" gap="1">
                            <Text size="1" weight="medium" color="gray">{t('common.search')}</Text>
                            <Flex gap="2">
                                <TextField.Root style={{ flex: 1 }} placeholder={t('log.search_placeholder')} value={search} onChange={(e) => setSearch(e.target.value)} size="2">
                                    <TextField.Slot><Search size={14} style={{ color: 'var(--cp-text-muted)' }} /></TextField.Slot>
                                </TextField.Root>
                                <Button type="submit" variant="soft" size="2">{t('log.filter')}</Button>
                            </Flex>
                        </Flex>
                    </form>
                </Flex>
            </Card>

            <Card style={{ background: 'var(--cp-code-bg)', border: '1px solid var(--cp-border)', padding: 0 }}>
                <Flex justify="between" align="center" px="4" py="2" style={{ borderBottom: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2"><FileText size={14} style={{ color: 'var(--cp-text-muted)' }} /><Text size="1" color="gray">{logType}</Text></Flex>
                    <Badge variant="soft" size="1">{t('log.lines_count', { count: logLines.length })}</Badge>
                </Flex>
                <Box p="4" style={{ maxHeight: 500, overflow: 'auto' }}>
                    {logLines.length === 0 ? (
                        <Flex justify="center" p="6"><Text size="2" color="gray">{loading ? t('common.loading') : t('log.no_logs')}</Text></Flex>
                    ) : (
                        <div className="log-viewer">
                            {logLines.map((line, i) => (
                                <div key={i} style={{ padding: '1px 0', borderBottom: '1px solid rgba(255,255,255,0.02)', display: 'flex', gap: 12 }}>
                                    <span style={{ color: 'var(--cp-text-muted)', userSelect: 'none', minWidth: 40, textAlign: 'right' }}>{i + 1}</span>
                                    <span style={{ color: 'var(--cp-text)' }}>{line}</span>
                                </div>
                            ))}
                            <div ref={logEndRef} />
                        </div>
                    )}
                </Box>
            </Card>
        </Box>
    )
}

function AuditLogsPanel() {
    const { t } = useTranslation()
    const actionColors = { CREATE: 'green', UPDATE: 'blue', DELETE: 'red', ENABLE: 'green', DISABLE: 'orange', TOGGLE: 'orange', START: 'green', STOP: 'red', RELOAD: 'blue' }
    const [logs, setLogs] = useState([])
    const [total, setTotal] = useState(0)
    const [page, setPage] = useState(1)
    const [loading, setLoading] = useState(true)
    const perPage = 20

    const fetchLogs = async (p = 1) => {
        setLoading(true)
        try { const res = await auditAPI.list({ page: p, per_page: perPage }); setLogs(res.data.logs || []); setTotal(res.data.total || 0); setPage(p) }
        catch { /* ignore */ }
        finally { setLoading(false) }
    }

    useEffect(() => { fetchLogs() }, [])

    const totalPages = Math.ceil(total / perPage)

    return (
        <Box mt="4">
            <Text size="2" color="gray" mb="3" as="p">{t('audit.subtitle_with_count', { count: total })}</Text>
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                {loading ? (
                    <Flex justify="center" p="6"><Spinner size="3" /></Flex>
                ) : (
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('audit.time')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.user')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.action')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.target')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.detail')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.ip')}</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {logs.map((log) => (
                                <Table.Row key={log.id}>
                                    <Table.Cell><Text size="1" color="gray">{new Date(log.created_at).toLocaleString()}</Text></Table.Cell>
                                    <Table.Cell><Text size="2" weight="medium">{log.username}</Text></Table.Cell>
                                    <Table.Cell><Badge color={actionColors[log.action] || 'gray'} size="1">{log.action}</Badge></Table.Cell>
                                    <Table.Cell><Text size="2">{log.target}</Text>{log.target_id && <Text size="1" color="gray"> #{log.target_id}</Text>}</Table.Cell>
                                    <Table.Cell><Text size="1" style={{ maxWidth: 300, display: 'block' }}>{log.detail}</Text></Table.Cell>
                                    <Table.Cell><Text size="1" color="gray">{log.ip}</Text></Table.Cell>
                                </Table.Row>
                            ))}
                            {logs.length === 0 && (
                                <Table.Row><Table.Cell colSpan={6}><Text color="gray" size="2">{t('audit.no_logs')}</Text></Table.Cell></Table.Row>
                            )}
                        </Table.Body>
                    </Table.Root>
                )}
                {totalPages > 1 && (
                    <Flex justify="center" align="center" gap="3" pt="3" pb="1">
                        <Button size="1" variant="soft" disabled={page <= 1} onClick={() => fetchLogs(page - 1)}>
                            <ChevronLeft size={14} /> {t('common.prev_page')}
                        </Button>
                        <Text size="2" color="gray">{page} / {totalPages}</Text>
                        <Button size="1" variant="soft" disabled={page >= totalPages} onClick={() => fetchLogs(page + 1)}>
                            {t('common.next_page')} <ChevronRight size={14} />
                        </Button>
                    </Flex>
                )}
            </Card>
        </Box>
    )
}


// ======================== Deploy Logs Panel ========================
function DeployLogsPanel() {
    const { t } = useTranslation()
    const [projects, setProjects] = useState([])
    const [selectedProject, setSelectedProject] = useState('')
    const [deployments, setDeployments] = useState([])
    const [logLines, setLogLines] = useState([])
    const [loading, setLoading] = useState(false)
    const [logLoading, setLogLoading] = useState(false)

    useEffect(() => {
        deployAPI.listProjects().then(res => {
            const list = res.data?.projects || res.data || []
            setProjects(Array.isArray(list) ? list : [])
        }).catch(() => {})
    }, [])

    useEffect(() => {
        if (!selectedProject) { setDeployments([]); setLogLines([]); return }
        setLoading(true)
        deployAPI.deployments(selectedProject).then(res => {
            const list = res.data?.deployments || res.data || []
            setDeployments(Array.isArray(list) ? list : [])
        }).catch(() => setDeployments([])).finally(() => setLoading(false))
    }, [selectedProject])

    const viewLog = async (projectId, params) => {
        setLogLoading(true)
        try {
            const res = await deployAPI.logs(projectId, params)
            const content = res.data?.lines || res.data?.content || ''
            setLogLines(typeof content === 'string' ? content.split('\n') : Array.isArray(content) ? content : [])
        } catch { setLogLines([]) }
        finally { setLogLoading(false) }
    }

    return (
        <Box mt="4">
            <Flex gap="3" align="end" mb="3">
                <Box style={{ minWidth: 200 }}>
                    <Text size="1" weight="medium" color="gray" style={{ display: 'block', marginBottom: 4 }}>{t('log.select_project')}</Text>
                    <Select.Root value={selectedProject} onValueChange={setSelectedProject}>
                        <Select.Trigger placeholder={t('log.select_project')} />
                        <Select.Content>
                            {projects.map(p => <Select.Item key={p.id} value={String(p.id)}>{p.name}</Select.Item>)}
                        </Select.Content>
                    </Select.Root>
                </Box>
            </Flex>
            {selectedProject && (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }} mb="3">
                    {loading ? <Flex justify="center" p="4"><Spinner size="2" /></Flex> : (
                        <Table.Root size="1">
                            <Table.Header>
                                <Table.Row>
                                    <Table.ColumnHeaderCell>#</Table.ColumnHeaderCell>
                                    <Table.ColumnHeaderCell>{t('log.deploy_status')}</Table.ColumnHeaderCell>
                                    <Table.ColumnHeaderCell>{t('log.deploy_commit')}</Table.ColumnHeaderCell>
                                    <Table.ColumnHeaderCell>{t('log.deploy_time')}</Table.ColumnHeaderCell>
                                    <Table.ColumnHeaderCell></Table.ColumnHeaderCell>
                                </Table.Row>
                            </Table.Header>
                            <Table.Body>
                                {deployments.map(d => (
                                    <Table.Row key={d.id}>
                                        <Table.Cell><Text size="2">#{d.build_num}</Text></Table.Cell>
                                        <Table.Cell><Badge color={d.status === 'success' ? 'green' : d.status === 'building' ? 'yellow' : 'red'} size="1">{d.status}</Badge></Table.Cell>
                                        <Table.Cell><Text size="1" color="gray">{d.git_commit || '-'}</Text></Table.Cell>
                                        <Table.Cell><Text size="1" color="gray">{new Date(d.created_at).toLocaleString()}</Text></Table.Cell>
                                        <Table.Cell><Button size="1" variant="ghost" onClick={() => viewLog(selectedProject, { build_num: d.build_num })}><FileText size={12} /> {t('log.view')}</Button></Table.Cell>
                                    </Table.Row>
                                ))}
                                {deployments.length === 0 && <Table.Row><Table.Cell colSpan={5}><Text color="gray" size="2">{t('log.no_deployments')}</Text></Table.Cell></Table.Row>}
                            </Table.Body>
                        </Table.Root>
                    )}
                </Card>
            )}
            {logLines.length > 0 && (
                <Card style={{ background: 'var(--cp-code-bg)', border: '1px solid var(--cp-border)', padding: 0 }}>
                    <Flex justify="between" align="center" px="4" py="2" style={{ borderBottom: '1px solid var(--cp-border)' }}>
                        <Text size="1" color="gray">{t('log.build_log')}</Text>
                        <Badge variant="soft" size="1">{logLines.length} {t('log.lines')}</Badge>
                    </Flex>
                    <Box p="4" style={{ maxHeight: 400, overflow: 'auto' }}>
                        {logLoading ? <Flex justify="center" p="4"><Spinner size="2" /></Flex> : (
                            <div className="log-viewer">
                                {logLines.map((line, i) => (
                                    <div key={i} style={{ padding: '1px 0', borderBottom: '1px solid rgba(255,255,255,0.02)', display: 'flex', gap: 12 }}>
                                        <span style={{ color: 'var(--cp-text-muted)', userSelect: 'none', minWidth: 40, textAlign: 'right' }}>{i + 1}</span>
                                        <span style={{ color: 'var(--cp-text)' }}>{line}</span>
                                    </div>
                                ))}
                            </div>
                        )}
                    </Box>
                </Card>
            )}
        </Box>
    )
}


// ======================== Backup Logs Panel ========================
function BackupLogsPanel() {
    const { t } = useTranslation()
    const [logs, setLogs] = useState([])
    const [loading, setLoading] = useState(true)

    useEffect(() => {
        backupAPI.listLogs().then(res => setLogs(res.data?.logs || []))
            .catch(() => {}).finally(() => setLoading(false))
    }, [])

    const levelColors = { info: 'blue', warn: 'orange', error: 'red', success: 'green' }

    return (
        <Box mt="4">
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                {loading ? <Flex justify="center" p="6"><Spinner size="3" /></Flex> : (
                    <Table.Root size="1">
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('log.time')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('log.level')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('log.message')}</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {logs.map((log, i) => (
                                <Table.Row key={log.id || i}>
                                    <Table.Cell><Text size="1" color="gray">{new Date(log.created_at).toLocaleString()}</Text></Table.Cell>
                                    <Table.Cell><Badge color={levelColors[log.level] || 'gray'} size="1">{(log.level || 'info').toUpperCase()}</Badge></Table.Cell>
                                    <Table.Cell><Text size="2">{log.message}</Text></Table.Cell>
                                </Table.Row>
                            ))}
                            {logs.length === 0 && <Table.Row><Table.Cell colSpan={3}><Text color="gray" size="2">{t('log.no_backup_logs')}</Text></Table.Cell></Table.Row>}
                        </Table.Body>
                    </Table.Root>
                )}
            </Card>
        </Box>
    )
}


// ======================== System Logs Panel ========================
function SystemLogsPanel() {
    const { t } = useTranslation()
    const [logLines, setLogLines] = useState([])
    const [lines, setLines] = useState('200')
    const [search, setSearch] = useState('')
    const [loading, setLoading] = useState(false)
    const logEndRef = useRef(null)

    const fetchLogs = async () => {
        setLoading(true)
        try {
            const res = await logAPI.system({ lines, search })
            setLogLines(res.data?.lines || [])
            setTimeout(() => logEndRef.current?.scrollIntoView({ behavior: 'smooth' }), 100)
        } catch { setLogLines([]) }
        finally { setLoading(false) }
    }

    useEffect(() => { fetchLogs() }, [lines])

    const handleSearch = (e) => { e.preventDefault(); fetchLogs() }

    return (
        <Box mt="4">
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }} mb="4">
                <Flex gap="3" align="end" wrap="wrap">
                    <Flex direction="column" gap="1" style={{ minWidth: 100 }}>
                        <Text size="1" weight="medium" color="gray">{t('log.lines')}</Text>
                        <Select.Root value={lines} onValueChange={setLines}>
                            <Select.Trigger />
                            <Select.Content>
                                {['100', '200', '500', '1000', '5000'].map(n => <Select.Item key={n} value={n}>{n}</Select.Item>)}
                            </Select.Content>
                        </Select.Root>
                    </Flex>
                    <form onSubmit={handleSearch} style={{ flex: 1, minWidth: 200 }}>
                        <Flex direction="column" gap="1">
                            <Text size="1" weight="medium" color="gray">{t('common.search')}</Text>
                            <Flex gap="2">
                                <TextField.Root style={{ flex: 1 }} placeholder={t('log.search_placeholder')} value={search} onChange={(e) => setSearch(e.target.value)} size="2">
                                    <TextField.Slot><Search size={14} style={{ color: 'var(--cp-text-muted)' }} /></TextField.Slot>
                                </TextField.Root>
                                <Button type="submit" variant="soft" size="2">{t('log.filter')}</Button>
                            </Flex>
                        </Flex>
                    </form>
                    <Button variant="soft" size="1" onClick={fetchLogs} disabled={loading}>
                        <RefreshCw size={14} className={loading ? 'animate-spin' : ''} /> {t('common.refresh')}
                    </Button>
                </Flex>
            </Card>
            <Card style={{ background: 'var(--cp-code-bg)', border: '1px solid var(--cp-border)', padding: 0 }}>
                <Flex justify="between" align="center" px="4" py="2" style={{ borderBottom: '1px solid var(--cp-border)' }}>
                    <Flex align="center" gap="2"><Server size={14} style={{ color: 'var(--cp-text-muted)' }} /><Text size="1" color="gray">{t('log.system_logs')}</Text></Flex>
                    <Badge variant="soft" size="1">{t('log.lines_count', { count: logLines.length })}</Badge>
                </Flex>
                <Box p="4" style={{ maxHeight: 500, overflow: 'auto' }}>
                    {logLines.length === 0 ? (
                        <Flex justify="center" p="6"><Text size="2" color="gray">{loading ? t('common.loading') : t('log.no_logs')}</Text></Flex>
                    ) : (
                        <div className="log-viewer">
                            {logLines.map((line, i) => (
                                <div key={i} style={{ padding: '1px 0', borderBottom: '1px solid rgba(255,255,255,0.02)', display: 'flex', gap: 12 }}>
                                    <span style={{ color: 'var(--cp-text-muted)', userSelect: 'none', minWidth: 40, textAlign: 'right' }}>{i + 1}</span>
                                    <span style={{ color: 'var(--cp-text)' }}>{line}</span>
                                </div>
                            ))}
                            <div ref={logEndRef} />
                        </div>
                    )}
                </Box>
            </Card>
        </Box>
    )
}


// ======================== AI Tab ========================
function AITab({ showMessage }) {
    const { t } = useTranslation()
    const [config, setConfig] = useState({ base_url: '', api_key: '', model: '', api_format: 'openai-chat', embedding_model: '', embedding_base_url: '', embedding_api_key: '' })
    const [presets, setPresets] = useState({})
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [testing, setTesting] = useState(false)
    const [testResult, setTestResult] = useState(null)

    useEffect(() => {
        Promise.all([
            aiAPI.getConfig().then(res => setConfig(res.data || { base_url: '', api_key: '', model: '', api_format: 'openai-chat', embedding_model: '', embedding_base_url: '', embedding_api_key: '' })),
            aiAPI.getPresets().then(res => setPresets(res.data || {})).catch(() => {}),
        ]).catch(() => {}).finally(() => setLoading(false))
    }, [])

    const handleSave = async () => {
        setSaving(true); setTestResult(null)
        try { await aiAPI.updateConfig(config); setTestResult({ ok: true, msg: t('ai.config_saved') }) }
        catch (e) { setTestResult({ ok: false, msg: e.response?.data?.error || e.message }) }
        finally { setSaving(false) }
    }

    const handleTest = async () => {
        setTesting(true); setTestResult(null)
        try {
            await aiAPI.updateConfig(config)
            await aiAPI.testConnection()
            setTestResult({ ok: true, msg: t('ai.connection_ok') })
        }
        catch (e) { setTestResult({ ok: false, msg: e.response?.data?.error || e.message }) }
        finally { setTesting(false) }
    }

    const applyPreset = (key) => {
        const p = presets[key]
        if (!p) return
        setConfig(prev => ({ ...prev, base_url: p.base_url, api_format: p.api_format, model: p.models?.[0] || '', embedding_model: p.embedding_models?.[0] || '' }))
        setTestResult(null)
    }

    // Find current preset's model list
    const currentPresetModels = Object.values(presets).find(p => p.base_url === config.base_url)?.models || []

    if (loading) return <Flex justify="center" p="6"><Text>{t('common.loading')}</Text></Flex>

    return (
        <Card mt="4" style={{ maxWidth: 680, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
            <Flex direction="column" gap="4">
                {/* Provider Presets */}
                <Box>
                    <Text size="2" weight="bold" mb="2" style={{ display: 'block' }}>{t('ai.provider_presets')}</Text>
                    <Flex gap="2" wrap="wrap">
                        {Object.entries(presets).map(([key, p]) => (
                            <Button key={key} size="1" variant={config.base_url === p.base_url ? 'solid' : 'outline'}
                                onClick={() => applyPreset(key)}>
                                {p.name}
                            </Button>
                        ))}
                    </Flex>
                    <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.preset_hint')}</Text>
                </Box>
                <Separator size="4" />
                {/* API Format */}
                <Box>
                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('ai.api_format')}</Text>
                    <Select.Root value={config.api_format || 'openai-chat'} onValueChange={(v) => setConfig(prev => ({ ...prev, api_format: v }))}>
                        <Select.Trigger style={{ width: '100%' }} />
                        <Select.Content>
                            <Select.Item value="openai-chat">OpenAI Chat Completions</Select.Item>
                            <Select.Item value="anthropic-messages">Anthropic Messages</Select.Item>
                            <Select.Item value="google-generativeai">Google Generative AI</Select.Item>
                        </Select.Content>
                    </Select.Root>
                    <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.api_format_hint')}</Text>
                </Box>
                {/* Base URL */}
                <Box>
                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('ai.base_url')}</Text>
                    <TextField.Root placeholder="https://api.openai.com" value={config.base_url} onChange={(e) => setConfig(prev => ({ ...prev, base_url: e.target.value }))} />
                    <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.base_url_hint')}</Text>
                </Box>
                {/* API Key */}
                <Box>
                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('ai.api_key')}</Text>
                    <TextField.Root type="password" placeholder="sk-..." value={config.api_key} onChange={(e) => setConfig(prev => ({ ...prev, api_key: e.target.value }))} />
                    <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.api_key_hint')}</Text>
                </Box>
                {/* Model */}
                <Box>
                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('ai.model')}</Text>
                    {currentPresetModels.length > 0 ? (
                        <Flex gap="2" align="center">
                            <Select.Root value={currentPresetModels.includes(config.model) ? config.model : '_custom'} onValueChange={(v) => setConfig(prev => ({ ...prev, model: v === '_custom' ? '' : v }))}>
                                <Select.Trigger style={{ flex: 1 }} />
                                <Select.Content>
                                    {currentPresetModels.map(m => <Select.Item key={m} value={m}>{m}</Select.Item>)}
                                    <Select.Item value="_custom">{t('ai.custom_model')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                            {!currentPresetModels.includes(config.model) && (
                                <TextField.Root style={{ flex: 1 }} placeholder={t('ai.model_placeholder')} value={config.model} onChange={(e) => setConfig(prev => ({ ...prev, model: e.target.value }))} />
                            )}
                        </Flex>
                    ) : (
                        <TextField.Root placeholder="gpt-4o / claude-sonnet-4-20250514 / deepseek-chat" value={config.model} onChange={(e) => setConfig(prev => ({ ...prev, model: e.target.value }))} />
                    )}
                    <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.model_hint')}</Text>
                </Box>
                {/* Embedding Model */}
                <Box>
                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('ai.embedding_model')}</Text>
                    {(() => {
                        const currentPresetEmbeddings = Object.values(presets).find(p => p.base_url === config.base_url)?.embedding_models || []
                        if (currentPresetEmbeddings.length > 0) {
                            return (
                                <Flex gap="2" align="center">
                                    <Select.Root value={currentPresetEmbeddings.includes(config.embedding_model) ? config.embedding_model : (config.embedding_model ? '_custom' : '_none')} onValueChange={(v) => {
                                        if (v === '_none') {
                                            setConfig(prev => ({ ...prev, embedding_model: '' }))
                                        } else if (v === '_custom') {
                                            setConfig(prev => ({ ...prev, embedding_model: prev.embedding_model && !currentPresetEmbeddings.includes(prev.embedding_model) ? prev.embedding_model : 'custom-model' }))
                                        } else {
                                            setConfig(prev => ({ ...prev, embedding_model: v }))
                                        }
                                    }}>
                                        <Select.Trigger style={{ flex: 1 }} />
                                        <Select.Content>
                                            <Select.Item value="_none">{t('ai.embedding_disabled')}</Select.Item>
                                            {currentPresetEmbeddings.map(m => <Select.Item key={m} value={m}>{m}</Select.Item>)}
                                            <Select.Item value="_custom">{t('ai.custom_model')}</Select.Item>
                                        </Select.Content>
                                    </Select.Root>
                                    {config.embedding_model && !currentPresetEmbeddings.includes(config.embedding_model) && (
                                        <TextField.Root style={{ flex: 1 }} placeholder={t('ai.embedding_model_placeholder')} value={config.embedding_model} onChange={(e) => setConfig(prev => ({ ...prev, embedding_model: e.target.value }))} />
                                    )}
                                </Flex>
                            )
                        }
                        return (
                            <TextField.Root placeholder={t('ai.embedding_model_placeholder')} value={config.embedding_model} onChange={(e) => setConfig(prev => ({ ...prev, embedding_model: e.target.value }))} />
                        )
                    })()}
                    <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.embedding_model_hint')}</Text>
                </Box>
                {/* Separate Embedding API Credentials (shown when embedding model is set) */}
                {config.embedding_model && (
                    <Box style={{ padding: '12px', borderRadius: '8px', background: 'var(--gray-a2)', border: '1px solid var(--gray-a4)' }}>
                        <Text size="2" weight="bold" mb="2" style={{ display: 'block' }}>{t('ai.embedding_api_override')}</Text>
                        <Text size="1" color="gray" mb="3" style={{ display: 'block' }}>{t('ai.embedding_api_override_hint')}</Text>
                        <Flex direction="column" gap="3">
                            <Box>
                                <Text size="1" weight="medium" mb="1" style={{ display: 'block' }}>{t('ai.embedding_base_url')}</Text>
                                <TextField.Root placeholder={t('ai.embedding_base_url_placeholder')} value={config.embedding_base_url || ''} onChange={(e) => setConfig(prev => ({ ...prev, embedding_base_url: e.target.value }))} />
                            </Box>
                            <Box>
                                <Text size="1" weight="medium" mb="1" style={{ display: 'block' }}>{t('ai.embedding_api_key')}</Text>
                                <TextField.Root type="password" placeholder={t('ai.embedding_api_key_placeholder')} value={config.embedding_api_key || ''} onChange={(e) => setConfig(prev => ({ ...prev, embedding_api_key: e.target.value }))} />
                            </Box>
                        </Flex>
                    </Box>
                )}
                <Separator size="4" />
                {testResult && (
                    <Badge size="2" color={testResult.ok ? 'green' : 'red'} variant="soft" style={{ padding: '8px 12px' }}>
                        {testResult.ok ? <Check size={14} /> : null} {testResult.msg}
                    </Badge>
                )}
                <Flex gap="2">
                    <Button onClick={handleSave} disabled={saving}><Save size={14} /> {saving ? t('common.saving') : t('common.save')}</Button>
                    <Button variant="soft" onClick={handleTest} disabled={testing}><TestTube size={14} /> {testing ? t('common.loading') : t('ai.test_connection')}</Button>
                </Flex>
            </Flex>
        </Card>
    )
}


// ======================== DNS Tab ========================
function DnsTab() {
    const { t } = useTranslation()
    const getProviderFields = () => ({
        cloudflare: { label: t('dns.cloudflare'), fields: [{ key: 'api_token', label: t('dns.api_token'), placeholder: 'Cloudflare API Token' }] },
        alidns: { label: t('dns.alidns'), fields: [{ key: 'access_key_id', label: t('dns.access_key_id'), placeholder: 'LTAI...' }, { key: 'access_key_secret', label: t('dns.access_key_secret'), placeholder: 'AccessKeySecret' }] },
        tencentcloud: { label: t('dns.tencentcloud'), fields: [{ key: 'secret_id', label: t('dns.secret_id'), placeholder: 'AKIDxxxxxxxx' }, { key: 'secret_key', label: t('dns.secret_key'), placeholder: 'SecretKey' }] },
        route53: { label: t('dns.route53'), fields: [{ key: 'region', label: t('dns.region'), placeholder: 'us-east-1' }, { key: 'access_key_id', label: t('dns.access_key_id'), placeholder: 'AKIA...' }, { key: 'secret_access_key', label: t('dns.access_key_secret'), placeholder: 'SecretAccessKey' }] },
    })
    const PROVIDER_FIELDS = getProviderFields()
    const DEFAULT_FORM = { name: '', provider: 'cloudflare', config: {}, is_default: false }
    const [providers, setProviders] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editId, setEditId] = useState(null)
    const [form, setForm] = useState({ ...DEFAULT_FORM })
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    const [deleteId, setDeleteId] = useState(null)

    const load = useCallback(async () => {
        setLoading(true)
        try { const res = await dnsProviderAPI.list(); setProviders(res.data.providers || []) } catch { /* ignore */ }
        setLoading(false)
    }, [])

    useEffect(() => { load() }, [load])

    const openCreate = () => { setEditId(null); setForm({ ...DEFAULT_FORM }); setError(''); setDialogOpen(true) }
    const openEdit = async (p) => {
        setEditId(p.id); setError('')
        try {
            const res = await dnsProviderAPI.get(p.id); const data = res.data
            let cfg = {}; try { cfg = JSON.parse(data.config) } catch { /* */ }
            setForm({ name: data.name, provider: data.provider, config: cfg, is_default: data.is_default || false })
        } catch {
            setForm({ name: p.name, provider: p.provider, config: {}, is_default: p.is_default || false })
        }
        setDialogOpen(true)
    }

    const handleSave = async () => {
        setError(''); setSaving(true)
        try {
            const payload = { name: form.name, provider: form.provider, config: JSON.stringify(form.config), is_default: form.is_default }
            if (editId) await dnsProviderAPI.update(editId, payload); else await dnsProviderAPI.create(payload)
            setDialogOpen(false); load()
        } catch (e) { setError(e.response?.data?.error || t('common.save_failed')) }
        setSaving(false)
    }

    const handleDelete = async () => {
        try { await dnsProviderAPI.delete(deleteId); setDeleteId(null); load() }
        catch (e) { alert(e.response?.data?.error || t('common.delete_failed')); setDeleteId(null) }
    }

    const setConfigField = (key, value) => setForm({ ...form, config: { ...form.config, [key]: value } })
    const providerDef = PROVIDER_FIELDS[form.provider]

    return (
        <Box mt="4">
            <Flex justify="between" align="center" mb="4">
                <Text size="3" weight="bold">{t('dns.title')}</Text>
                <Button size="2" onClick={openCreate}><Plus size={14} /> {t('dns.add_provider')}</Button>
            </Flex>

            {loading ? <Text color="gray">{t('common.loading')}</Text> : providers.length === 0 ? (
                <Card style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                    <Flex direction="column" align="center" gap="3" py="6">
                        <Shield size={40} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text color="gray">{t('dns.no_providers')}</Text>
                        <Button variant="soft" size="2" onClick={openCreate}><Plus size={14} /> {t('dns.add_first')}</Button>
                    </Flex>
                </Card>
            ) : (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('common.name')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('dns.provider')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('dns.is_default')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell style={{ width: 100 }}>{t('common.actions')}</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {providers.map((p) => (
                                <Table.Row key={p.id}>
                                    <Table.Cell><Flex align="center" gap="2"><Shield size={14} color="#10b981" /><Text weight="medium">{p.name}</Text></Flex></Table.Cell>
                                    <Table.Cell><Badge variant="soft" size="1">{PROVIDER_FIELDS[p.provider]?.label || p.provider}</Badge></Table.Cell>
                                    <Table.Cell>{p.is_default && <Tooltip content={t('dns.default_provider_tooltip')}><Star size={14} color="#f59e0b" fill="#f59e0b" /></Tooltip>}</Table.Cell>
                                    <Table.Cell>
                                        <Flex gap="2">
                                            <Tooltip content={t('common.edit')}><IconButton variant="ghost" size="1" onClick={() => openEdit(p)}><Pencil size={14} /></IconButton></Tooltip>
                                            <Tooltip content={t('common.delete')}><IconButton variant="ghost" size="1" color="red" onClick={() => setDeleteId(p.id)}><Trash2 size={14} /></IconButton></Tooltip>
                                        </Flex>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                </Card>
            )}

            <Dialog.Root open={dialogOpen} onOpenChange={(o) => !o && setDialogOpen(false)}>
                <Dialog.Content maxWidth="480px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>{editId ? t('dns.edit_provider') : t('dns.add_provider')}</Dialog.Title>
                    <Dialog.Description size="2" color="gray" mb="4">{t('dns.dialog_description')}</Dialog.Description>
                    <Flex direction="column" gap="3">
                        {error && <Callout.Root color="red" size="1"><Callout.Icon><AlertCircle size={14} /></Callout.Icon><Callout.Text>{error}</Callout.Text></Callout.Root>}
                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('common.name')}</Text>
                            <TextField.Root placeholder={t('dns.name_placeholder')} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
                        </Flex>
                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('dns.provider_type')}</Text>
                            <Select.Root value={form.provider} onValueChange={(v) => setForm({ ...form, provider: v, config: {} })}>
                                <Select.Trigger /><Select.Content>{Object.entries(PROVIDER_FIELDS).map(([k, v]) => <Select.Item key={k} value={k}>{v.label}</Select.Item>)}</Select.Content>
                            </Select.Root>
                        </Flex>
                        {providerDef && (
                            <Card style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                                <Flex direction="column" gap="2">
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('dns.api_credentials')}</Text>
                                    {providerDef.fields.map((f) => (
                                        <Flex direction="column" gap="1" key={f.key}>
                                            <Text size="1" color="gray">{f.label}</Text>
                                            <TextField.Root type="password" placeholder={f.placeholder} value={form.config[f.key] || ''} onChange={(e) => setConfigField(f.key, e.target.value)} />
                                        </Flex>
                                    ))}
                                </Flex>
                            </Card>
                        )}
                        <Flex justify="between" align="center">
                            <Flex direction="column"><Text size="2" weight="medium">{t('dns.set_default')}</Text><Text size="1" color="gray">{t('dns.set_default_hint')}</Text></Flex>
                            <Switch checked={form.is_default} onCheckedChange={(v) => setForm({ ...form, is_default: v })} />
                        </Flex>
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleSave} disabled={saving || !form.name || !form.provider}>{saving ? t('common.saving') : t('common.save')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            <Dialog.Root open={!!deleteId} onOpenChange={(o) => !o && setDeleteId(null)}>
                <Dialog.Content maxWidth="400px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>{t('dns.confirm_delete_title')}</Dialog.Title>
                    <Dialog.Description size="2" color="gray">{t('dns.confirm_delete_desc')}</Dialog.Description>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button color="red" onClick={handleDelete}>{t('dns.confirm_delete_btn')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}


// ======================== Certificates Tab ========================
function CertificatesTab() {
    const { t } = useTranslation()
    const [certs, setCerts] = useState([])
    const [loading, setLoading] = useState(true)
    const [message, setMessage] = useState(null)
    const [uploadOpen, setUploadOpen] = useState(false)
    const [deleteTarget, setDeleteTarget] = useState(null)
    const [uploadForm, setUploadForm] = useState({ name: '', certFile: null, keyFile: null })
    const [uploading, setUploading] = useState(false)
    const certInputRef = useRef(null)
    const keyInputRef = useRef(null)

    const fetchCerts = async () => { try { const res = await certificateAPI.list(); setCerts(res.data.certificates || []) } catch { /* ignore */ }; setLoading(false) }
    useEffect(() => { fetchCerts() }, [])

    const showMsg = (type, text) => { setMessage({ type, text }); setTimeout(() => setMessage(null), 5000) }

    const handleDelete = async () => {
        if (!deleteTarget) return
        try { await certificateAPI.delete(deleteTarget.id); showMsg('success', t('cert.delete_success')); setDeleteTarget(null); fetchCerts() }
        catch (err) { showMsg('error', err.response?.data?.error || t('common.delete_failed')); setDeleteTarget(null) }
    }

    const handleUpload = async () => {
        if (!uploadForm.name.trim()) { showMsg('error', t('cert.error_no_name')); return }
        if (!uploadForm.certFile) { showMsg('error', t('cert.error_no_cert')); return }
        if (!uploadForm.keyFile) { showMsg('error', t('cert.error_no_key')); return }
        setUploading(true)
        try {
            const formData = new FormData()
            formData.append('name', uploadForm.name.trim())
            formData.append('cert', uploadForm.certFile)
            formData.append('key', uploadForm.keyFile)
            await certificateAPI.upload(formData)
            setUploadForm({ name: '', certFile: null, keyFile: null })
            setUploadOpen(false); fetchCerts(); showMsg('success', t('common.save_success'))
        } catch (err) { showMsg('error', err.response?.data?.error || t('common.operation_failed')) }
        setUploading(false)
    }

    const formatDate = (d) => {
        if (!d) return '-'
        const date = new Date(d); const now = new Date(); const days = Math.floor((date - now) / 86400000); const str = date.toLocaleDateString()
        if (days < 0) return <Badge color="red" size="1">{t('cert.expired')} {str}</Badge>
        if (days < 30) return <Badge color="orange" size="1">{str} ({days} {t('common.days')})</Badge>
        return <Badge color="green" size="1" variant="soft">{str} ({days} {t('common.days')})</Badge>
    }

    return (
        <Box mt="4">
            <Flex justify="between" align="center" mb="4">
                <Text size="3" weight="bold">{t('cert.title')}</Text>
                <Button size="2" onClick={() => setUploadOpen(true)}><Plus size={14} /> {t('cert.upload')}</Button>
            </Flex>

            {message && (
                <Callout.Root color={message.type === 'success' ? 'green' : 'red'} size="1" mb="4">
                    <Callout.Icon>{message.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}</Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                {certs.length === 0 ? (
                    <Flex direction="column" align="center" justify="center" py="8">
                        <ShieldCheck size={40} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="2" color="gray" mt="3">{loading ? t('common.loading') : t('cert.no_certs_hint')}</Text>
                    </Flex>
                ) : (
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('common.name')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('cert.domain')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('cert.expires')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('cert.linked_hosts')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell width="60"></Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {certs.map((cert) => (
                                <Table.Row key={cert.id}>
                                    <Table.Cell><Flex align="center" gap="2"><ShieldCheck size={14} color="#10b981" /><Text size="2">{cert.name}</Text></Flex></Table.Cell>
                                    <Table.Cell><Text size="1" style={{ fontFamily: 'monospace', color: 'var(--cp-text-secondary)' }}>{cert.domains || '-'}</Text></Table.Cell>
                                    <Table.Cell>{formatDate(cert.expires_at)}</Table.Cell>
                                    <Table.Cell><Badge variant="soft" size="1">{t('common.host_count', { count: cert.host_count || 0 })}</Badge></Table.Cell>
                                    <Table.Cell><IconButton size="1" variant="ghost" color="red" onClick={() => setDeleteTarget(cert)}><Trash2 size={14} /></IconButton></Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                )}
            </Card>

            {/* Upload Dialog */}
            <Dialog.Root open={uploadOpen} onOpenChange={(o) => !o && setUploadOpen(false)}>
                <Dialog.Content maxWidth="480px">
                    <Dialog.Title>{t('cert.upload')}</Dialog.Title>
                    <Dialog.Description size="2" color="gray">{t('cert.upload_description')}</Dialog.Description>
                    <Flex direction="column" gap="3" mt="4">
                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('cert.name')}</Text>
                            <TextField.Root placeholder={t('cert.name_placeholder')} value={uploadForm.name} onChange={(e) => setUploadForm({ ...uploadForm, name: e.target.value })} />
                        </Flex>
                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('cert.cert_file')}</Text>
                            <Button variant="soft" color="gray" size="2" onClick={() => certInputRef.current?.click()}>
                                <Upload size={14} /> {uploadForm.certFile ? uploadForm.certFile.name : t('cert.choose_cert')}
                            </Button>
                            <input ref={certInputRef} type="file" accept=".pem,.crt,.cer" onChange={(e) => setUploadForm({ ...uploadForm, certFile: e.target.files?.[0] || null })} style={{ display: 'none' }} />
                        </Flex>
                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('cert.key_file')}</Text>
                            <Button variant="soft" color="gray" size="2" onClick={() => keyInputRef.current?.click()}>
                                <Upload size={14} /> {uploadForm.keyFile ? uploadForm.keyFile.name : t('cert.choose_key')}
                            </Button>
                            <input ref={keyInputRef} type="file" accept=".pem,.key" onChange={(e) => setUploadForm({ ...uploadForm, keyFile: e.target.files?.[0] || null })} style={{ display: 'none' }} />
                        </Flex>
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleUpload} disabled={uploading}>{uploading ? t('common.uploading') : t('common.upload')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Delete Confirmation */}
            <AlertDialog.Root open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
                <AlertDialog.Content maxWidth="400px">
                    <AlertDialog.Title>{t('cert.delete_title')}</AlertDialog.Title>
                    <AlertDialog.Description dangerouslySetInnerHTML={{ __html: t('cert.confirm_delete_desc', { name: deleteTarget?.name }) }} />
                    <Flex gap="3" mt="4" justify="end">
                        <AlertDialog.Cancel><Button variant="soft" color="gray">{t('common.cancel')}</Button></AlertDialog.Cancel>
                        <AlertDialog.Action><Button color="red" onClick={handleDelete}>{t('common.delete')}</Button></AlertDialog.Action>
                    </Flex>
                </AlertDialog.Content>
            </AlertDialog.Root>
        </Box>
    )
}


// ======================== Plugins Tab ========================
function PluginsTab() {
    const { t } = useTranslation()
    const categoryColors = { deploy: 'blue', database: 'green', tool: 'orange', monitor: 'purple' }
    const [plugins, setPlugins] = useState([])
    const [loading, setLoading] = useState(true)
    const [toggling, setToggling] = useState(null)

    const fetchPlugins = async () => { try { const res = await pluginAPI.list(); setPlugins(res.data?.plugins || []) } catch { /* ignore */ } finally { setLoading(false) } }
    useEffect(() => { fetchPlugins() }, [])

    const handleToggle = async (id, currentEnabled) => {
        setToggling(id)
        try { if (currentEnabled) await pluginAPI.disable(id); else await pluginAPI.enable(id); await fetchPlugins() }
        catch { /* ignore */ }
        finally { setToggling(null) }
    }

    if (loading) return <Flex align="center" justify="center" style={{ minHeight: 200 }}><RefreshCw size={20} className="spin" /><Text ml="2">{t('common.loading')}</Text></Flex>

    return (
        <Box mt="4">
            <Flex align="center" justify="between" mb="4">
                <Text size="3" weight="bold">{t('plugins.title')}</Text>
                <Badge variant="soft" size="2">{plugins.length} {t('plugins.installed')}</Badge>
            </Flex>
            <Text size="2" color="gray" mb="4" style={{ display: 'block' }}>{t('plugins.description')}</Text>
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
                            <Flex align="center" justify="between">
                                <Flex direction="column" gap="1" style={{ flex: 1 }}>
                                    <Flex align="center" gap="2">
                                        <Text weight="bold" size="3">{p.name}</Text>
                                        <Badge variant="soft" size="1">v{p.version}</Badge>
                                        {p.category && <Badge color={categoryColors[p.category] || 'gray'} variant="soft" size="1">{p.category}</Badge>}
                                    </Flex>
                                    <Text size="2" color="gray">{p.description}</Text>
                                    {p.dependencies?.length > 0 && <Text size="1" color="gray">{t('plugins.depends_on')}: {p.dependencies.join(', ')}</Text>}
                                </Flex>
                                <Switch checked={p.enabled} disabled={toggling === p.id} onCheckedChange={() => handleToggle(p.id, p.enabled)} />
                            </Flex>
                        </Card>
                    ))}
                </Flex>
            )}
        </Box>
    )
}


// ======================== API Tokens Tab ========================
function APITokensTab() {
    const { t } = useTranslation()
    const [tokens, setTokens] = useState([])
    const [loading, setLoading] = useState(true)
    const [createOpen, setCreateOpen] = useState(false)
    const [form, setForm] = useState({ name: '', expires_in: 0 })
    const [creating, setCreating] = useState(false)
    const [newToken, setNewToken] = useState(null) // plaintext shown once
    const [copied, setCopied] = useState(false)
    const [deleteTarget, setDeleteTarget] = useState(null)
    const [message, setMessage] = useState(null)

    const showMsg = (type, text) => { setMessage({ type, text }); setTimeout(() => setMessage(null), 5000) }

    const fetchTokens = async () => {
        try { const res = await mcpAPI.listTokens(); setTokens(res.data.tokens || []) }
        catch { /* ignore */ }
        finally { setLoading(false) }
    }

    useEffect(() => { fetchTokens() }, [])

    const handleCreate = async () => {
        if (!form.name.trim()) return
        setCreating(true)
        try {
            const res = await mcpAPI.createToken({ name: form.name.trim(), permissions: [], expires_in: form.expires_in })
            setNewToken(res.data.token)
            setCopied(false)
            setForm({ name: '', expires_in: 0 })
            fetchTokens()
        } catch (err) {
            showMsg('error', err.response?.data?.error || t('common.operation_failed'))
            setCreateOpen(false)
        } finally { setCreating(false) }
    }

    const handleDelete = async () => {
        if (!deleteTarget) return
        try { await mcpAPI.deleteToken(deleteTarget.id); showMsg('success', t('mcp.token_revoked')); setDeleteTarget(null); fetchTokens() }
        catch (err) { showMsg('error', err.response?.data?.error || t('common.delete_failed')); setDeleteTarget(null) }
    }

    const handleCopy = (text) => {
        navigator.clipboard.writeText(text)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
    }

    const mcpEndpoint = `${window.location.origin}/api/plugins/mcpserver/mcp`

    return (
        <Box mt="4">
            {message && (
                <Callout.Root color={message.type === 'success' ? 'green' : 'red'} size="1" mb="4">
                    <Callout.Icon>{message.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}</Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Flex justify="between" align="center" mb="4">
                <Flex direction="column" gap="1">
                    <Text size="3" weight="bold">{t('mcp.title')}</Text>
                    <Text size="2" color="gray">{t('mcp.subtitle')}</Text>
                </Flex>
                <Button size="2" onClick={() => { setCreateOpen(true); setNewToken(null) }}>
                    <Plus size={14} /> {t('mcp.create_token')}
                </Button>
            </Flex>

            {/* MCP Endpoint Info */}
            <Card mb="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Flex direction="column" gap="3">
                    <Flex align="center" gap="2">
                        <Cpu size={16} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="2" weight="bold">{t('mcp.mcp_endpoint')}</Text>
                    </Flex>
                    <Flex align="center" gap="2">
                        <Code size="2" style={{ flex: 1, fontFamily: 'monospace', padding: '8px 12px', background: 'var(--cp-code-bg)', borderRadius: 6 }}>
                            {mcpEndpoint}
                        </Code>
                        <Tooltip content={t('common.copy')}>
                            <IconButton size="1" variant="ghost" onClick={() => { navigator.clipboard.writeText(mcpEndpoint) }}>
                                <Copy size={14} />
                            </IconButton>
                        </Tooltip>
                    </Flex>
                    <Text size="1" color="gray">{t('mcp.ide_config')}</Text>
                    <Box style={{ background: 'var(--cp-code-bg)', border: '1px solid var(--cp-border)', borderRadius: 8, padding: 12 }}>
                        <pre className="log-viewer" style={{ margin: 0, fontSize: 12, color: 'var(--cp-text)' }}>{`{
  "mcpServers": {
    "webcasa": {
      "type": "streamable-http",
      "url": "${mcpEndpoint}",
      "headers": {
        "Authorization": "Bearer wc_YOUR_TOKEN_HERE"
      }
    }
  }
}`}</pre>
                    </Box>
                </Flex>
            </Card>

            {/* Token List */}
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                {loading ? (
                    <Flex justify="center" p="6"><Spinner size="3" /></Flex>
                ) : tokens.length === 0 ? (
                    <Flex direction="column" align="center" justify="center" py="8">
                        <Key size={40} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="2" color="gray" mt="3">{t('mcp.no_tokens')}</Text>
                    </Flex>
                ) : (
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('mcp.token_name')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('mcp.token_prefix')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('mcp.created_at')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('mcp.last_used')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('mcp.expires')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell width="60"></Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {tokens.map((token) => (
                                <Table.Row key={token.id}>
                                    <Table.Cell><Flex align="center" gap="2"><Key size={14} style={{ color: 'var(--cp-text-muted)' }} /><Text weight="medium">{token.name}</Text></Flex></Table.Cell>
                                    <Table.Cell><Code size="1" style={{ fontFamily: 'monospace' }}>{token.prefix}...</Code></Table.Cell>
                                    <Table.Cell><Text size="1" color="gray">{new Date(token.created_at).toLocaleDateString()}</Text></Table.Cell>
                                    <Table.Cell><Text size="1" color="gray">{token.last_used_at ? new Date(token.last_used_at).toLocaleDateString() : t('mcp.never_used')}</Text></Table.Cell>
                                    <Table.Cell>
                                        {token.expires_at
                                            ? <Badge color={new Date(token.expires_at) < new Date() ? 'red' : 'gray'} size="1" variant="soft">
                                                {new Date(token.expires_at).toLocaleDateString()}
                                              </Badge>
                                            : <Badge color="green" size="1" variant="soft">{t('mcp.no_expiry')}</Badge>
                                        }
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Tooltip content={t('mcp.revoke_token')}>
                                            <IconButton size="1" variant="ghost" color="red" onClick={() => setDeleteTarget(token)}>
                                                <Trash2 size={14} />
                                            </IconButton>
                                        </Tooltip>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                )}
            </Card>

            {/* Create Token Dialog */}
            <Dialog.Root open={createOpen} onOpenChange={(o) => { if (!o) { setCreateOpen(false); setNewToken(null) } }}>
                <Dialog.Content maxWidth="500px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>{t('mcp.create_token')}</Dialog.Title>

                    {newToken ? (
                        <Flex direction="column" gap="3" mt="3">
                            <Callout.Root color="orange" size="1">
                                <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                                <Callout.Text>{t('mcp.token_created_warning')}</Callout.Text>
                            </Callout.Root>
                            <Text size="2" weight="medium">{t('mcp.your_token')}:</Text>
                            <Flex align="center" gap="2">
                                <Code size="2" style={{ flex: 1, fontFamily: 'monospace', padding: '10px 14px', background: 'var(--cp-code-bg)', borderRadius: 6, wordBreak: 'break-all' }}>
                                    {newToken}
                                </Code>
                                <Button variant="soft" size="2" onClick={() => handleCopy(newToken)}>
                                    {copied ? <><Check size={14} /> {t('common.copied')}</> : <><Copy size={14} /> {t('common.copy')}</>}
                                </Button>
                            </Flex>
                            <Flex justify="end" mt="2">
                                <Button onClick={() => { setCreateOpen(false); setNewToken(null) }}>{t('common.close')}</Button>
                            </Flex>
                        </Flex>
                    ) : (
                        <Flex direction="column" gap="3" mt="3">
                            <Flex direction="column" gap="1">
                                <Text size="2" weight="medium">{t('mcp.token_name')}</Text>
                                <TextField.Root placeholder={t('mcp.token_name_placeholder')} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
                            </Flex>
                            <Flex direction="column" gap="1">
                                <Text size="2" weight="medium">{t('mcp.expires_in')}</Text>
                                <Select.Root value={String(form.expires_in)} onValueChange={(v) => setForm({ ...form, expires_in: Number(v) })}>
                                    <Select.Trigger />
                                    <Select.Content>
                                        <Select.Item value="0">{t('mcp.no_expiry')}</Select.Item>
                                        <Select.Item value="30">{t('mcp.days_30')}</Select.Item>
                                        <Select.Item value="90">{t('mcp.days_90')}</Select.Item>
                                        <Select.Item value="180">{t('mcp.days_180')}</Select.Item>
                                        <Select.Item value="365">{t('mcp.days_365')}</Select.Item>
                                    </Select.Content>
                                </Select.Root>
                            </Flex>
                            <Flex gap="3" mt="2" justify="end">
                                <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                                <Button onClick={handleCreate} disabled={creating || !form.name.trim()}>
                                    {creating ? t('common.creating') : t('common.create')}
                                </Button>
                            </Flex>
                        </Flex>
                    )}
                </Dialog.Content>
            </Dialog.Root>

            {/* Revoke Confirmation */}
            <AlertDialog.Root open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
                <AlertDialog.Content maxWidth="400px">
                    <AlertDialog.Title>{t('mcp.revoke_title')}</AlertDialog.Title>
                    <AlertDialog.Description>{t('mcp.revoke_confirm', { name: deleteTarget?.name })}</AlertDialog.Description>
                    <Flex gap="3" mt="4" justify="end">
                        <AlertDialog.Cancel><Button variant="soft" color="gray">{t('common.cancel')}</Button></AlertDialog.Cancel>
                        <AlertDialog.Action><Button color="red" onClick={handleDelete}>{t('mcp.revoke_token')}</Button></AlertDialog.Action>
                    </Flex>
                </AlertDialog.Content>
            </AlertDialog.Root>
        </Box>
    )
}

// ======================== Notify Tab ========================
const EVENT_OPTIONS = [
    { value: 'deploy.*', label: 'Deploy Events' },
    { value: 'deploy.build.failed', label: 'Build Failed' },
    { value: 'deploy.build.success', label: 'Build Success' },
    { value: 'backup.*', label: 'Backup Events' },
    { value: 'monitoring.alert.*', label: 'Monitoring Alerts' },
    { value: '*', label: 'All Events' },
]

function NotifyConfigForm({ type, config, onChange, t }) {
    let parsed = {}
    try { parsed = JSON.parse(config) } catch {}

    const update = (key, value) => {
        const next = { ...parsed, [key]: value }
        onChange(JSON.stringify(next, null, 2))
    }

    switch (type) {
        case 'webhook':
            return (
                <Flex direction="column" gap="2">
                    <label>
                        <Text size="2" weight="medium">Webhook URL</Text>
                        <TextField.Root placeholder="https://example.com/webhook" value={parsed.url || ''} onChange={e => update('url', e.target.value)} />
                    </label>
                    <label>
                        <Text size="2" weight="medium">{t('notify.custom_headers')}</Text>
                        <Text size="1" color="gray">{t('notify.headers_hint')}</Text>
                        <TextArea rows={2} value={parsed.headers ? JSON.stringify(parsed.headers, null, 2) : ''} onChange={e => {
                            try { update('headers', JSON.parse(e.target.value)) } catch {}
                        }} style={{ fontFamily: 'monospace', fontSize: 12 }} />
                    </label>
                </Flex>
            )
        case 'email':
            return (
                <Flex direction="column" gap="2">
                    <Flex gap="2">
                        <Box style={{ flex: 1 }}><label><Text size="2" weight="medium">SMTP Host</Text><TextField.Root placeholder="smtp.gmail.com" value={parsed.smtp_host || ''} onChange={e => update('smtp_host', e.target.value)} /></label></Box>
                        <Box style={{ width: 80 }}><label><Text size="2" weight="medium">Port</Text><TextField.Root type="number" value={parsed.smtp_port || 587} onChange={e => update('smtp_port', parseInt(e.target.value) || 587)} /></label></Box>
                    </Flex>
                    <Flex gap="2">
                        <Box style={{ flex: 1 }}><label><Text size="2" weight="medium">{t('common.username')}</Text><TextField.Root value={parsed.username || ''} onChange={e => update('username', e.target.value)} /></label></Box>
                        <Box style={{ flex: 1 }}><label><Text size="2" weight="medium">{t('common.password')}</Text><TextField.Root type="password" value={parsed.password || ''} onChange={e => update('password', e.target.value)} /></label></Box>
                    </Flex>
                    <Flex gap="2">
                        <Box style={{ flex: 1 }}><label><Text size="2" weight="medium">From</Text><TextField.Root placeholder="noreply@example.com" value={parsed.from || ''} onChange={e => update('from', e.target.value)} /></label></Box>
                        <Box style={{ flex: 1 }}><label><Text size="2" weight="medium">To</Text><TextField.Root placeholder="admin@example.com" value={parsed.to || ''} onChange={e => update('to', e.target.value)} /></label></Box>
                    </Flex>
                    <Flex align="center" gap="2"><Switch size="1" checked={parsed.use_tls !== false} onCheckedChange={v => update('use_tls', v)} /><Text size="2">TLS</Text></Flex>
                </Flex>
            )
        case 'discord':
            return (
                <Flex direction="column" gap="2">
                    <label>
                        <Text size="2" weight="medium">Discord Webhook URL</Text>
                        <Text size="1" color="gray">{t('notify.discord_hint')}</Text>
                        <TextField.Root placeholder="https://discord.com/api/webhooks/..." value={parsed.webhook_url || ''} onChange={e => update('webhook_url', e.target.value)} />
                    </label>
                </Flex>
            )
        case 'telegram':
            return (
                <Flex direction="column" gap="2">
                    <label>
                        <Text size="2" weight="medium">Bot Token</Text>
                        <Text size="1" color="gray">{t('notify.telegram_token_hint')}</Text>
                        <TextField.Root placeholder="123456:ABC-DEF..." value={parsed.bot_token || ''} onChange={e => update('bot_token', e.target.value)} />
                    </label>
                    <label>
                        <Text size="2" weight="medium">Chat ID</Text>
                        <Text size="1" color="gray">{t('notify.telegram_chat_hint')}</Text>
                        <TextField.Root placeholder="-1001234567890" value={parsed.chat_id || ''} onChange={e => update('chat_id', e.target.value)} />
                    </label>
                </Flex>
            )
        default:
            return (
                <label>
                    <Text size="2" weight="medium">{t('notify.config')}</Text>
                    <TextArea rows={6} value={config} onChange={e => onChange(e.target.value)} style={{ fontFamily: 'monospace', fontSize: 12 }} />
                </label>
            )
    }
}

function NotifyTab({ showMessage }) {
    const { t } = useTranslation()
    const [channels, setChannels] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editChannel, setEditChannel] = useState(null)
    const [testing, setTesting] = useState(null)

    const [form, setForm] = useState({
        name: '', type: 'webhook', config: '', events: '["*"]', enabled: true,
    })

    const fetchChannels = async () => {
        try {
            const res = await notifyAPI.listChannels()
            setChannels(res.data || [])
        } catch (e) { console.error(e) }
        finally { setLoading(false) }
    }

    useEffect(() => { fetchChannels() }, [])

    const getDefaultConfig = (type) => {
        switch (type) {
            case 'webhook': return JSON.stringify({ url: '' }, null, 2)
            case 'email': return JSON.stringify({ smtp_host: '', smtp_port: 587, username: '', password: '', from: '', to: '', use_tls: true }, null, 2)
            case 'discord': return JSON.stringify({ webhook_url: '' }, null, 2)
            case 'telegram': return JSON.stringify({ bot_token: '', chat_id: '' }, null, 2)
            default: return '{}'
        }
    }

    const openCreate = () => {
        setEditChannel(null)
        setForm({ name: '', type: 'webhook', config: getDefaultConfig('webhook'), events: '["*"]', enabled: true })
        setDialogOpen(true)
    }

    const openEdit = (ch) => {
        setEditChannel(ch)
        setForm({ name: ch.name, type: ch.type, config: ch.config || '', events: ch.events || '["*"]', enabled: ch.enabled })
        setDialogOpen(true)
    }

    const handleSave = async () => {
        try { JSON.parse(form.config); JSON.parse(form.events) }
        catch { showMessage('error', t('notify.invalid_json')); return }

        try {
            if (editChannel) await notifyAPI.updateChannel(editChannel.id, form)
            else await notifyAPI.createChannel(form)
            setDialogOpen(false)
            fetchChannels()
            showMessage('success', t('common.saved'))
        } catch (e) { showMessage('error', e.response?.data?.error || t('common.operation_failed')) }
    }

    const handleDelete = async (id) => {
        if (!confirm(t('notify.confirm_delete'))) return
        try { await notifyAPI.deleteChannel(id); fetchChannels() }
        catch (e) { showMessage('error', e.response?.data?.error || t('common.operation_failed')) }
    }

    const handleTest = async (id) => {
        setTesting(id)
        try { await notifyAPI.testChannel(id); showMessage('success', t('notify.test_sent')) }
        catch (e) { showMessage('error', e.response?.data?.error || t('notify.test_failed')) }
        finally { setTesting(null) }
    }

    const handleToggle = async (id, enabled) => {
        try { await notifyAPI.updateChannel(id, { enabled }); fetchChannels() }
        catch (e) { console.error(e) }
    }

    if (loading) return <Text color="gray">{t('common.loading')}</Text>

    return (
        <Box>
            <Flex justify="between" align="center" mb="4" mt="4">
                <Box>
                    <Heading size="4">{t('notify.title')}</Heading>
                    <Text size="2" color="gray">{t('notify.subtitle')}</Text>
                </Box>
                <Button onClick={openCreate}><Plus size={14} /> {t('notify.add_channel')}</Button>
            </Flex>

            {channels.length === 0 ? (
                <Card><Text size="2" color="gray" style={{ textAlign: 'center', padding: '2rem 0' }}>{t('notify.no_channels')}</Text></Card>
            ) : (
                <Table.Root variant="surface">
                    <Table.Header>
                        <Table.Row>
                            <Table.ColumnHeaderCell>{t('common.name')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('notify.type')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('notify.events')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                        </Table.Row>
                    </Table.Header>
                    <Table.Body>
                        {channels.map(ch => (
                            <Table.Row key={ch.id}>
                                <Table.Cell><Text weight="medium">{ch.name}</Text></Table.Cell>
                                <Table.Cell><Badge variant="soft">{ch.type}</Badge></Table.Cell>
                                <Table.Cell><Text size="1" color="gray" style={{ fontFamily: 'monospace' }}>{ch.events || '*'}</Text></Table.Cell>
                                <Table.Cell><Switch size="1" checked={ch.enabled} onCheckedChange={v => handleToggle(ch.id, v)} /></Table.Cell>
                                <Table.Cell>
                                    <Flex gap="1">
                                        <Tooltip content={t('notify.test')}><IconButton size="1" variant="ghost" onClick={() => handleTest(ch.id)} disabled={testing === ch.id}>{testing === ch.id ? <Spinner size="1" /> : <TestTube size={14} />}</IconButton></Tooltip>
                                        <Tooltip content={t('common.edit')}><IconButton size="1" variant="ghost" onClick={() => openEdit(ch)}><Pencil size={14} /></IconButton></Tooltip>
                                        <Tooltip content={t('common.delete')}><IconButton size="1" variant="ghost" color="red" onClick={() => handleDelete(ch.id)}><Trash2 size={14} /></IconButton></Tooltip>
                                    </Flex>
                                </Table.Cell>
                            </Table.Row>
                        ))}
                    </Table.Body>
                </Table.Root>
            )}

            <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
                <Dialog.Content maxWidth="500px">
                    <Dialog.Title>{editChannel ? t('notify.edit_channel') : t('notify.add_channel')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        <label>
                            <Text size="2" weight="medium">{t('common.name')}</Text>
                            <TextField.Root placeholder="My Webhook" value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} />
                        </label>
                        <label>
                            <Text size="2" weight="medium">{t('notify.type')}</Text>
                            <Select.Root value={form.type} onValueChange={v => {
                                setForm(f => ({ ...f, type: v, config: getDefaultConfig(v) }))
                            }}>
                                <Select.Trigger />
                                <Select.Content>
                                    <Select.Item value="webhook">Webhook</Select.Item>
                                    <Select.Item value="email">Email (SMTP)</Select.Item>
                                    <Select.Item value="discord">Discord</Select.Item>
                                    <Select.Item value="telegram">Telegram</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </label>
                        <NotifyConfigForm type={form.type} config={form.config} onChange={config => setForm(f => ({ ...f, config }))} t={t} />
                        <label>
                            <Text size="2" weight="medium">{t('notify.events')}</Text>
                            <Text size="1" color="gray">{t('notify.events_hint')}</Text>
                            <TextArea rows={2} value={form.events} onChange={e => setForm(f => ({ ...f, events: e.target.value }))} style={{ fontFamily: 'monospace', fontSize: 12 }} />
                        </label>
                        <Flex wrap="wrap" gap="1">
                            {EVENT_OPTIONS.map(opt => (
                                <Badge key={opt.value} variant="soft" style={{ cursor: 'pointer' }} onClick={() => {
                                    try {
                                        const cur = JSON.parse(form.events || '[]')
                                        if (!cur.includes(opt.value)) setForm(f => ({ ...f, events: JSON.stringify([...cur, opt.value]) }))
                                    } catch {}
                                }}>+ {opt.label}</Badge>
                            ))}
                        </Flex>
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleSave}>{t('common.save')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
