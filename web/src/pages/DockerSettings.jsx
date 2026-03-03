import { useState, useEffect, useCallback } from 'react'
import { Box, Flex, Text, Card, Button, TextField, Separator, Callout, Badge, Switch } from '@radix-ui/themes'
import { Settings, Plus, X, RefreshCw, AlertTriangle, Check, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { dockerAPI } from '../api/index.js'

const MIRROR_PRESETS = [
    { label: 'DaoCloud', url: 'https://docker.m.daocloud.io' },
    { label: '腾讯云', url: 'https://mirror.ccs.tencentyun.com' },
    { label: '中科大', url: 'https://docker.mirrors.ustc.edu.cn' },
]

const LOG_DRIVERS = ['json-file', 'syslog', 'journald', 'local', 'none']
const STORAGE_DRIVERS = ['overlay2', 'fuse-overlayfs', 'btrfs', 'zfs', 'vfs']

export default function DockerSettings() {
    const { t } = useTranslation()
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [success, setSuccess] = useState(false)
    const [error, setError] = useState('')

    // Form state
    const [mirrors, setMirrors] = useState([])
    const [insecureRegistries, setInsecureRegistries] = useState([])
    const [logDriver, setLogDriver] = useState('')
    const [logMaxSize, setLogMaxSize] = useState('')
    const [logMaxFile, setLogMaxFile] = useState('')
    const [storageDriver, setStorageDriver] = useState('')
    const [liveRestore, setLiveRestore] = useState(false)
    const [liveRestoreSet, setLiveRestoreSet] = useState(false)

    // Track original storage driver for warning
    const [originalStorageDriver, setOriginalStorageDriver] = useState('')

    const fetchConfig = useCallback(async () => {
        setLoading(true)
        try {
            const res = await dockerAPI.getDaemonConfig()
            const cfg = res.data?.config || {}
            setMirrors(cfg['registry-mirrors'] || [])
            setInsecureRegistries(cfg['insecure-registries'] || [])
            setLogDriver(cfg['log-driver'] || '')
            setLogMaxSize(cfg['log-opts']?.['max-size'] || '')
            setLogMaxFile(cfg['log-opts']?.['max-file'] || '')
            setStorageDriver(cfg['storage-driver'] || '')
            setOriginalStorageDriver(cfg['storage-driver'] || '')
            if (cfg['live-restore'] != null) {
                setLiveRestore(cfg['live-restore'])
                setLiveRestoreSet(true)
            } else {
                setLiveRestore(false)
                setLiveRestoreSet(false)
            }
        } catch {
            setError(t('docker.config_save_failed'))
        } finally {
            setLoading(false)
        }
    }, [t])

    useEffect(() => { fetchConfig() }, [fetchConfig])

    const handleSave = async () => {
        setSaving(true)
        setError('')
        setSuccess(false)
        try {
            const logOpts = {}
            if (logMaxSize) logOpts['max-size'] = logMaxSize
            if (logMaxFile) logOpts['max-file'] = logMaxFile

            const data = {
                'registry-mirrors': mirrors.filter(m => m.trim()),
                'insecure-registries': insecureRegistries.filter(r => r.trim()),
                'log-driver': logDriver,
                'log-opts': Object.keys(logOpts).length > 0 ? logOpts : null,
                'storage-driver': storageDriver,
                'live-restore': liveRestoreSet ? liveRestore : null,
            }
            await dockerAPI.updateDaemonConfig(data)
            setSuccess(true)
            setOriginalStorageDriver(storageDriver)
            setTimeout(() => setSuccess(false), 5000)
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        } finally {
            setSaving(false)
        }
    }

    // Mirror list helpers
    const addMirror = () => setMirrors([...mirrors, ''])
    const updateMirror = (i, val) => {
        const next = [...mirrors]
        next[i] = val
        setMirrors(next)
    }
    const removeMirror = (i) => setMirrors(mirrors.filter((_, idx) => idx !== i))
    const addPresetMirror = (url) => {
        if (!mirrors.includes(url)) {
            setMirrors([...mirrors, url])
        }
    }

    // Insecure registry list helpers
    const addRegistry = () => setInsecureRegistries([...insecureRegistries, ''])
    const updateRegistry = (i, val) => {
        const next = [...insecureRegistries]
        next[i] = val
        setInsecureRegistries(next)
    }
    const removeRegistry = (i) => setInsecureRegistries(insecureRegistries.filter((_, idx) => idx !== i))

    if (loading) {
        return (
            <Flex align="center" justify="center" style={{ minHeight: 200 }}>
                <RefreshCw size={20} className="spin" />
                <Text ml="2">{t('common.loading')}</Text>
            </Flex>
        )
    }

    const storageDriverChanged = storageDriver !== originalStorageDriver && originalStorageDriver !== ''

    return (
        <Box>
            <Flex align="center" gap="2" mb="2">
                <Settings size={24} />
                <Text size="5" weight="bold">{t('docker.daemon_config')}</Text>
            </Flex>
            <Text size="2" color="gray" mb="4" style={{ display: 'block' }}>
                {t('docker.registry_mirrors_desc')}
            </Text>
            <Separator size="4" mb="4" />

            <Flex direction="column" gap="4" style={{ maxWidth: 700 }}>
                {/* Registry Mirrors */}
                <Card style={{ padding: 20 }}>
                    <Text size="3" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.registry_mirrors')}</Text>
                    <Text size="2" color="gray" mb="3" style={{ display: 'block' }}>{t('docker.registry_mirrors_desc')}</Text>

                    <Flex direction="column" gap="2" mb="2">
                        {mirrors.map((m, i) => (
                            <Flex key={i} gap="2" align="center">
                                <Box style={{ flex: 1 }}>
                                    <TextField.Root
                                        placeholder={t('docker.mirror_placeholder')}
                                        value={m}
                                        onChange={(e) => updateMirror(i, e.target.value)}
                                    />
                                </Box>
                                <Button variant="ghost" size="1" color="red" onClick={() => removeMirror(i)}>
                                    <X size={14} />
                                </Button>
                            </Flex>
                        ))}
                    </Flex>

                    <Button variant="ghost" size="1" onClick={addMirror} mb="3">
                        <Plus size={14} /> {t('docker.add_mirror')}
                    </Button>

                    <Flex align="center" gap="2" wrap="wrap">
                        <Text size="1" color="gray">{t('docker.mirror_presets')}:</Text>
                        {MIRROR_PRESETS.map((preset) => (
                            <Badge
                                key={preset.url}
                                variant="soft"
                                style={{ cursor: 'pointer' }}
                                onClick={() => addPresetMirror(preset.url)}
                            >
                                {preset.label}
                            </Badge>
                        ))}
                    </Flex>
                </Card>

                {/* Insecure Registries */}
                <Card style={{ padding: 20 }}>
                    <Text size="3" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.insecure_registries')}</Text>
                    <Text size="2" color="gray" mb="3" style={{ display: 'block' }}>{t('docker.insecure_registries_desc')}</Text>

                    <Flex direction="column" gap="2" mb="2">
                        {insecureRegistries.map((r, i) => (
                            <Flex key={i} gap="2" align="center">
                                <Box style={{ flex: 1 }}>
                                    <TextField.Root
                                        placeholder={t('docker.registry_placeholder')}
                                        value={r}
                                        onChange={(e) => updateRegistry(i, e.target.value)}
                                    />
                                </Box>
                                <Button variant="ghost" size="1" color="red" onClick={() => removeRegistry(i)}>
                                    <X size={14} />
                                </Button>
                            </Flex>
                        ))}
                    </Flex>

                    <Button variant="ghost" size="1" onClick={addRegistry}>
                        <Plus size={14} /> {t('docker.add_registry')}
                    </Button>
                </Card>

                {/* Log Configuration */}
                <Card style={{ padding: 20 }}>
                    <Text size="3" weight="bold" mb="3" style={{ display: 'block' }}>{t('docker.log_config')}</Text>

                    <Flex direction="column" gap="3">
                        <Box>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.log_driver')}</Text>
                            <select
                                value={logDriver}
                                onChange={(e) => setLogDriver(e.target.value)}
                                style={{
                                    width: '100%', padding: '8px 10px', borderRadius: 6,
                                    border: '1px solid var(--gray-6)', fontSize: 13,
                                    background: 'var(--color-background)',
                                }}
                            >
                                <option value="">Default (json-file)</option>
                                {LOG_DRIVERS.map(d => <option key={d} value={d}>{d}</option>)}
                            </select>
                        </Box>

                        <Flex gap="3">
                            <Box style={{ flex: 1 }}>
                                <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.log_max_size')}</Text>
                                <TextField.Root placeholder="10m" value={logMaxSize} onChange={(e) => setLogMaxSize(e.target.value)} />
                            </Box>
                            <Box style={{ flex: 1 }}>
                                <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('docker.log_max_file')}</Text>
                                <TextField.Root placeholder="3" value={logMaxFile} onChange={(e) => setLogMaxFile(e.target.value)} />
                            </Box>
                        </Flex>
                    </Flex>
                </Card>

                {/* Storage Driver */}
                <Card style={{ padding: 20 }}>
                    <Text size="3" weight="bold" mb="3" style={{ display: 'block' }}>{t('docker.storage_driver_label')}</Text>

                    <select
                        value={storageDriver}
                        onChange={(e) => setStorageDriver(e.target.value)}
                        style={{
                            width: '100%', padding: '8px 10px', borderRadius: 6,
                            border: '1px solid var(--gray-6)', fontSize: 13,
                            background: 'var(--color-background)',
                        }}
                    >
                        <option value="">Default (overlay2)</option>
                        {STORAGE_DRIVERS.map(d => <option key={d} value={d}>{d}</option>)}
                    </select>

                    {storageDriverChanged && (
                        <Callout.Root color="orange" mt="3">
                            <Callout.Icon><AlertTriangle size={16} /></Callout.Icon>
                            <Callout.Text>{t('docker.storage_driver_warning')}</Callout.Text>
                        </Callout.Root>
                    )}
                </Card>

                {/* Other Options */}
                <Card style={{ padding: 20 }}>
                    <Text size="3" weight="bold" mb="3" style={{ display: 'block' }}>{t('docker.other_options')}</Text>

                    <Flex align="center" justify="between">
                        <Box>
                            <Text size="2" weight="bold" style={{ display: 'block' }}>{t('docker.live_restore')}</Text>
                            <Text size="2" color="gray">{t('docker.live_restore_desc')}</Text>
                        </Box>
                        <Switch
                            checked={liveRestore}
                            onCheckedChange={(checked) => {
                                setLiveRestore(checked)
                                setLiveRestoreSet(true)
                            }}
                        />
                    </Flex>
                </Card>

                {/* Status messages */}
                {success && (
                    <Callout.Root color="green">
                        <Callout.Icon><Check size={16} /></Callout.Icon>
                        <Callout.Text>{t('docker.config_saved')}</Callout.Text>
                    </Callout.Root>
                )}
                {error && (
                    <Callout.Root color="red">
                        <Callout.Icon><AlertTriangle size={16} /></Callout.Icon>
                        <Callout.Text>{error}</Callout.Text>
                    </Callout.Root>
                )}

                {/* Save Button */}
                <Flex justify="end">
                    <Button size="3" disabled={saving} onClick={handleSave}>
                        {saving ? (
                            <><Loader2 size={16} className="spin" /> {t('docker.saving_restarting')}</>
                        ) : (
                            <><RefreshCw size={16} /> {t('docker.save_restart')}</>
                        )}
                    </Button>
                </Flex>
            </Flex>
        </Box>
    )
}
