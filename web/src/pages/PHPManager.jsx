import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Grid, Card, Button, IconButton, Text, Heading, Badge, Dialog, TextField, Select, Switch, Callout, Tabs, Separator, Tooltip, TextArea } from '@radix-ui/themes'
import { FileCode, Plus, Play, Square, RotateCcw, Trash2, RefreshCw, AlertCircle, CheckCircle2, Globe, Sparkles, Settings, Package, Info } from 'lucide-react'
import { phpAPI, dockerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import DockerRequired from '../components/DockerRequired.jsx'

const statusColors = { running: 'green', stopped: 'gray', error: 'red', creating: 'orange' }
const typeColors = { fpm: 'blue', frankenphp: 'purple' }

export default function PHPManager() {
    const { t } = useTranslation()
    const [dockerStatus, setDockerStatus] = useState(null)
    const [dockerChecking, setDockerChecking] = useState(true)
    const [activeTab, setActiveTab] = useState('runtimes')
    const [runtimes, setRuntimes] = useState([])
    const [sites, setSites] = useState([])
    const [versions, setVersions] = useState([])
    const [loading, setLoading] = useState(true)
    const [message, setMessage] = useState(null)

    // Install dialog
    const [installOpen, setInstallOpen] = useState(false)
    const [installForm, setInstallForm] = useState({ version: '8.4', type: 'fpm', extensions: [], memory_limit: '256m', auto_start: true })

    // Create site dialog
    const [siteDialogOpen, setSiteDialogOpen] = useState(false)
    const [siteForm, setSiteForm] = useState({ name: '', domain: '', php_version: '8.4', runtime_type: 'fpm', runtime_id: 0, root_path: '', worker_mode: false, worker_script: '', extensions: [], tls_enabled: true, http_redirect: true })

    // Config panel
    const [configRuntimeId, setConfigRuntimeId] = useState(null)
    const [phpConfig, setPHPConfig] = useState(null)
    const [fpmConfig, setFPMConfig] = useState(null)
    const [configLoading, setConfigLoading] = useState(false)

    // Progress dialog
    const [progressOpen, setProgressOpen] = useState(false)
    const [progressLogs, setProgressLogs] = useState([])
    const [progressDone, setProgressDone] = useState(false)
    const [progressError, setProgressError] = useState(null)
    const progressRef = useRef(null)

    // Extension management
    const [commonExtensions, setCommonExtensions] = useState([])
    const [extDialogOpen, setExtDialogOpen] = useState(false)
    const [extRuntimeId, setExtRuntimeId] = useState(null)
    const [selectedExts, setSelectedExts] = useState([])
    const [installedExts, setInstalledExts] = useState([])

    // Action loading
    const [actionLoading, setActionLoading] = useState(null)

    const checkDocker = useCallback(async () => {
        setDockerChecking(true)
        try {
            const res = await dockerAPI.status()
            setDockerStatus(res.data)
        } catch {
            setDockerStatus({ installed: false, daemon_running: false })
        } finally {
            setDockerChecking(false)
        }
    }, [])

    const fetchData = useCallback(async () => {
        try {
            const [rtRes, siteRes, verRes, extRes] = await Promise.allSettled([
                phpAPI.listRuntimes(),
                phpAPI.listSites(),
                phpAPI.versions(),
                phpAPI.commonExtensions(),
            ])
            if (rtRes.status === 'fulfilled') setRuntimes(rtRes.value.data || [])
            if (siteRes.status === 'fulfilled') setSites(siteRes.value.data || [])
            if (verRes.status === 'fulfilled') setVersions(verRes.value.data || [])
            if (extRes.status === 'fulfilled') setCommonExtensions(extRes.value.data || [])
        } catch { /* ignore */ } finally { setLoading(false) }
    }, [])

    useEffect(() => { checkDocker() }, [checkDocker])
    useEffect(() => { if (dockerStatus?.installed && dockerStatus?.daemon_running) fetchData() }, [dockerStatus, fetchData])

    // Docker gate
    if (dockerChecking) return <Box p="6"><Text>{t('common.loading')}...</Text></Box>
    if (!dockerStatus?.installed || !dockerStatus?.daemon_running) {
        return (
            <Box p="6">
                <Flex align="center" gap="2" mb="4">
                    <FileCode size={24} />
                    <Heading size="5">{t('php.title')}</Heading>
                </Flex>
                <DockerRequired installed={dockerStatus?.installed} daemonRunning={dockerStatus?.daemon_running} error={dockerStatus?.error} onRetry={checkDocker} extraMessage={t('php.docker_required')} />
            </Box>
        )
    }

    // ── Handlers ──

    const handleAction = async (id, action) => {
        setActionLoading(`${action}-${id}`)
        try {
            if (action === 'start') await phpAPI.startRuntime(id)
            else if (action === 'stop') await phpAPI.stopRuntime(id)
            else if (action === 'restart') await phpAPI.restartRuntime(id)
            await fetchData()
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || e.message })
        } finally { setActionLoading(null) }
    }

    const handleDeleteRuntime = async (id) => {
        if (!confirm(t('php.confirm_delete_runtime'))) return
        try {
            await phpAPI.deleteRuntime(id)
            await fetchData()
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || e.message })
        }
    }

    const handleDeleteSite = async (id, name) => {
        if (!confirm(t('php.confirm_delete_site', { name }))) return
        try {
            await phpAPI.deleteSite(id, false)
            await fetchData()
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || e.message })
        }
    }

    // SSE stream helper
    const streamSSE = (url, body, onDone) => {
        setProgressLogs([])
        setProgressDone(false)
        setProgressError(null)
        setProgressOpen(true)

        const token = localStorage.getItem('token')
        fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
            body: JSON.stringify(body),
        }).then(async (response) => {
            if (!response.ok) {
                let errMsg = `HTTP ${response.status}`
                try { const j = await response.json(); errMsg = j.error || errMsg } catch { /* ignore */ }
                setProgressError(errMsg)
                setProgressDone(true)
                return
            }

            const reader = response.body.getReader()
            const decoder = new TextDecoder()
            let buffer = ''
            let nextEvent = null
            let gotDone = false

            while (true) {
                const { done, value } = await reader.read()
                if (done) break
                buffer += decoder.decode(value, { stream: true })
                const lines = buffer.split('\n')
                buffer = lines.pop() || ''

                for (const line of lines) {
                    if (line.startsWith('event: ')) {
                        nextEvent = line.slice(7).trim()
                    } else if (line.startsWith('data: ')) {
                        const msg = line.slice(6)
                        if (nextEvent === 'error') {
                            setProgressError(msg)
                            setProgressDone(true)
                        } else if (nextEvent === 'done') {
                            gotDone = true
                            setProgressDone(true)
                            if (onDone) onDone()
                        } else {
                            setProgressLogs(prev => [...prev, msg])
                        }
                        nextEvent = null
                    }
                }
            }

            if (!gotDone) {
                setProgressDone(true)
                if (onDone) onDone()
            }
        }).catch((e) => {
            setProgressError(e.message)
            setProgressDone(true)
        })
    }

    const handleInstallRuntime = () => {
        const url = phpAPI.createRuntimeStreamUrl()
        streamSSE(url, installForm, () => {
            fetchData()
            setInstallOpen(false)
        })
    }

    const handleCreateSite = () => {
        const url = phpAPI.createSiteStreamUrl()
        const body = { ...siteForm }
        if (!body.root_path) body.root_path = `/var/www/${body.domain}`
        if (body.runtime_type === 'fpm' && body.runtime_id === 0) {
            const matching = runtimes.find(r => r.version === body.php_version && r.type === 'fpm')
            if (matching) body.runtime_id = matching.id
        }
        streamSSE(url, body, () => {
            fetchData()
            setSiteDialogOpen(false)
        })
    }

    // Config
    const loadConfig = async (id) => {
        setConfigRuntimeId(id)
        setConfigLoading(true)
        setActiveTab('config')
        try {
            const res = await phpAPI.getConfig(id)
            setPHPConfig(res.data.php_config)
            setFPMConfig(res.data.fpm_config)
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || e.message })
        } finally { setConfigLoading(false) }
    }

    const saveConfig = async () => {
        try {
            await phpAPI.updateConfig(configRuntimeId, { php_config: phpConfig, fpm_config: fpmConfig })
            setMessage({ type: 'success', text: t('php.config_saved') })
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || e.message })
        }
    }

    const handleOptimize = async () => {
        try {
            const res = await phpAPI.optimize(configRuntimeId)
            setFPMConfig(res.data)
            setMessage({ type: 'success', text: t('php.fpm_optimized') })
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || e.message })
        }
    }

    // Extensions
    const openExtDialog = async (id) => {
        setExtRuntimeId(id)
        try {
            const res = await phpAPI.getExtensions(id)
            setInstalledExts(res.data || [])
            setSelectedExts([])
        } catch { setInstalledExts([]) }
        setExtDialogOpen(true)
    }

    const handleInstallExts = () => {
        const url = phpAPI.installExtensionsStreamUrl(extRuntimeId)
        streamSSE(url, { extensions: selectedExts }, () => {
            fetchData()
            setExtDialogOpen(false)
        })
    }

    // Auto-scroll progress
    useEffect(() => {
        if (progressRef.current) {
            progressRef.current.scrollTop = progressRef.current.scrollHeight
        }
    }, [progressLogs])

    // Filter versions by type
    const fpmVersions = versions.filter(v => v.type === 'fpm')
    const frankenVersions = versions.filter(v => v.type === 'frankenphp')
    const selectedTypeVersions = installForm.type === 'fpm' ? fpmVersions : frankenVersions

    return (
        <Box p="6">
            <Flex justify="between" align="center" mb="4">
                <Flex align="center" gap="2">
                    <FileCode size={24} />
                    <Box>
                        <Heading size="5">{t('php.title')}</Heading>
                        <Text size="2" color="gray">{t('php.subtitle')}</Text>
                    </Box>
                </Flex>
                <Flex gap="2">
                    <Button variant="soft" onClick={fetchData}><RefreshCw size={14} /> {t('common.refresh')}</Button>
                </Flex>
            </Flex>

            {message && (
                <Callout.Root color={message.type === 'error' ? 'red' : 'green'} mb="4">
                    <Callout.Icon>{message.type === 'error' ? <AlertCircle size={16} /> : <CheckCircle2 size={16} />}</Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Tabs.Root value={activeTab} onValueChange={setActiveTab}>
                <Tabs.List>
                    <Tabs.Trigger value="runtimes">{t('php.tab_runtimes')}</Tabs.Trigger>
                    <Tabs.Trigger value="sites">{t('php.tab_sites')}</Tabs.Trigger>
                    <Tabs.Trigger value="config" disabled={!configRuntimeId}>{t('php.tab_config')}</Tabs.Trigger>
                </Tabs.List>

                {/* ── Runtimes Tab ── */}
                <Tabs.Content value="runtimes">
                    <Flex justify="between" align="center" my="4">
                        <Text size="2" color="gray">{t('php.runtime_count', { count: runtimes.length })}</Text>
                        <Button onClick={() => setInstallOpen(true)}><Plus size={14} /> {t('php.install_runtime')}</Button>
                    </Flex>

                    {runtimes.length === 0 && !loading ? (
                        <Card><Box p="6" style={{ textAlign: 'center' }}><Text color="gray">{t('php.no_runtimes')}</Text></Box></Card>
                    ) : (
                        <Grid columns={{ initial: '1', md: '2', lg: '3' }} gap="4">
                            {runtimes.map(rt => (
                                <Card key={rt.id}>
                                    <Flex justify="between" align="start" mb="2">
                                        <Flex align="center" gap="2">
                                            <FileCode size={20} />
                                            <Box>
                                                <Flex align="center" gap="2">
                                                    <Text weight="bold">PHP {rt.version}</Text>
                                                    <Badge color={typeColors[rt.type] || 'gray'} size="1">{rt.type === 'fpm' ? t('php.fpm') : t('php.frankenphp')}</Badge>
                                                </Flex>
                                                <Text size="1" color="gray">:{rt.port}</Text>
                                            </Box>
                                        </Flex>
                                        <Badge color={statusColors[rt.status] || 'gray'}>{rt.status === 'running' ? t('php.status_running') : t('php.status_stopped')}</Badge>
                                    </Flex>

                                    <Text size="1" color="gray" mb="2">{t('php.memory_limit')}: {rt.memory_limit}</Text>

                                    <Separator size="4" my="2" />
                                    <Flex gap="1" wrap="wrap">
                                        <Tooltip content="Start"><IconButton size="1" variant="ghost" color="green" disabled={rt.status === 'running' || actionLoading} onClick={() => handleAction(rt.id, 'start')}><Play size={14} /></IconButton></Tooltip>
                                        <Tooltip content="Stop"><IconButton size="1" variant="ghost" color="red" disabled={rt.status === 'stopped' || actionLoading} onClick={() => handleAction(rt.id, 'stop')}><Square size={14} /></IconButton></Tooltip>
                                        <Tooltip content="Restart"><IconButton size="1" variant="ghost" disabled={actionLoading} onClick={() => handleAction(rt.id, 'restart')}><RotateCcw size={14} /></IconButton></Tooltip>
                                        <Tooltip content={t('php.tab_config')}><IconButton size="1" variant="ghost" onClick={() => loadConfig(rt.id)}><Settings size={14} /></IconButton></Tooltip>
                                        <Tooltip content={t('php.manage_extensions')}><IconButton size="1" variant="ghost" onClick={() => openExtDialog(rt.id)}><Package size={14} /></IconButton></Tooltip>
                                        <Tooltip content={t('common.delete')}><IconButton size="1" variant="ghost" color="red" onClick={() => handleDeleteRuntime(rt.id)}><Trash2 size={14} /></IconButton></Tooltip>
                                    </Flex>
                                </Card>
                            ))}
                        </Grid>
                    )}
                </Tabs.Content>

                {/* ── Sites Tab ── */}
                <Tabs.Content value="sites">
                    <Flex justify="between" align="center" my="4">
                        <Text size="2" color="gray">{t('php.site_count', { count: sites.length })}</Text>
                        <Button onClick={() => setSiteDialogOpen(true)} disabled={runtimes.length === 0}><Plus size={14} /> {t('php.create_site')}</Button>
                    </Flex>

                    {sites.length === 0 && !loading ? (
                        <Card><Box p="6" style={{ textAlign: 'center' }}><Text color="gray">{t('php.no_sites')}</Text></Box></Card>
                    ) : (
                        <Grid columns={{ initial: '1', md: '2' }} gap="4">
                            {sites.map(site => (
                                <Card key={site.id}>
                                    <Flex justify="between" align="start" mb="2">
                                        <Box>
                                            <Text weight="bold">{site.name}</Text>
                                            <Flex align="center" gap="1" mt="1">
                                                <Globe size={12} />
                                                <Text size="1" color="blue">{site.domain}</Text>
                                            </Flex>
                                        </Box>
                                        <Flex gap="1" align="center">
                                            <Badge color={typeColors[site.runtime_type] || 'gray'} size="1">{site.runtime_type === 'fpm' ? t('php.fpm') : t('php.frankenphp')}</Badge>
                                            <Badge color={statusColors[site.status] || 'gray'} size="1">{site.status}</Badge>
                                        </Flex>
                                    </Flex>
                                    <Text size="1" color="gray">PHP {site.php_version} | {site.root_path}</Text>
                                    {site.worker_mode && <Badge color="orange" size="1" mt="1">{t('php.worker_mode_badge')}</Badge>}
                                    <Separator size="4" my="2" />
                                    <Flex gap="1">
                                        <Tooltip content={t('common.delete')}><IconButton size="1" variant="ghost" color="red" onClick={() => handleDeleteSite(site.id, site.name)}><Trash2 size={14} /></IconButton></Tooltip>
                                    </Flex>
                                </Card>
                            ))}
                        </Grid>
                    )}
                </Tabs.Content>

                {/* ── Config Tab ── */}
                <Tabs.Content value="config">
                    {configLoading ? (
                        <Box p="6"><Text>{t('common.loading')}...</Text></Box>
                    ) : phpConfig ? (
                        <Box mt="4">
                            <Flex justify="between" align="center" mb="4">
                                <Heading size="4">{t('php.php_ini')}</Heading>
                                <Flex gap="2">
                                    <Button variant="soft" color="orange" onClick={handleOptimize}><Sparkles size={14} /> {t('php.optimize')}</Button>
                                    <Button onClick={saveConfig}><CheckCircle2 size={14} /> {t('php.save_config')}</Button>
                                </Flex>
                            </Flex>

                            <Grid columns={{ initial: '1', md: '2' }} gap="4">
                                {/* php.ini settings */}
                                <Card>
                                    <Heading size="3" mb="3">{t('php.resource_limits')}</Heading>
                                    <Flex direction="column" gap="2">
                                        <ConfigField label="memory_limit" value={phpConfig.memory_limit} onChange={v => setPHPConfig(p => ({ ...p, memory_limit: v }))} />
                                        <ConfigField label="max_execution_time" value={phpConfig.max_execution_time} type="number" onChange={v => setPHPConfig(p => ({ ...p, max_execution_time: parseInt(v) || 0 }))} />
                                        <ConfigField label="max_input_time" value={phpConfig.max_input_time} type="number" onChange={v => setPHPConfig(p => ({ ...p, max_input_time: parseInt(v) || 0 }))} />
                                        <ConfigField label="max_input_vars" value={phpConfig.max_input_vars} type="number" onChange={v => setPHPConfig(p => ({ ...p, max_input_vars: parseInt(v) || 0 }))} />
                                    </Flex>
                                    <Separator size="4" my="3" />
                                    <Heading size="3" mb="3">{t('php.upload')}</Heading>
                                    <Flex direction="column" gap="2">
                                        <ConfigField label="upload_max_filesize" value={phpConfig.upload_max_filesize} onChange={v => setPHPConfig(p => ({ ...p, upload_max_filesize: v }))} />
                                        <ConfigField label="post_max_size" value={phpConfig.post_max_size} onChange={v => setPHPConfig(p => ({ ...p, post_max_size: v }))} />
                                        <ConfigField label="max_file_uploads" value={phpConfig.max_file_uploads} type="number" onChange={v => setPHPConfig(p => ({ ...p, max_file_uploads: parseInt(v) || 0 }))} />
                                    </Flex>
                                    <Separator size="4" my="3" />
                                    <Heading size="3" mb="3">{t('php.error_handling')}</Heading>
                                    <Flex direction="column" gap="2">
                                        <Flex align="center" justify="between"><Text size="2">display_errors</Text><Switch checked={phpConfig.display_errors} onCheckedChange={v => setPHPConfig(p => ({ ...p, display_errors: v }))} /></Flex>
                                        <Flex align="center" justify="between"><Text size="2">log_errors</Text><Switch checked={phpConfig.log_errors} onCheckedChange={v => setPHPConfig(p => ({ ...p, log_errors: v }))} /></Flex>
                                    </Flex>
                                    <Separator size="4" my="3" />
                                    <Heading size="3" mb="3">{t('php.opcache')}</Heading>
                                    <Flex direction="column" gap="2">
                                        <Flex align="center" justify="between"><Text size="2">opcache.enable</Text><Switch checked={phpConfig.opcache_enable} onCheckedChange={v => setPHPConfig(p => ({ ...p, opcache_enable: v }))} /></Flex>
                                        <ConfigField label="opcache.memory_consumption" value={phpConfig.opcache_memory} onChange={v => setPHPConfig(p => ({ ...p, opcache_memory: v }))} />
                                        <ConfigField label="opcache.max_accelerated_files" value={phpConfig.opcache_max_files} type="number" onChange={v => setPHPConfig(p => ({ ...p, opcache_max_files: parseInt(v) || 0 }))} />
                                        <ConfigField label="opcache.revalidate_freq" value={phpConfig.opcache_revalidate} type="number" onChange={v => setPHPConfig(p => ({ ...p, opcache_revalidate: parseInt(v) || 0 }))} />
                                    </Flex>
                                    <Separator size="4" my="3" />
                                    <ConfigField label="date.timezone" value={phpConfig.date_timezone} onChange={v => setPHPConfig(p => ({ ...p, date_timezone: v }))} />
                                </Card>

                                {/* FPM settings */}
                                <Card>
                                    <Heading size="3" mb="3">{t('php.fpm_settings')}</Heading>
                                    <Flex direction="column" gap="2">
                                        <Flex align="center" justify="between">
                                            <Text size="2">{t('php.pm_mode')}</Text>
                                            <Select.Root value={fpmConfig?.pm || 'dynamic'} onValueChange={v => setFPMConfig(p => ({ ...p, pm: v }))}>
                                                <Select.Trigger />
                                                <Select.Content>
                                                    <Select.Item value="dynamic">dynamic</Select.Item>
                                                    <Select.Item value="static">static</Select.Item>
                                                    <Select.Item value="ondemand">ondemand</Select.Item>
                                                </Select.Content>
                                            </Select.Root>
                                        </Flex>
                                        <ConfigField label={t('php.max_children')} value={fpmConfig?.max_children} type="number" onChange={v => setFPMConfig(p => ({ ...p, max_children: parseInt(v) || 0 }))} />
                                        {fpmConfig?.pm === 'dynamic' && (
                                            <>
                                                <ConfigField label={t('php.start_servers')} value={fpmConfig?.start_servers} type="number" onChange={v => setFPMConfig(p => ({ ...p, start_servers: parseInt(v) || 0 }))} />
                                                <ConfigField label={t('php.min_spare')} value={fpmConfig?.min_spare_servers} type="number" onChange={v => setFPMConfig(p => ({ ...p, min_spare_servers: parseInt(v) || 0 }))} />
                                                <ConfigField label={t('php.max_spare')} value={fpmConfig?.max_spare_servers} type="number" onChange={v => setFPMConfig(p => ({ ...p, max_spare_servers: parseInt(v) || 0 }))} />
                                            </>
                                        )}
                                        {fpmConfig?.pm === 'ondemand' && (
                                            <ConfigField label={t('php.idle_timeout')} value={fpmConfig?.idle_timeout} type="number" onChange={v => setFPMConfig(p => ({ ...p, idle_timeout: parseInt(v) || 0 }))} />
                                        )}
                                        <ConfigField label={t('php.max_requests')} value={fpmConfig?.max_requests} type="number" onChange={v => setFPMConfig(p => ({ ...p, max_requests: parseInt(v) || 0 }))} />
                                    </Flex>

                                    <Separator size="4" my="3" />
                                    <Callout.Root color="blue">
                                        <Callout.Icon><Sparkles size={16} /></Callout.Icon>
                                        <Callout.Text>{t('php.optimize_desc')}</Callout.Text>
                                    </Callout.Root>
                                </Card>
                            </Grid>

                            {/* Advanced: Custom directives */}
                            <Card mt="4">
                                <Heading size="3" mb="2">{t('php.advanced')} — {t('php.custom_directives')}</Heading>
                                <TextArea rows={4} value={phpConfig.custom_directives || ''} onChange={e => setPHPConfig(p => ({ ...p, custom_directives: e.target.value }))} placeholder="; Additional php.ini directives" style={{ fontFamily: 'monospace' }} />
                            </Card>
                        </Box>
                    ) : (
                        <Box p="6"><Text color="gray">{t('php.select_runtime_hint')}</Text></Box>
                    )}
                </Tabs.Content>
            </Tabs.Root>

            {/* ── Install Runtime Dialog ── */}
            <Dialog.Root open={installOpen} onOpenChange={setInstallOpen}>
                <Dialog.Content maxWidth="480px">
                    <Dialog.Title>{t('php.install_runtime')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('php.runtime_type')}</Text>
                            <Select.Root value={installForm.type} onValueChange={v => setInstallForm(p => ({ ...p, type: v, version: v === 'fpm' ? '8.4' : '8.4' }))}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    <Select.Item value="fpm">{t('php.fpm')} — {t('php.fpm_traditional')}</Select.Item>
                                    <Select.Item value="frankenphp">{t('php.frankenphp')} — {t('php.frankenphp_modern')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Box>

                        {installForm.type === 'frankenphp' && (
                            <Callout.Root color="blue">
                                <Callout.Icon><Info size={16} /></Callout.Icon>
                                <Callout.Text>
                                    <Text size="2" weight="bold">{t('php.franken_tip_title')}</Text><br />
                                    <Text size="1" color="green">{t('php.franken_suitable')}</Text><br />
                                    <Text size="1" color="red">{t('php.franken_unsuitable')}</Text>
                                </Callout.Text>
                            </Callout.Root>
                        )}

                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('php.version')}</Text>
                            <Select.Root value={installForm.version} onValueChange={v => setInstallForm(p => ({ ...p, version: v }))}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    {selectedTypeVersions.map(v => (
                                        <Select.Item key={v.version} value={v.version}>
                                            PHP {v.version} {v.eol ? '(EOL)' : ''} {v.default ? '(Default)' : ''}
                                        </Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                            {selectedTypeVersions.find(v => v.version === installForm.version)?.eol && (
                                <Callout.Root color="orange" mt="2" size="1"><Callout.Icon><AlertCircle size={14} /></Callout.Icon><Callout.Text>{t('php.eol_warning')}</Callout.Text></Callout.Root>
                            )}
                        </Box>

                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('php.memory_limit')}</Text>
                            <TextField.Root value={installForm.memory_limit} onChange={e => setInstallForm(p => ({ ...p, memory_limit: e.target.value }))} placeholder="256m" />
                        </Box>

                        <Flex align="center" justify="between">
                            <Text size="2">{t('php.auto_start')}</Text>
                            <Switch checked={installForm.auto_start} onCheckedChange={v => setInstallForm(p => ({ ...p, auto_start: v }))} />
                        </Flex>
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleInstallRuntime}>{t('php.install_runtime')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* ── Create Site Dialog ── */}
            <Dialog.Root open={siteDialogOpen} onOpenChange={setSiteDialogOpen}>
                <Dialog.Content maxWidth="520px">
                    <Dialog.Title>{t('php.create_site')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('php.site_name')}</Text>
                            <TextField.Root value={siteForm.name} onChange={e => setSiteForm(p => ({ ...p, name: e.target.value }))} placeholder="my-site" />
                        </Box>
                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('php.domain')}</Text>
                            <TextField.Root value={siteForm.domain} onChange={e => setSiteForm(p => ({ ...p, domain: e.target.value, root_path: `/var/www/${e.target.value}` }))} placeholder="example.com" />
                        </Box>
                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('php.runtime_type')}</Text>
                            <Select.Root value={siteForm.runtime_type} onValueChange={v => setSiteForm(p => ({ ...p, runtime_type: v }))}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    <Select.Item value="fpm">{t('php.fpm')}</Select.Item>
                                    <Select.Item value="frankenphp">{t('php.frankenphp')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Box>

                        {siteForm.runtime_type === 'frankenphp' && (
                            <Callout.Root color="blue" size="1">
                                <Callout.Icon><Info size={14} /></Callout.Icon>
                                <Callout.Text size="1">{t('php.franken_suitable')}</Callout.Text>
                            </Callout.Root>
                        )}

                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('php.php_version')}</Text>
                            <Select.Root value={siteForm.php_version} onValueChange={v => setSiteForm(p => ({ ...p, php_version: v }))}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    {(siteForm.runtime_type === 'fpm' ? fpmVersions : frankenVersions).map(v => (
                                        <Select.Item key={v.version} value={v.version}>PHP {v.version} {v.eol ? '(EOL)' : ''}</Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </Box>

                        {siteForm.runtime_type === 'fpm' && (
                            <Box>
                                <Text size="2" weight="bold" mb="1">{t('php.select_runtime')}</Text>
                                <Select.Root value={String(siteForm.runtime_id)} onValueChange={v => setSiteForm(p => ({ ...p, runtime_id: parseInt(v) }))}>
                                    <Select.Trigger style={{ width: '100%' }} />
                                    <Select.Content>
                                        {runtimes.filter(r => r.type === 'fpm' && r.version === siteForm.php_version).map(r => (
                                            <Select.Item key={r.id} value={String(r.id)}>PHP {r.version} FPM (:{r.port}) — {r.status}</Select.Item>
                                        ))}
                                    </Select.Content>
                                </Select.Root>
                            </Box>
                        )}

                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('php.root_path')}</Text>
                            <TextField.Root value={siteForm.root_path} onChange={e => setSiteForm(p => ({ ...p, root_path: e.target.value }))} placeholder="/var/www/example.com" />
                        </Box>

                        {siteForm.runtime_type === 'frankenphp' && (
                            <>
                                <Flex align="center" justify="between">
                                    <Text size="2">{t('php.worker_mode')}</Text>
                                    <Switch checked={siteForm.worker_mode} onCheckedChange={v => setSiteForm(p => ({ ...p, worker_mode: v }))} />
                                </Flex>
                                {siteForm.worker_mode && (
                                    <Box>
                                        <Text size="2" weight="bold" mb="1">{t('php.worker_script')}</Text>
                                        <TextField.Root value={siteForm.worker_script} onChange={e => setSiteForm(p => ({ ...p, worker_script: e.target.value }))} placeholder="public/index.php" />
                                    </Box>
                                )}
                            </>
                        )}

                        <Flex align="center" justify="between">
                            <Text size="2">{t('php.tls_enabled')}</Text>
                            <Switch checked={siteForm.tls_enabled} onCheckedChange={v => setSiteForm(p => ({ ...p, tls_enabled: v }))} />
                        </Flex>
                        <Flex align="center" justify="between">
                            <Text size="2">{t('php.http_redirect')}</Text>
                            <Switch checked={siteForm.http_redirect} onCheckedChange={v => setSiteForm(p => ({ ...p, http_redirect: v }))} />
                        </Flex>
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleCreateSite} disabled={!siteForm.name || !siteForm.domain}>{t('php.create_site')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* ── Extension Management Dialog ── */}
            <Dialog.Root open={extDialogOpen} onOpenChange={setExtDialogOpen}>
                <Dialog.Content maxWidth="560px">
                    <Dialog.Title>{t('php.manage_extensions')}</Dialog.Title>
                    {installedExts.length > 0 && (
                        <Box mb="3">
                            <Text size="2" weight="bold" mb="1">{t('php.installed_label')}</Text>
                            <Flex gap="1" wrap="wrap">
                                {installedExts.map(e => <Badge key={e} color="green" size="1">{e}</Badge>)}
                            </Flex>
                        </Box>
                    )}
                    <Text size="2" weight="bold" mb="2">{t('php.install_extensions')}:</Text>
                    <Grid columns="2" gap="2">
                        {commonExtensions.filter(e => !installedExts.includes(e.name)).map(ext => (
                            <Flex key={ext.name} align="center" gap="2">
                                <Switch size="1" checked={selectedExts.includes(ext.name)} onCheckedChange={checked => {
                                    if (checked) setSelectedExts(p => [...p, ext.name])
                                    else setSelectedExts(p => p.filter(e => e !== ext.name))
                                }} />
                                <Box>
                                    <Text size="2">{ext.label}</Text>
                                    <Text size="1" color="gray"> ({ext.category})</Text>
                                </Box>
                            </Flex>
                        ))}
                    </Grid>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleInstallExts} disabled={selectedExts.length === 0}>{t('php.install_extensions')} ({selectedExts.length})</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* ── Progress Dialog ── */}
            <Dialog.Root open={progressOpen} onOpenChange={(open) => { if (progressDone) setProgressOpen(open) }}>
                <Dialog.Content maxWidth="600px">
                    <Dialog.Title>{progressDone ? (progressError ? t('php.progress_error') : t('php.progress_complete')) : t('php.installing')}</Dialog.Title>
                    <Box ref={progressRef} style={{ maxHeight: '300px', overflow: 'auto', background: 'var(--gray-2)', borderRadius: '8px', padding: '12px', fontFamily: 'monospace', fontSize: '12px' }}>
                        {progressLogs.map((line, i) => <Text key={i} as="div" size="1" style={{ whiteSpace: 'pre-wrap' }}>{line}</Text>)}
                    </Box>
                    {progressDone && (
                        <Flex mt="3" justify="end">
                            <Button onClick={() => setProgressOpen(false)}>{t('common.close')}</Button>
                        </Flex>
                    )}
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}

function ConfigField({ label, value, type = 'text', onChange }) {
    return (
        <Flex align="center" justify="between" gap="2">
            <Text size="2" style={{ minWidth: '180px', fontFamily: 'monospace' }}>{label}</Text>
            <TextField.Root size="1" value={value ?? ''} type={type} onChange={e => onChange(e.target.value)} style={{ maxWidth: '120px' }} />
        </Flex>
    )
}
