import { useState, useEffect, useRef } from 'react'
import { Box, Flex, Text, Button, Badge, Card, Heading, Tabs, Table, TextField, TextArea, Switch, Separator, IconButton, Code, Tooltip } from '@radix-ui/themes'
import { ArrowLeft, Rocket, Play, Square, RotateCw, Trash2, Plus, Copy, ExternalLink, Clock, GitCommit } from 'lucide-react'
import { useNavigate, useParams } from 'react-router'
import { deployAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

const statusColors = {
    pending: 'gray', building: 'blue', running: 'green', stopped: 'orange', error: 'red',
    success: 'green', failed: 'red', rolled_back: 'gray',
}

export default function ProjectDetail() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const { id } = useParams()
    const [project, setProject] = useState(null)
    const [deployments, setDeployments] = useState([])
    const [buildLog, setBuildLog] = useState('')
    const [runtimeLog, setRuntimeLog] = useState('')
    const [loading, setLoading] = useState(true)
    const [envVars, setEnvVars] = useState([])
    const [saving, setSaving] = useState(false)
    const logRef = useRef(null)

    const fetchProject = async () => {
        try {
            const res = await deployAPI.getProject(id)
            setProject(res.data)
            setEnvVars(res.data.env_vars || [])
        } catch (e) {
            console.error(e)
        } finally {
            setLoading(false)
        }
    }

    const fetchDeployments = async () => {
        try {
            const res = await deployAPI.deployments(id)
            setDeployments(res.data || [])
        } catch (e) { console.error(e) }
    }

    const fetchBuildLog = async () => {
        try {
            const res = await deployAPI.logs(id, { type: 'build' })
            setBuildLog(res.data.log || '')
        } catch (e) { console.error(e) }
    }

    const fetchRuntimeLog = async () => {
        try {
            const res = await deployAPI.logs(id, { type: 'runtime', lines: 200 })
            setRuntimeLog(res.data.log || '')
        } catch (e) { console.error(e) }
    }

    useEffect(() => {
        fetchProject()
        fetchDeployments()
        fetchBuildLog()
        fetchRuntimeLog()
    }, [id])

    // Auto-refresh when building
    useEffect(() => {
        if (project?.status !== 'building') return
        const timer = setInterval(() => {
            fetchProject()
            fetchBuildLog()
        }, 2000)
        return () => clearInterval(timer)
    }, [project?.status])

    // Auto-scroll log
    useEffect(() => {
        if (logRef.current) {
            logRef.current.scrollTop = logRef.current.scrollHeight
        }
    }, [buildLog])

    const handleAction = async (action) => {
        try {
            if (action === 'build') await deployAPI.build(id)
            else if (action === 'start') await deployAPI.start(id)
            else if (action === 'stop') await deployAPI.stop(id)
            else if (action === 'delete') {
                if (!confirm(t('deploy.confirm_delete'))) return
                await deployAPI.deleteProject(id)
                navigate('/deploy')
                return
            }
            fetchProject()
            fetchDeployments()
            setTimeout(fetchBuildLog, 1000)
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }

    const handleRollback = async (buildNum) => {
        if (!confirm(t('deploy.confirm_rollback', { build: buildNum }))) return
        try {
            await deployAPI.rollback(id, buildNum)
            fetchProject()
            fetchDeployments()
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }

    const saveEnvVars = async () => {
        setSaving(true)
        try {
            await deployAPI.updateProject(id, { env_vars: envVars.filter(e => e.key) })
            fetchProject()
        } catch (e) {
            alert(e.response?.data?.error || t('common.save_failed'))
        } finally {
            setSaving(false)
        }
    }

    const addEnvVar = () => setEnvVars([...envVars, { key: '', value: '' }])
    const removeEnvVar = (i) => setEnvVars(envVars.filter((_, idx) => idx !== i))
    const updateEnvVar = (i, field, val) => {
        const vars = [...envVars]
        vars[i] = { ...vars[i], [field]: val }
        setEnvVars(vars)
    }

    const copyWebhookUrl = () => {
        if (!project?.webhook_token) return
        const url = `${window.location.origin}/api/plugins/deploy/webhook/${project.webhook_token}`
        navigator.clipboard.writeText(url)
    }

    if (loading) return <Text color="gray">{t('common.loading')}</Text>
    if (!project) return <Text color="red">{t('deploy.not_found')}</Text>

    return (
        <Box>
            {/* Header */}
            <Flex justify="between" align="start" mb="4" wrap="wrap" gap="3">
                <Flex align="center" gap="2">
                    <Button variant="ghost" size="1" onClick={() => navigate('/deploy')}>
                        <ArrowLeft size={16} />
                    </Button>
                    <Box>
                        <Flex align="center" gap="2">
                            <Heading size="5">{project.name}</Heading>
                            <Badge color={statusColors[project.status]}>{t(`deploy.status_${project.status}`)}</Badge>
                        </Flex>
                        {project.domain && (
                            <Flex align="center" gap="1">
                                <Text size="2" color="gray">{project.domain}</Text>
                                <ExternalLink size={12} color="var(--gray-8)" />
                            </Flex>
                        )}
                    </Box>
                </Flex>
                <Flex gap="2">
                    <Button size="2" onClick={() => handleAction('build')} disabled={project.status === 'building'}>
                        <Rocket size={14} /> {t('deploy.build_deploy')}
                    </Button>
                    {project.status === 'running' && (
                        <Button size="2" variant="soft" color="orange" onClick={() => handleAction('stop')}>
                            <Square size={14} /> {t('docker.stop')}
                        </Button>
                    )}
                    {project.status === 'stopped' && (
                        <Button size="2" variant="soft" color="green" onClick={() => handleAction('start')}>
                            <Play size={14} /> {t('docker.start')}
                        </Button>
                    )}
                    <Button size="2" variant="soft" color="red" onClick={() => handleAction('delete')}>
                        <Trash2 size={14} />
                    </Button>
                </Flex>
            </Flex>

            {/* Error message */}
            {project.error_msg && (
                <Card mb="3" style={{ background: 'var(--red-2)', border: '1px solid var(--red-6)' }}>
                    <Text size="2" color="red">{project.error_msg}</Text>
                </Card>
            )}

            {/* Info cards */}
            <Flex gap="3" mb="4" wrap="wrap">
                <Card style={{ flex: '1 1 150px' }}>
                    <Text size="1" color="gray">{t('deploy.framework')}</Text>
                    <Text size="3" weight="medium">{project.framework || 'custom'}</Text>
                </Card>
                <Card style={{ flex: '1 1 150px' }}>
                    <Text size="1" color="gray">{t('deploy.build')}</Text>
                    <Text size="3" weight="medium">#{project.current_build || '—'}</Text>
                </Card>
                <Card style={{ flex: '1 1 150px' }}>
                    <Text size="1" color="gray">{t('deploy.port')}</Text>
                    <Text size="3" weight="medium">{project.port || '—'}</Text>
                </Card>
                <Card style={{ flex: '1 1 150px' }}>
                    <Text size="1" color="gray">{t('deploy.auto_deploy')}</Text>
                    <Text size="3" weight="medium">{project.auto_deploy ? t('common.enabled') : t('common.disabled')}</Text>
                </Card>
            </Flex>

            {/* Tabs */}
            <Tabs.Root defaultValue="logs">
                <Tabs.List>
                    <Tabs.Trigger value="logs">{t('deploy.build_log')}</Tabs.Trigger>
                    <Tabs.Trigger value="runtime">{t('deploy.runtime_log')}</Tabs.Trigger>
                    <Tabs.Trigger value="deployments">{t('deploy.deployments')}</Tabs.Trigger>
                    <Tabs.Trigger value="env">{t('deploy.env_vars')}</Tabs.Trigger>
                    <Tabs.Trigger value="webhook">{t('deploy.webhook')}</Tabs.Trigger>
                </Tabs.List>

                {/* Build Log */}
                <Tabs.Content value="logs">
                    <Card mt="3">
                        <Box ref={logRef} style={{ fontFamily: 'monospace', fontSize: 12, background: 'var(--gray-2)', padding: 16, borderRadius: 8, maxHeight: 500, overflow: 'auto', whiteSpace: 'pre-wrap', lineHeight: 1.6 }}>
                            {buildLog || t('deploy.no_logs')}
                        </Box>
                    </Card>
                </Tabs.Content>

                {/* Runtime Log */}
                <Tabs.Content value="runtime">
                    <Card mt="3">
                        <Flex justify="end" mb="2">
                            <Button variant="ghost" size="1" onClick={fetchRuntimeLog}>
                                <RotateCw size={14} /> {t('common.refresh')}
                            </Button>
                        </Flex>
                        <Box style={{ fontFamily: 'monospace', fontSize: 12, background: 'var(--gray-2)', padding: 16, borderRadius: 8, maxHeight: 500, overflow: 'auto', whiteSpace: 'pre-wrap', lineHeight: 1.6 }}>
                            {runtimeLog || t('deploy.no_logs')}
                        </Box>
                    </Card>
                </Tabs.Content>

                {/* Deployments */}
                <Tabs.Content value="deployments">
                    <Card mt="3">
                        {deployments.length === 0 ? (
                            <Text color="gray">{t('deploy.no_deployments')}</Text>
                        ) : (
                            <Table.Root>
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>#</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('deploy.commit')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('deploy.duration')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('deploy.time')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {deployments.map(d => (
                                        <Table.Row key={d.id}>
                                            <Table.Cell><Text weight="medium">#{d.build_num}</Text></Table.Cell>
                                            <Table.Cell>
                                                <Badge color={statusColors[d.status]}>{d.status}</Badge>
                                            </Table.Cell>
                                            <Table.Cell>
                                                <Flex align="center" gap="1">
                                                    <GitCommit size={12} />
                                                    <Code size="1">{d.git_commit || '—'}</Code>
                                                </Flex>
                                            </Table.Cell>
                                            <Table.Cell>
                                                <Flex align="center" gap="1">
                                                    <Clock size={12} />
                                                    <Text size="2">{d.duration ? `${d.duration}s` : '—'}</Text>
                                                </Flex>
                                            </Table.Cell>
                                            <Table.Cell>
                                                <Text size="2">{new Date(d.created_at).toLocaleString()}</Text>
                                            </Table.Cell>
                                            <Table.Cell>
                                                {d.status === 'success' && d.build_num !== project.current_build && (
                                                    <Tooltip content={t('deploy.rollback')}>
                                                        <Button variant="ghost" size="1" onClick={() => handleRollback(d.build_num)}>
                                                            <RotateCw size={14} />
                                                        </Button>
                                                    </Tooltip>
                                                )}
                                            </Table.Cell>
                                        </Table.Row>
                                    ))}
                                </Table.Body>
                            </Table.Root>
                        )}
                    </Card>
                </Tabs.Content>

                {/* Environment Variables */}
                <Tabs.Content value="env">
                    <Card mt="3">
                        <Flex justify="between" align="center" mb="3">
                            <Text size="2" weight="medium">{t('deploy.env_vars')}</Text>
                            <Flex gap="2">
                                <Button variant="ghost" size="1" onClick={addEnvVar}>
                                    <Plus size={14} /> {t('common.add')}
                                </Button>
                                <Button size="1" onClick={saveEnvVars} disabled={saving}>
                                    {saving ? t('common.saving') : t('common.save')}
                                </Button>
                            </Flex>
                        </Flex>
                        {envVars.length === 0 ? (
                            <Text size="2" color="gray">{t('deploy.no_env_vars')}</Text>
                        ) : (
                            <Flex direction="column" gap="2">
                                {envVars.map((ev, i) => (
                                    <Flex key={i} gap="2" align="center">
                                        <TextField.Root placeholder="KEY" value={ev.key} onChange={e => updateEnvVar(i, 'key', e.target.value)} style={{ flex: 1, fontFamily: 'monospace' }} />
                                        <TextField.Root placeholder="value" value={ev.value} onChange={e => updateEnvVar(i, 'value', e.target.value)} style={{ flex: 2, fontFamily: 'monospace' }} />
                                        <IconButton variant="ghost" color="red" size="1" onClick={() => removeEnvVar(i)}>
                                            <Trash2 size={14} />
                                        </IconButton>
                                    </Flex>
                                ))}
                            </Flex>
                        )}
                        <Text size="1" color="gray" mt="2">{t('deploy.env_rebuild_hint')}</Text>
                    </Card>
                </Tabs.Content>

                {/* Webhook */}
                <Tabs.Content value="webhook">
                    <Card mt="3">
                        <Flex direction="column" gap="3">
                            <Text size="2" weight="medium">{t('deploy.webhook')}</Text>
                            <Text size="2" color="gray">{t('deploy.webhook_hint')}</Text>
                            <Flex gap="2" align="center">
                                <Code style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                                    {`${window.location.origin}/api/plugins/deploy/webhook/${project.webhook_token}`}
                                </Code>
                                <Tooltip content={t('common.copy')}>
                                    <IconButton variant="ghost" size="1" onClick={copyWebhookUrl}>
                                        <Copy size={14} />
                                    </IconButton>
                                </Tooltip>
                            </Flex>
                            <Flex align="center" gap="2">
                                <Switch checked={project.auto_deploy} onCheckedChange={async (v) => {
                                    try {
                                        await deployAPI.updateProject(id, { auto_deploy: v })
                                        fetchProject()
                                    } catch {
                                        fetchProject() // revert UI to server state
                                    }
                                }} />
                                <Text size="2">{t('deploy.auto_deploy')}</Text>
                            </Flex>
                        </Flex>
                    </Card>
                </Tabs.Content>
            </Tabs.Root>
        </Box>
    )
}
