import { useState, useEffect, useRef } from 'react'
import { Box, Flex, Text, Button, Badge, Card, Heading, Tabs, Table, TextField, TextArea, Switch, Separator, IconButton, Code, Tooltip, Dialog } from '@radix-ui/themes'
import { ArrowLeft, Rocket, Play, Square, RotateCw, Trash2, Plus, Copy, ExternalLink, Clock, GitCommit, Container, Server, Bot, ChevronDown, ChevronRight, HardDrive, Wand2, Import, Timer, Layers, Pencil, FileSearch, Undo2 } from 'lucide-react'
import { useNavigate, useParams } from 'react-router'
import { deployAPI, aiAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

const statusColors = {
    pending: 'gray', building: 'blue', running: 'green', stopped: 'orange', error: 'red',
    success: 'green', failed: 'red', rolled_back: 'gray',
}

function DiagnosisCard({ diagnosis, t, onManualDiagnose, diagnosing }) {
    const [expanded, setExpanded] = useState(true)

    // No diagnosis yet — show manual diagnose button
    if (!diagnosis) {
        if (!onManualDiagnose) return null
        return (
            <Card mb="3" style={{ background: 'var(--orange-2)', border: '1px solid var(--orange-6)' }}>
                <Flex align="center" gap="2" justify="between">
                    <Flex align="center" gap="2">
                        <Bot size={16} color="var(--orange-9)" />
                        <Text size="2" color="orange">{t('deploy.no_diagnosis')}</Text>
                    </Flex>
                    <Button size="1" variant="soft" color="blue" onClick={onManualDiagnose} disabled={diagnosing}>
                        <Wand2 size={14} /> {diagnosing ? t('deploy.diagnosing') : t('deploy.manual_diagnose')}
                    </Button>
                </Flex>
            </Card>
        )
    }

    return (
        <Card mb="3" style={{ background: 'var(--blue-2)', border: '1px solid var(--blue-6)' }}>
            <Flex align="center" gap="2" mb={expanded ? '2' : '0'} style={{ cursor: 'pointer' }} onClick={() => setExpanded(!expanded)}>
                <Bot size={16} color="var(--blue-9)" />
                <Text size="2" weight="medium" color="blue">{t('deploy.ai_diagnosis')}</Text>
                {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            </Flex>
            {expanded && (
                <Box style={{ fontSize: 13, lineHeight: 1.6, whiteSpace: 'pre-wrap', color: 'var(--gray-12)' }}>
                    {diagnosis}
                </Box>
            )}
        </Card>
    )
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
    const [cacheSize, setCacheSize] = useState(0)
    const [allProjects, setAllProjects] = useState([])
    const [cloneDialogOpen, setCloneDialogOpen] = useState(false)
    const [diagnosing, setDiagnosing] = useState(false)
    const [manualDiagnosis, setManualDiagnosis] = useState(null)
    const [expandedDeps, setExpandedDeps] = useState({})
    const [cronJobs, setCronJobs] = useState([])
    const [cronDialogOpen, setCronDialogOpen] = useState(false)
    const [cronForm, setCronForm] = useState({ name: '', schedule: '', command: '', enabled: true })
    const [editingCronId, setEditingCronId] = useState(null)
    const [extraProcesses, setExtraProcesses] = useState([])
    const [procDialogOpen, setProcDialogOpen] = useState(false)
    const [procForm, setProcForm] = useState({ name: '', command: '', instances: 1, enabled: true })
    const [editingProcId, setEditingProcId] = useState(null)
    const [reviewing, setReviewing] = useState(false)
    const [reviewResult, setReviewResult] = useState(null)
    const [rollbackAdvice, setRollbackAdvice] = useState(null)
    const [requestingRollback, setRequestingRollback] = useState(false)
    const logRef = useRef(null)

    const fetchCacheInfo = async () => {
        try {
            const res = await deployAPI.getCacheInfo(id)
            setCacheSize(res.data.size || 0)
        } catch { /* ignore */ }
    }

    const handleClearCache = async () => {
        try {
            await deployAPI.clearCache(id)
            setCacheSize(0)
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }

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

    // ---- Cron Jobs ----
    const fetchCronJobs = async () => {
        try {
            const res = await deployAPI.listCrons(id)
            setCronJobs(res.data || [])
        } catch { /* ignore */ }
    }
    const openCronDialog = (job = null) => {
        if (job) {
            setEditingCronId(job.id)
            setCronForm({ name: job.name, schedule: job.schedule, command: job.command, enabled: job.enabled })
        } else {
            setEditingCronId(null)
            setCronForm({ name: '', schedule: '', command: '', enabled: true })
        }
        setCronDialogOpen(true)
    }
    const saveCronJob = async () => {
        try {
            if (editingCronId) {
                await deployAPI.updateCron(id, editingCronId, cronForm)
            } else {
                await deployAPI.createCron(id, cronForm)
            }
            setCronDialogOpen(false)
            fetchCronJobs()
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }
    const deleteCronJob = async (cronId) => {
        if (!confirm(t('common.delete') + '?')) return
        try {
            await deployAPI.deleteCron(id, cronId)
            fetchCronJobs()
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }
    const toggleCronJob = async (job) => {
        try {
            await deployAPI.updateCron(id, job.id, { enabled: !job.enabled })
            fetchCronJobs()
        } catch { /* ignore */ }
    }

    // ---- Extra Processes ----
    const fetchExtraProcesses = async () => {
        try {
            const res = await deployAPI.listProcesses(id)
            setExtraProcesses(res.data || [])
        } catch { /* ignore */ }
    }
    const openProcDialog = (proc = null) => {
        if (proc) {
            setEditingProcId(proc.id)
            setProcForm({ name: proc.name, command: proc.command, instances: proc.instances, enabled: proc.enabled })
        } else {
            setEditingProcId(null)
            setProcForm({ name: '', command: '', instances: 1, enabled: true })
        }
        setProcDialogOpen(true)
    }
    const saveExtraProcess = async () => {
        try {
            if (editingProcId) {
                await deployAPI.updateProcess(id, editingProcId, procForm)
            } else {
                await deployAPI.createProcess(id, procForm)
            }
            setProcDialogOpen(false)
            fetchExtraProcesses()
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }
    const deleteExtraProcess = async (procId) => {
        if (!confirm(t('common.delete') + '?')) return
        try {
            await deployAPI.deleteProcess(id, procId)
            fetchExtraProcesses()
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }
    const restartExtraProcess = async (procId) => {
        try {
            await deployAPI.restartProcess(id, procId)
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }
    const toggleExtraProcess = async (proc) => {
        try {
            await deployAPI.updateProcess(id, proc.id, { enabled: !proc.enabled })
            fetchExtraProcesses()
        } catch { /* ignore */ }
    }

    useEffect(() => {
        fetchProject()
        fetchDeployments()
        fetchBuildLog()
        fetchRuntimeLog()
        fetchCacheInfo()
        fetchCronJobs()
        fetchExtraProcesses()
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

    const suggestEnvVars = async () => {
        if (!project?.framework || project.framework === 'custom' || project.framework === 'dockerfile') return
        try {
            const res = await deployAPI.suggestEnv(project.framework)
            const suggestions = res.data || []
            if (suggestions.length === 0) return
            const existingKeys = new Set(envVars.map(e => e.key))
            const newVars = suggestions
                .filter(s => !existingKeys.has(s.key))
                .map(s => ({ key: s.key, value: s.default_value || '' }))
            if (newVars.length > 0) {
                setEnvVars([...envVars, ...newVars])
            }
        } catch { /* ignore */ }
    }

    const openCloneEnv = async () => {
        try {
            const res = await deployAPI.listProjects()
            setAllProjects((res.data || []).filter(p => p.id !== parseInt(id)))
            setCloneDialogOpen(true)
        } catch { /* ignore */ }
    }

    const handleCloneEnv = async (sourceId) => {
        try {
            await deployAPI.cloneEnv(id, sourceId)
            setCloneDialogOpen(false)
            // Refresh project to get new env vars
            const res = await deployAPI.getProject(id)
            setEnvVars(res.data.env_vars || [])
        } catch (e) {
            console.error(e)
        }
    }

    const handleManualDiagnose = async () => {
        if (!buildLog) return
        setDiagnosing(true)
        try {
            const res = await aiAPI.diagnose(buildLog, `Project: ${project.name}, Framework: ${project.framework}`)
            setManualDiagnosis(res.data.result || res.data)
            // Refresh deployments in case backend saved the result
            fetchDeployments()
        } catch (e) {
            console.error(e)
        } finally {
            setDiagnosing(false)
        }
    }

    const handleCodeReview = async () => {
        setReviewing(true)
        setReviewResult(null)
        try {
            const token = localStorage.getItem('token')
            const resp = await fetch('/api/plugins/ai/review-code', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
                body: JSON.stringify({ project_id: parseInt(id) }),
            })
            const reader = resp.body.getReader()
            const decoder = new TextDecoder()
            let text = ''
            while (true) {
                const { done, value } = await reader.read()
                if (done) break
                const chunk = decoder.decode(value, { stream: true })
                for (const line of chunk.split('\n')) {
                    if (line.startsWith('data: ')) {
                        text += line.slice(6)
                    } else if (line.startsWith('event: done')) {
                        break
                    } else if (line.startsWith('event: error')) {
                        // Next data line is the error
                    }
                }
                setReviewResult(text)
            }
        } catch (e) {
            console.error(e)
        } finally {
            setReviewing(false)
        }
    }

    const handleRequestRollbackAdvice = async () => {
        setRequestingRollback(true)
        setRollbackAdvice(null)
        try {
            const token = localStorage.getItem('token')
            const resp = await fetch('/api/plugins/ai/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
                body: JSON.stringify({ message: `Please analyze project #${id} and suggest whether I should rollback, and if so, which version to rollback to. Use the suggest_rollback tool.` }),
            })
            const reader = resp.body.getReader()
            const decoder = new TextDecoder()
            let text = ''
            while (true) {
                const { done, value } = await reader.read()
                if (done) break
                const chunk = decoder.decode(value, { stream: true })
                for (const line of chunk.split('\n')) {
                    if (line.startsWith('event: delta')) {
                        continue
                    } else if (line.startsWith('data: ') && !line.includes('"')) {
                        text += line.slice(6)
                    }
                }
                setRollbackAdvice(text)
            }
        } catch (e) {
            console.error(e)
        } finally {
            setRequestingRollback(false)
        }
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
                    <Button size="2" variant="soft" color="purple" onClick={handleCodeReview} disabled={reviewing}>
                        <FileSearch size={14} /> {reviewing ? t('deploy.code_review_running') : t('deploy.code_review')}
                    </Button>
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

            {/* AI Diagnosis of latest failed build */}
            {project.status === 'error' && (
                <>
                    <DiagnosisCard
                        diagnosis={deployments[0]?.diagnosis_result || manualDiagnosis}
                        t={t}
                        onManualDiagnose={handleManualDiagnose}
                        diagnosing={diagnosing}
                    />
                    <Card mb="3" style={{ background: 'var(--yellow-2)', border: '1px solid var(--yellow-6)' }}>
                        <Flex align="center" gap="2" justify="between">
                            <Flex align="center" gap="2">
                                <Undo2 size={16} color="var(--yellow-9)" />
                                <Text size="2" color="yellow">{rollbackAdvice ? '' : t('deploy.request_rollback_advice')}</Text>
                            </Flex>
                            {!rollbackAdvice && (
                                <Button size="1" variant="soft" color="yellow" onClick={handleRequestRollbackAdvice} disabled={requestingRollback}>
                                    <Bot size={14} /> {requestingRollback ? '...' : t('deploy.request_rollback_advice')}
                                </Button>
                            )}
                        </Flex>
                        {rollbackAdvice && (
                            <Box mt="2" style={{ fontSize: 13, lineHeight: 1.6, whiteSpace: 'pre-wrap', color: 'var(--gray-12)' }}>
                                {rollbackAdvice}
                            </Box>
                        )}
                    </Card>
                </>
            )}

            {/* Info cards */}
            <Flex gap="3" mb="4" wrap="wrap">
                <Card style={{ flex: '1 1 150px' }}>
                    <Text size="1" color="gray">{t('deploy.framework')}</Text>
                    <Text size="3" weight="medium">{project.framework || 'custom'}</Text>
                </Card>
                <Card style={{ flex: '1 1 150px' }}>
                    <Text size="1" color="gray">{t('deploy.deploy_mode')}</Text>
                    <Flex align="center" gap="1">
                        {project.deploy_mode === 'docker' ? <Container size={14} /> : <Server size={14} />}
                        <Text size="3" weight="medium">{project.deploy_mode === 'docker' ? 'Docker' : t('deploy.mode_bare')}</Text>
                    </Flex>
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

            {/* Docker info */}
            {project.deploy_mode === 'docker' && project.docker_image && (
                <Card mb="4" style={{ background: 'var(--blue-2)' }}>
                    <Flex gap="4" wrap="wrap">
                        <Box>
                            <Text size="1" color="gray">{t('deploy.docker_image')}</Text>
                            <Code size="2">{project.docker_image}</Code>
                        </Box>
                        {project.container_name && (
                            <Box>
                                <Text size="1" color="gray">{t('deploy.container_name')}</Text>
                                <Code size="2">{project.container_name}</Code>
                            </Box>
                        )}
                    </Flex>
                </Card>
            )}

            {/* Resource Limits & Build Cache */}
            {(project.memory_limit > 0 || project.cpu_limit > 0 || cacheSize > 0) && (
                <Card mb="4">
                    <Flex gap="4" wrap="wrap" align="center" justify="between">
                        <Flex gap="4" wrap="wrap">
                            {project.memory_limit > 0 && (
                                <Box>
                                    <Text size="1" color="gray">{t('deploy.memory_limit')}</Text>
                                    <Text size="2" weight="medium">{project.memory_limit} MB</Text>
                                </Box>
                            )}
                            {project.cpu_limit > 0 && (
                                <Box>
                                    <Text size="1" color="gray">{t('deploy.cpu_limit')}</Text>
                                    <Text size="2" weight="medium">{project.cpu_limit}%</Text>
                                </Box>
                            )}
                            {project.build_timeout > 0 && project.build_timeout !== 30 && (
                                <Box>
                                    <Text size="1" color="gray">{t('deploy.build_timeout')}</Text>
                                    <Text size="2" weight="medium">{project.build_timeout} min</Text>
                                </Box>
                            )}
                        </Flex>
                        {cacheSize > 0 && (
                            <Flex align="center" gap="2">
                                <HardDrive size={14} color="var(--gray-9)" />
                                <Text size="2" color="gray">{t('deploy.build_cache')}: {(cacheSize / 1024 / 1024).toFixed(1)} MB</Text>
                                <Button variant="ghost" size="1" color="red" onClick={handleClearCache}>
                                    <Trash2 size={12} /> {t('deploy.clear_cache')}
                                </Button>
                            </Flex>
                        )}
                    </Flex>
                </Card>
            )}

            {/* Code Review Result */}
            {reviewResult && (
                <Card mb="4" style={{ background: 'var(--purple-2)', border: '1px solid var(--purple-6)' }}>
                    <Flex align="center" gap="2" mb="2">
                        <FileSearch size={16} color="var(--purple-9)" />
                        <Text size="2" weight="medium" color="purple">{t('deploy.code_review')}</Text>
                    </Flex>
                    <Box style={{ fontSize: 13, lineHeight: 1.6, whiteSpace: 'pre-wrap', color: 'var(--gray-12)' }}>
                        {reviewResult}
                    </Box>
                    <Flex mt="3" gap="2">
                        <Button size="1" onClick={() => handleAction('build')} disabled={project.status === 'building'}>
                            <Rocket size={14} /> {t('deploy.continue_deploy')}
                        </Button>
                        <Button size="1" variant="soft" color="gray" onClick={() => setReviewResult(null)}>
                            {t('common.close')}
                        </Button>
                    </Flex>
                </Card>
            )}

            {/* Tabs */}
            <Tabs.Root defaultValue="logs">
                <Tabs.List>
                    <Tabs.Trigger value="logs">{t('deploy.build_log')}</Tabs.Trigger>
                    <Tabs.Trigger value="runtime">{project.deploy_mode === 'docker' ? t('deploy.container_log') : t('deploy.runtime_log')}</Tabs.Trigger>
                    <Tabs.Trigger value="deployments">{t('deploy.deployments')}</Tabs.Trigger>
                    <Tabs.Trigger value="env">{t('deploy.env_vars')}</Tabs.Trigger>
                    <Tabs.Trigger value="crons">{t('deploy.cron_jobs')}</Tabs.Trigger>
                    <Tabs.Trigger value="processes">{t('deploy.processes')}</Tabs.Trigger>
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
                                    {deployments.map(d => (<>
                                        <Table.Row key={d.id}>
                                            <Table.Cell><Text weight="medium">#{d.build_num}</Text></Table.Cell>
                                            <Table.Cell>
                                                <Flex align="center" gap="1">
                                                    <Badge color={statusColors[d.status]}>{d.status}</Badge>
                                                    {d.status === 'failed' && d.diagnosis_result && (
                                                        <Tooltip content={t('deploy.ai_diagnosis')}>
                                                            <IconButton variant="ghost" size="1" onClick={() => setExpandedDeps(prev => ({ ...prev, [d.id]: !prev[d.id] }))}>
                                                                <Bot size={12} color="var(--blue-9)" />
                                                            </IconButton>
                                                        </Tooltip>
                                                    )}
                                                </Flex>
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
                                        {d.status === 'failed' && d.diagnosis_result && expandedDeps[d.id] && (
                                            <Table.Row key={`${d.id}-diag`}>
                                                <Table.Cell colSpan={6}>
                                                    <DiagnosisCard diagnosis={d.diagnosis_result} t={t} />
                                                </Table.Cell>
                                            </Table.Row>
                                        )}
                                    </>))}
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
                                <Button variant="ghost" size="1" onClick={openCloneEnv}>
                                    <Import size={14} /> {t('deploy.import_env')}
                                </Button>
                                {project?.framework && project.framework !== 'custom' && project.framework !== 'dockerfile' && (
                                    <Button variant="ghost" size="1" onClick={suggestEnvVars}>
                                        <Wand2 size={14} /> {t('deploy.suggest_env')}
                                    </Button>
                                )}
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

                    <Dialog.Root open={cloneDialogOpen} onOpenChange={setCloneDialogOpen}>
                        <Dialog.Content maxWidth="400px">
                            <Dialog.Title>{t('deploy.import_env')}</Dialog.Title>
                            <Text size="2" color="gray" mb="3">{t('deploy.import_env_hint')}</Text>
                            <Flex direction="column" gap="2" mt="3">
                                {allProjects.length === 0 ? (
                                    <Text size="2" color="gray">{t('deploy.no_other_projects')}</Text>
                                ) : allProjects.map(p => (
                                    <Button key={p.id} variant="soft" onClick={() => handleCloneEnv(p.id)} style={{ justifyContent: 'flex-start' }}>
                                        <Text>{p.name}</Text>
                                        <Badge size="1" variant="outline" ml="auto">{p.framework}</Badge>
                                    </Button>
                                ))}
                            </Flex>
                            <Flex mt="4" justify="end">
                                <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                            </Flex>
                        </Dialog.Content>
                    </Dialog.Root>
                </Tabs.Content>

                {/* Cron Jobs */}
                <Tabs.Content value="crons">
                    <Card mt="3">
                        <Flex justify="between" align="center" mb="3">
                            <Text size="2" weight="medium">{t('deploy.cron_jobs')}</Text>
                            <Button size="1" onClick={() => openCronDialog()}>
                                <Plus size={14} /> {t('deploy.cron_add')}
                            </Button>
                        </Flex>
                        {cronJobs.length === 0 ? (
                            <Text size="2" color="gray">{t('deploy.cron_no_jobs')}</Text>
                        ) : (
                            <Table.Root>
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>{t('deploy.cron_name')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('deploy.cron_schedule')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('deploy.cron_command')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('deploy.cron_last_run')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {cronJobs.map(job => (
                                        <Table.Row key={job.id}>
                                            <Table.Cell><Text weight="medium">{job.name}</Text></Table.Cell>
                                            <Table.Cell><Code size="1">{job.schedule}</Code></Table.Cell>
                                            <Table.Cell><Code size="1" style={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', display: 'inline-block' }}>{job.command}</Code></Table.Cell>
                                            <Table.Cell>
                                                {job.last_run_at ? (
                                                    <Flex direction="column">
                                                        <Text size="1">{new Date(job.last_run_at).toLocaleString()}</Text>
                                                        {job.last_status && <Badge size="1" color={job.last_status === 'success' ? 'green' : 'red'}>{job.last_status}</Badge>}
                                                    </Flex>
                                                ) : <Text size="2" color="gray">—</Text>}
                                            </Table.Cell>
                                            <Table.Cell>
                                                <Switch checked={job.enabled} onCheckedChange={() => toggleCronJob(job)} />
                                            </Table.Cell>
                                            <Table.Cell>
                                                <Flex gap="1">
                                                    <Tooltip content={t('common.edit')}>
                                                        <IconButton variant="ghost" size="1" onClick={() => openCronDialog(job)}>
                                                            <Pencil size={14} />
                                                        </IconButton>
                                                    </Tooltip>
                                                    <Tooltip content={t('common.delete')}>
                                                        <IconButton variant="ghost" size="1" color="red" onClick={() => deleteCronJob(job.id)}>
                                                            <Trash2 size={14} />
                                                        </IconButton>
                                                    </Tooltip>
                                                </Flex>
                                            </Table.Cell>
                                        </Table.Row>
                                    ))}
                                </Table.Body>
                            </Table.Root>
                        )}
                    </Card>

                    <Dialog.Root open={cronDialogOpen} onOpenChange={setCronDialogOpen}>
                        <Dialog.Content maxWidth="450px">
                            <Dialog.Title>{editingCronId ? t('deploy.cron_edit') : t('deploy.cron_add')}</Dialog.Title>
                            <Flex direction="column" gap="3" mt="3">
                                <Box>
                                    <Text size="2" mb="1" weight="medium">{t('deploy.cron_name')}</Text>
                                    <TextField.Root value={cronForm.name} onChange={e => setCronForm({ ...cronForm, name: e.target.value })} placeholder="e.g. Clear temp files" />
                                </Box>
                                <Box>
                                    <Text size="2" mb="1" weight="medium">{t('deploy.cron_schedule')}</Text>
                                    <TextField.Root value={cronForm.schedule} onChange={e => setCronForm({ ...cronForm, schedule: e.target.value })} placeholder="*/5 * * * *" />
                                    <Text size="1" color="gray">{t('deploy.cron_schedule_hint')}</Text>
                                </Box>
                                <Box>
                                    <Text size="2" mb="1" weight="medium">{t('deploy.cron_command')}</Text>
                                    <TextField.Root value={cronForm.command} onChange={e => setCronForm({ ...cronForm, command: e.target.value })} placeholder="npm run cleanup" />
                                </Box>
                                <Flex align="center" gap="2">
                                    <Switch checked={cronForm.enabled} onCheckedChange={v => setCronForm({ ...cronForm, enabled: v })} />
                                    <Text size="2">{t('deploy.cron_enabled')}</Text>
                                </Flex>
                            </Flex>
                            <Flex mt="4" justify="end" gap="2">
                                <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                                <Button onClick={saveCronJob}>{t('common.save')}</Button>
                            </Flex>
                        </Dialog.Content>
                    </Dialog.Root>
                </Tabs.Content>

                {/* Extra Processes */}
                <Tabs.Content value="processes">
                    <Card mt="3">
                        <Flex justify="between" align="center" mb="3">
                            <Text size="2" weight="medium">{t('deploy.processes')}</Text>
                            <Button size="1" onClick={() => openProcDialog()}>
                                <Plus size={14} /> {t('deploy.process_add')}
                            </Button>
                        </Flex>
                        {extraProcesses.length === 0 ? (
                            <Text size="2" color="gray">{t('deploy.process_no_procs')}</Text>
                        ) : (
                            <Table.Root>
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>{t('deploy.process_name')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('deploy.process_command')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('deploy.process_instances')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {extraProcesses.map(proc => (
                                        <Table.Row key={proc.id}>
                                            <Table.Cell><Text weight="medium">{proc.name}</Text></Table.Cell>
                                            <Table.Cell><Code size="1" style={{ maxWidth: 250, overflow: 'hidden', textOverflow: 'ellipsis', display: 'inline-block' }}>{proc.command}</Code></Table.Cell>
                                            <Table.Cell><Text size="2">{proc.instances}</Text></Table.Cell>
                                            <Table.Cell>
                                                <Switch checked={proc.enabled} onCheckedChange={() => toggleExtraProcess(proc)} />
                                            </Table.Cell>
                                            <Table.Cell>
                                                <Flex gap="1">
                                                    <Tooltip content={t('deploy.process_restart')}>
                                                        <IconButton variant="ghost" size="1" onClick={() => restartExtraProcess(proc.id)}>
                                                            <RotateCw size={14} />
                                                        </IconButton>
                                                    </Tooltip>
                                                    <Tooltip content={t('common.edit')}>
                                                        <IconButton variant="ghost" size="1" onClick={() => openProcDialog(proc)}>
                                                            <Pencil size={14} />
                                                        </IconButton>
                                                    </Tooltip>
                                                    <Tooltip content={t('common.delete')}>
                                                        <IconButton variant="ghost" size="1" color="red" onClick={() => deleteExtraProcess(proc.id)}>
                                                            <Trash2 size={14} />
                                                        </IconButton>
                                                    </Tooltip>
                                                </Flex>
                                            </Table.Cell>
                                        </Table.Row>
                                    ))}
                                </Table.Body>
                            </Table.Root>
                        )}
                    </Card>

                    <Dialog.Root open={procDialogOpen} onOpenChange={setProcDialogOpen}>
                        <Dialog.Content maxWidth="450px">
                            <Dialog.Title>{editingProcId ? t('deploy.process_edit') : t('deploy.process_add')}</Dialog.Title>
                            <Flex direction="column" gap="3" mt="3">
                                <Box>
                                    <Text size="2" mb="1" weight="medium">{t('deploy.process_name')}</Text>
                                    <TextField.Root value={procForm.name} onChange={e => setProcForm({ ...procForm, name: e.target.value })} placeholder="e.g. worker" />
                                </Box>
                                <Box>
                                    <Text size="2" mb="1" weight="medium">{t('deploy.process_command')}</Text>
                                    <TextField.Root value={procForm.command} onChange={e => setProcForm({ ...procForm, command: e.target.value })} placeholder="node worker.js" />
                                </Box>
                                <Box>
                                    <Text size="2" mb="1" weight="medium">{t('deploy.process_instances')}</Text>
                                    <TextField.Root type="number" min="1" value={procForm.instances} onChange={e => setProcForm({ ...procForm, instances: parseInt(e.target.value) || 1 })} />
                                </Box>
                                <Flex align="center" gap="2">
                                    <Switch checked={procForm.enabled} onCheckedChange={v => setProcForm({ ...procForm, enabled: v })} />
                                    <Text size="2">{t('common.enabled')}</Text>
                                </Flex>
                            </Flex>
                            <Flex mt="4" justify="end" gap="2">
                                <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                                <Button onClick={saveExtraProcess}>{t('common.save')}</Button>
                            </Flex>
                        </Dialog.Content>
                    </Dialog.Root>
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
