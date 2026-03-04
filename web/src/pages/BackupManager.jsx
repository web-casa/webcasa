import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Text, Card, Badge, Button, Table, Dialog, TextField, Select, Switch, Tabs, TextArea } from '@radix-ui/themes'
import { HardDrive, Play, RotateCcw, Trash2, Settings, Clock, CheckCircle, XCircle, AlertCircle, Download, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { backupAPI } from '../api/index.js'

function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return (bytes / Math.pow(1024, i)).toFixed(1) + ' ' + units[i]
}

function formatDate(timestamp) {
    if (!timestamp) return '-'
    return new Date(timestamp).toLocaleString()
}

export default function BackupManager({ embedded }) {
    const { t } = useTranslation()

    const [kopiaStatus, setKopiaStatus] = useState(null)
    const [config, setConfig] = useState({
        target_type: 'local',
        target: {},
        encryption_password: '',
        schedule_enabled: false,
        schedule_cron: '',
        retain_count: 10,
        retain_days: 30,
        scopes: { panel: true, docker: false, database: false },
    })
    const [status, setStatus] = useState({ state: 'idle', last_backup: null, last_status: '', next_run: null })
    const [snapshots, setSnapshots] = useState([])
    const [logs, setLogs] = useState([])
    const [logFilter, setLogFilter] = useState('')

    const [saving, setSaving] = useState(false)
    const [testing, setTesting] = useState(false)
    const [backingUp, setBackingUp] = useState(false)
    const [restoreId, setRestoreId] = useState(null)
    const [restoring, setRestoring] = useState(false)

    // Kopia install state
    const [installing, setInstalling] = useState(false)
    const [installLogs, setInstallLogs] = useState([])
    const [installDone, setInstallDone] = useState(false)
    const [installError, setInstallError] = useState(false)
    const installLogsEndRef = useRef(null)

    const fetchAll = useCallback(async () => {
        try {
            const depRes = await backupAPI.checkDependency()
            setKopiaStatus(depRes.data)
        } catch { setKopiaStatus({ available: false }) }

        const [cfgRes, statusRes, snapRes, logRes] = await Promise.allSettled([
            backupAPI.getConfig(),
            backupAPI.getStatus(),
            backupAPI.listSnapshots(),
            backupAPI.listLogs(),
        ])
        if (cfgRes.status === 'fulfilled' && cfgRes.value.data) {
            const d = cfgRes.value.data
            setConfig({
                target_type: d.target_type || 'local',
                target: d.target || {},
                encryption_password: d.encryption_password || '',
                schedule_enabled: !!d.schedule_enabled,
                schedule_cron: d.schedule_cron || '',
                retain_count: d.retain_count ?? 10,
                retain_days: d.retain_days ?? 30,
                scopes: d.scopes || { panel: true, docker: false, database: false },
            })
        }
        if (statusRes.status === 'fulfilled' && statusRes.value.data) setStatus(statusRes.value.data)
        if (snapRes.status === 'fulfilled') setSnapshots(snapRes.value.data?.snapshots || [])
        if (logRes.status === 'fulfilled') setLogs(logRes.value.data?.logs || [])
    }, [])

    useEffect(() => { fetchAll() }, [fetchAll])

    useEffect(() => {
        if (installLogsEndRef.current) {
            installLogsEndRef.current.scrollIntoView({ behavior: 'smooth' })
        }
    }, [installLogs])

    const handleInstallKopia = () => {
        setInstalling(true)
        setInstallLogs([])
        setInstallDone(false)
        setInstallError(false)

        const token = localStorage.getItem('token')
        fetch('/api/plugins/backup/install-kopia', {
            method: 'POST',
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
                        setInstallLogs(prev => [...prev, line.slice(6)])
                    } else if (line.startsWith('event: done')) {
                        setInstallDone(true)
                    } else if (line.startsWith('event: error')) {
                        setInstallError(true)
                    }
                }
            }

            if (buffer) {
                for (const line of buffer.split('\n')) {
                    if (line.startsWith('data: ')) {
                        setInstallLogs(prev => [...prev, line.slice(6)])
                    }
                }
            }

            setInstallDone(prev => prev || true)
        }).catch((err) => {
            setInstallLogs(prev => [...prev, `ERROR: ${err.message}`])
            setInstallError(true)
        }).finally(() => {
            setInstalling(false)
        })
    }

    const handleBackupNow = async () => {
        setBackingUp(true)
        try {
            await backupAPI.createSnapshot()
            await fetchAll()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setBackingUp(false) }
    }

    const handleSave = async () => {
        setSaving(true)
        try {
            await backupAPI.updateConfig(config)
            await fetchAll()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setSaving(false) }
    }

    const handleTest = async () => {
        setTesting(true)
        try {
            const res = await backupAPI.testConnection()
            alert(res.data?.message || 'Connection OK')
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setTesting(false) }
    }

    const handleRestore = async () => {
        if (!restoreId) return
        setRestoring(true)
        try {
            await backupAPI.restoreSnapshot(restoreId)
            setRestoreId(null)
            await fetchAll()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setRestoring(false) }
    }

    const handleDelete = async (id) => {
        if (!window.confirm(t('backup.confirm_delete'))) return
        try {
            await backupAPI.deleteSnapshot(id)
            await fetchAll()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        }
    }

    const updateTarget = (key, value) => {
        setConfig(prev => ({ ...prev, target: { ...prev.target, [key]: value } }))
    }

    const updateScope = (key, value) => {
        setConfig(prev => ({ ...prev, scopes: { ...prev.scopes, [key]: value } }))
    }

    const statusBadge = (s) => {
        if (s === 'completed') return <Badge color="green" variant="soft"><CheckCircle size={12} /> {s}</Badge>
        if (s === 'failed') return <Badge color="red" variant="soft"><XCircle size={12} /> {s}</Badge>
        if (s === 'running') return <Badge color="yellow" variant="soft"><Clock size={12} /> {s}</Badge>
        return <Badge color="gray" variant="soft">{s || '-'}</Badge>
    }

    const logLevelBadge = (level) => {
        if (level === 'error') return <Badge color="red" variant="soft">{level}</Badge>
        if (level === 'warn') return <Badge color="orange" variant="soft">{level}</Badge>
        if (level === 'info') return <Badge color="blue" variant="soft">{level}</Badge>
        return <Badge color="gray" variant="soft">{level}</Badge>
    }

    const filteredLogs = logFilter
        ? logs.filter(l => l.snapshot_id === logFilter)
        : logs

    // Dynamic target fields based on target_type
    const renderTargetFields = () => {
        const type = config.target_type
        if (type === 'local') {
            return (
                <Box>
                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.local_path')}</Text>
                    <TextField.Root
                        placeholder="/var/backups/webcasa"
                        value={config.target.local_path || ''}
                        onChange={e => updateTarget('local_path', e.target.value)}
                    />
                </Box>
            )
        }
        if (type === 's3') {
            return (
                <Flex direction="column" gap="3">
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.s3_endpoint')}</Text>
                        <TextField.Root placeholder="https://s3.amazonaws.com" value={config.target.endpoint || ''} onChange={e => updateTarget('endpoint', e.target.value)} />
                    </Box>
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.s3_bucket')}</Text>
                        <TextField.Root placeholder="my-backup-bucket" value={config.target.bucket || ''} onChange={e => updateTarget('bucket', e.target.value)} />
                    </Box>
                    <Flex gap="3">
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.s3_access_key')}</Text>
                            <TextField.Root value={config.target.access_key || ''} onChange={e => updateTarget('access_key', e.target.value)} />
                        </Box>
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.s3_secret_key')}</Text>
                            <TextField.Root type="password" value={config.target.secret_key || ''} onChange={e => updateTarget('secret_key', e.target.value)} />
                        </Box>
                    </Flex>
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.s3_region')}</Text>
                        <TextField.Root placeholder="us-east-1" value={config.target.region || ''} onChange={e => updateTarget('region', e.target.value)} />
                    </Box>
                </Flex>
            )
        }
        if (type === 'webdav') {
            return (
                <Flex direction="column" gap="3">
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.webdav_url')}</Text>
                        <TextField.Root placeholder="https://dav.example.com/backup" value={config.target.url || ''} onChange={e => updateTarget('url', e.target.value)} />
                    </Box>
                    <Flex gap="3">
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.webdav_user')}</Text>
                            <TextField.Root value={config.target.user || ''} onChange={e => updateTarget('user', e.target.value)} />
                        </Box>
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.webdav_password')}</Text>
                            <TextField.Root type="password" value={config.target.password || ''} onChange={e => updateTarget('password', e.target.value)} />
                        </Box>
                    </Flex>
                </Flex>
            )
        }
        if (type === 'sftp') {
            return (
                <Flex direction="column" gap="3">
                    <Flex gap="3">
                        <Box style={{ flex: 2 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.sftp_host')}</Text>
                            <TextField.Root placeholder="backup.example.com" value={config.target.host || ''} onChange={e => updateTarget('host', e.target.value)} />
                        </Box>
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.sftp_port')}</Text>
                            <TextField.Root type="number" placeholder="22" value={config.target.port || ''} onChange={e => updateTarget('port', e.target.value)} />
                        </Box>
                    </Flex>
                    <Flex gap="3">
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.sftp_user')}</Text>
                            <TextField.Root value={config.target.user || ''} onChange={e => updateTarget('user', e.target.value)} />
                        </Box>
                        <Box style={{ flex: 1 }}>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.sftp_password')}</Text>
                            <TextField.Root type="password" value={config.target.password || ''} onChange={e => updateTarget('password', e.target.value)} />
                        </Box>
                    </Flex>
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.sftp_key_path')}</Text>
                        <TextField.Root placeholder="~/.ssh/id_rsa" value={config.target.key_path || ''} onChange={e => updateTarget('key_path', e.target.value)} />
                    </Box>
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.sftp_path')}</Text>
                        <TextField.Root placeholder="/backups/webcasa" value={config.target.path || ''} onChange={e => updateTarget('path', e.target.value)} />
                    </Box>
                </Flex>
            )
        }
        return null
    }

    return (
        <Box>
            {/* Header */}
            <Flex align="center" justify="between" mb="4">
                {!embedded && (
                    <Box>
                        <Flex align="center" gap="2" mb="1">
                            <HardDrive size={24} />
                            <Text size="5" weight="bold" style={{ color: 'var(--cp-text)' }}>{t('backup.title')}</Text>
                        </Flex>
                        <Text size="2" style={{ color: 'var(--cp-text-muted)' }}>{t('backup.subtitle')}</Text>
                    </Box>
                )}
                {embedded && <Box />}
                <Button size="2" disabled={backingUp} onClick={handleBackupNow}>
                    <Play size={16} /> {backingUp ? t('backup.running') : t('backup.backup_now')}
                </Button>
            </Flex>

            {/* Status cards */}
            <Flex gap="3" mb="4" wrap="wrap">
                <Card style={{ padding: '12px 16px', flex: 1, minWidth: 180, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Text size="1" style={{ color: 'var(--cp-text-muted)', display: 'block' }}>{t('backup.current_status')}</Text>
                    <Flex align="center" gap="2" mt="1">
                        {status.state === 'running'
                            ? <Badge color="yellow" variant="soft"><Clock size={12} /> {t('backup.running')}</Badge>
                            : <Badge color="green" variant="soft"><CheckCircle size={12} /> {t('backup.idle')}</Badge>
                        }
                    </Flex>
                </Card>
                <Card style={{ padding: '12px 16px', flex: 1, minWidth: 180, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Text size="1" style={{ color: 'var(--cp-text-muted)', display: 'block' }}>{t('backup.last_backup')}</Text>
                    <Text size="2" weight="bold" style={{ display: 'block', color: 'var(--cp-text)', marginTop: 4 }}>
                        {formatDate(status.last_backup)}
                    </Text>
                    {status.last_status && statusBadge(status.last_status)}
                </Card>
                <Card style={{ padding: '12px 16px', flex: 1, minWidth: 180, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Text size="1" style={{ color: 'var(--cp-text-muted)', display: 'block' }}>{t('backup.next_scheduled')}</Text>
                    <Text size="2" weight="bold" style={{ display: 'block', color: 'var(--cp-text)', marginTop: 4 }}>
                        {status.next_run ? formatDate(status.next_run) : t('backup.never')}
                    </Text>
                </Card>
            </Flex>

            {/* Kopia dependency warning + one-click install */}
            {kopiaStatus && !kopiaStatus.available && (
                <Card mb="4" style={{ background: 'var(--orange-2)', border: '1px solid var(--orange-6)', padding: '16px 20px' }}>
                    <Flex direction="column" gap="3">
                        <Flex align="center" gap="2">
                            <AlertCircle size={18} style={{ color: 'var(--orange-9)' }} />
                            <Text size="3" weight="bold" style={{ color: 'var(--orange-11)' }}>{t('backup.kopia_not_installed')}</Text>
                        </Flex>
                        <Text size="2" style={{ color: 'var(--orange-11)' }}>{t('backup.kopia_install_hint')}</Text>

                        {/* Install logs area */}
                        {installLogs.length > 0 && (
                            <Box
                                style={{
                                    background: 'var(--gray-1)',
                                    border: '1px solid var(--gray-6)',
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
                                            color: log.startsWith('ERROR') ? 'var(--red-11)' : 'var(--gray-12)',
                                            whiteSpace: 'pre-wrap',
                                            wordBreak: 'break-all',
                                        }}
                                    >
                                        {log}
                                    </Text>
                                ))}
                                {installing && (
                                    <Flex align="center" gap="1" mt="1">
                                        <RefreshCw size={12} className="spin" />
                                        <Text size="1" color="gray">{t('backup.installing_kopia')}</Text>
                                    </Flex>
                                )}
                                <div ref={installLogsEndRef} />
                            </Box>
                        )}

                        {/* Install / Retry / Success buttons */}
                        {installDone && !installError ? (
                            <Button size="2" onClick={() => window.location.reload()}>
                                <CheckCircle size={16} /> {t('backup.install_kopia_success')}
                            </Button>
                        ) : installError ? (
                            <Button size="2" color="red" onClick={handleInstallKopia} disabled={installing}>
                                <RefreshCw size={16} /> {t('backup.retry_install')}
                            </Button>
                        ) : (
                            <Button size="2" onClick={handleInstallKopia} disabled={installing}>
                                {installing ? <RefreshCw size={16} className="spin" /> : <Download size={16} />}
                                {installing ? t('backup.installing_kopia') : t('backup.install_kopia')}
                            </Button>
                        )}
                    </Flex>
                </Card>
            )}

            {/* Tabs */}
            <Tabs.Root defaultValue="config">
                <Tabs.List>
                    <Tabs.Trigger value="config"><Settings size={14} style={{ marginRight: 4 }} /> {t('backup.configuration')}</Tabs.Trigger>
                    <Tabs.Trigger value="snapshots"><HardDrive size={14} style={{ marginRight: 4 }} /> {t('backup.snapshots')}</Tabs.Trigger>
                    <Tabs.Trigger value="logs"><AlertCircle size={14} style={{ marginRight: 4 }} /> {t('backup.logs')}</Tabs.Trigger>
                </Tabs.List>

                {/* Configuration Tab */}
                <Tabs.Content value="config">
                    <Box mt="4">
                        {/* Storage Target */}
                        <Card style={{ padding: 20, marginBottom: 16, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                            <Text size="3" weight="bold" mb="3" style={{ display: 'block', color: 'var(--cp-text)' }}>{t('backup.storage_target')}</Text>
                            <Flex direction="column" gap="3">
                                <Box>
                                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.target_type')}</Text>
                                    <Select.Root value={config.target_type} onValueChange={v => setConfig(prev => ({ ...prev, target_type: v, target: {} }))}>
                                        <Select.Trigger style={{ width: '100%' }} />
                                        <Select.Content>
                                            <Select.Item value="local">{t('backup.target_local')}</Select.Item>
                                            <Select.Item value="s3">{t('backup.target_s3')}</Select.Item>
                                            <Select.Item value="webdav">{t('backup.target_webdav')}</Select.Item>
                                            <Select.Item value="sftp">{t('backup.target_sftp')}</Select.Item>
                                        </Select.Content>
                                    </Select.Root>
                                </Box>
                                {renderTargetFields()}
                                <Box>
                                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.repo_password')}</Text>
                                    <TextField.Root
                                        type="password"
                                        placeholder={t('backup.encryption_password_placeholder')}
                                        value={config.encryption_password}
                                        onChange={e => setConfig(prev => ({ ...prev, encryption_password: e.target.value }))}
                                    />
                                </Box>
                                <Flex gap="2">
                                    <Button variant="soft" size="2" disabled={testing} onClick={handleTest}>
                                        {testing ? t('backup.testing') : t('backup.test_connection')}
                                    </Button>
                                </Flex>
                            </Flex>
                        </Card>

                        {/* Schedule */}
                        <Card style={{ padding: 20, marginBottom: 16, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                            <Text size="3" weight="bold" mb="3" style={{ display: 'block', color: 'var(--cp-text)' }}>{t('backup.schedule')}</Text>
                            <Flex direction="column" gap="3">
                                <Flex align="center" gap="2">
                                    <Switch
                                        checked={config.schedule_enabled}
                                        onCheckedChange={v => setConfig(prev => ({ ...prev, schedule_enabled: v }))}
                                    />
                                    <Text size="2">{t('backup.schedule_enabled')}</Text>
                                </Flex>
                                {config.schedule_enabled && (
                                    <>
                                        <Box>
                                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.cron_expr')}</Text>
                                            <TextField.Root
                                                placeholder="0 2 * * *"
                                                value={config.schedule_cron}
                                                onChange={e => setConfig(prev => ({ ...prev, schedule_cron: e.target.value }))}
                                            />
                                        </Box>
                                        <Flex gap="2" wrap="wrap">
                                            <Button variant="soft" size="1" onClick={() => setConfig(prev => ({ ...prev, schedule_cron: '0 2 * * *' }))}>
                                                {t('backup.cron_daily_2am')}
                                            </Button>
                                            <Button variant="soft" size="1" onClick={() => setConfig(prev => ({ ...prev, schedule_cron: '0 4 * * *' }))}>
                                                {t('backup.cron_daily_4am')}
                                            </Button>
                                            <Button variant="soft" size="1" onClick={() => setConfig(prev => ({ ...prev, schedule_cron: '0 2 * * 0' }))}>
                                                {t('backup.cron_weekly')}
                                            </Button>
                                        </Flex>
                                    </>
                                )}
                            </Flex>
                        </Card>

                        {/* Retention */}
                        <Card style={{ padding: 20, marginBottom: 16, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                            <Text size="3" weight="bold" mb="3" style={{ display: 'block', color: 'var(--cp-text)' }}>{t('backup.retention')}</Text>
                            <Flex gap="3" wrap="wrap">
                                <Box style={{ flex: 1, minWidth: 150 }}>
                                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.retain_count')}</Text>
                                    <TextField.Root
                                        type="number"
                                        value={config.retain_count}
                                        onChange={e => setConfig(prev => ({ ...prev, retain_count: parseInt(e.target.value) || 0 }))}
                                    />
                                </Box>
                                <Box style={{ flex: 1, minWidth: 150 }}>
                                    <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('backup.retain_days')}</Text>
                                    <TextField.Root
                                        type="number"
                                        value={config.retain_days}
                                        onChange={e => setConfig(prev => ({ ...prev, retain_days: parseInt(e.target.value) || 0 }))}
                                    />
                                </Box>
                            </Flex>
                        </Card>

                        {/* Scopes */}
                        <Card style={{ padding: 20, marginBottom: 16, background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                            <Text size="3" weight="bold" mb="3" style={{ display: 'block', color: 'var(--cp-text)' }}>{t('backup.scopes')}</Text>
                            <Flex direction="column" gap="2">
                                <Flex align="center" gap="2">
                                    <Switch checked={!!config.scopes.panel} onCheckedChange={v => updateScope('panel', v)} />
                                    <Text size="2">{t('backup.scope_panel')}</Text>
                                </Flex>
                                <Flex align="center" gap="2">
                                    <Switch checked={!!config.scopes.docker} onCheckedChange={v => updateScope('docker', v)} />
                                    <Text size="2">{t('backup.scope_docker')}</Text>
                                </Flex>
                                <Flex align="center" gap="2">
                                    <Switch checked={!!config.scopes.database} onCheckedChange={v => updateScope('database', v)} />
                                    <Text size="2">{t('backup.scope_database')}</Text>
                                </Flex>
                            </Flex>
                        </Card>

                        <Button size="2" disabled={saving} onClick={handleSave}>
                            {saving ? t('common.saving') : t('backup.save_config')}
                        </Button>
                    </Box>
                </Tabs.Content>

                {/* Snapshots Tab */}
                <Tabs.Content value="snapshots">
                    <Box mt="4">
                        {snapshots.length === 0 ? (
                            <Card style={{ padding: 40, textAlign: 'center', background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                                <HardDrive size={48} style={{ margin: '0 auto 12px', opacity: 0.3 }} />
                                <Text size="3" style={{ color: 'var(--cp-text-muted)', display: 'block' }}>{t('backup.no_snapshots')}</Text>
                            </Card>
                        ) : (
                            <Table.Root variant="surface">
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>{t('backup.created_at')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('backup.snapshot_id')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('backup.status')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('backup.size')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('backup.trigger')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('backup.scopes')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {snapshots.map(snap => (
                                        <Table.Row key={snap.id}>
                                            <Table.Cell>{formatDate(snap.created_at)}</Table.Cell>
                                            <Table.Cell>
                                                <Text size="1" style={{ fontFamily: 'monospace' }}>{snap.id?.slice(0, 12)}</Text>
                                            </Table.Cell>
                                            <Table.Cell>{statusBadge(snap.status)}</Table.Cell>
                                            <Table.Cell>{formatBytes(snap.size)}</Table.Cell>
                                            <Table.Cell>{snap.trigger || '-'}</Table.Cell>
                                            <Table.Cell>
                                                {(snap.scopes || []).map(s => (
                                                    <Badge key={s} variant="soft" size="1" style={{ marginRight: 4 }}>{s}</Badge>
                                                ))}
                                            </Table.Cell>
                                            <Table.Cell>
                                                <Flex gap="2">
                                                    <Button
                                                        variant="soft"
                                                        size="1"
                                                        color="blue"
                                                        disabled={snap.status !== 'completed'}
                                                        onClick={() => setRestoreId(snap.id)}
                                                    >
                                                        <RotateCcw size={14} /> {t('backup.restore')}
                                                    </Button>
                                                    <Button
                                                        variant="soft"
                                                        size="1"
                                                        color="red"
                                                        onClick={() => handleDelete(snap.id)}
                                                    >
                                                        <Trash2 size={14} />
                                                    </Button>
                                                </Flex>
                                            </Table.Cell>
                                        </Table.Row>
                                    ))}
                                </Table.Body>
                            </Table.Root>
                        )}
                    </Box>
                </Tabs.Content>

                {/* Logs Tab */}
                <Tabs.Content value="logs">
                    <Box mt="4">
                        <Flex mb="3">
                            <TextField.Root
                                placeholder={t('backup.filter_by_snapshot')}
                                value={logFilter}
                                onChange={e => setLogFilter(e.target.value)}
                                style={{ maxWidth: 300 }}
                            />
                        </Flex>
                        {filteredLogs.length === 0 ? (
                            <Card style={{ padding: 40, textAlign: 'center', background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                                <Text size="3" style={{ color: 'var(--cp-text-muted)' }}>{t('backup.no_logs')}</Text>
                            </Card>
                        ) : (
                            <Table.Root variant="surface">
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>{t('backup.time')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('backup.level')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('backup.message')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {filteredLogs.map((log, i) => (
                                        <Table.Row key={i}>
                                            <Table.Cell style={{ whiteSpace: 'nowrap' }}>{formatDate(log.time)}</Table.Cell>
                                            <Table.Cell>{logLevelBadge(log.level)}</Table.Cell>
                                            <Table.Cell style={{ fontFamily: 'monospace', fontSize: '0.85rem' }}>{log.message}</Table.Cell>
                                        </Table.Row>
                                    ))}
                                </Table.Body>
                            </Table.Root>
                        )}
                    </Box>
                </Tabs.Content>
            </Tabs.Root>

            {/* Restore Confirmation Dialog */}
            <Dialog.Root open={!!restoreId} onOpenChange={v => { if (!v) setRestoreId(null) }}>
                <Dialog.Content maxWidth="450px">
                    <Dialog.Title>{t('backup.confirm_restore_title')}</Dialog.Title>
                    <Dialog.Description size="2" mb="4">
                        {t('backup.confirm_restore_message')}
                    </Dialog.Description>
                    <Flex justify="end" gap="2">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button color="blue" disabled={restoring} onClick={handleRestore}>
                            <RotateCcw size={14} /> {restoring ? t('backup.restoring') : t('backup.restore')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
