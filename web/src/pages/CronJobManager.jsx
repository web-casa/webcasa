import { useState, useEffect, useCallback } from 'react'
import {
    Box, Flex, Text, Card, Badge, Button, Table, Dialog, TextField,
    Switch, TextArea, Heading, Callout, ScrollArea, Tabs,
} from '@radix-ui/themes'
import {
    Clock, Play, Plus, Pencil, Trash2,
    Timer, RotateCcw, ChevronDown, ChevronUp,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cronjobAPI } from '../api/index.js'

function statusBadge(status, t) {
    const statusMap = {
        success: { color: 'green', label: t('cronjob.status_success') },
        failed: { color: 'red', label: t('cronjob.status_failed') },
        timeout: { color: 'orange', label: t('cronjob.status_timeout') },
        skipped: { color: 'gray', label: t('cronjob.status_skipped') },
        running: { color: 'blue', label: t('cronjob.status_running') },
    }
    const m = statusMap[status] || { color: 'gray', label: status || '-' }
    return <Badge color={m.color} variant="soft">{m.label}</Badge>
}

function formatDate(ts) {
    if (!ts) return '-'
    const d = new Date(ts)
    if (isNaN(d.getTime())) return '-'
    return d.toLocaleString()
}

function formatDuration(ms) {
    if (ms == null) return '-'
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
    return `${Math.floor(ms / 60000)}m ${Math.round((ms % 60000) / 1000)}s`
}

const defaultForm = {
    name: '', expression: '', command: '', working_dir: '',
    tags: '', timeout_sec: 300, max_retries: 0, notify_on_failure: false, enabled: true,
}

export default function CronJobManager() {
    const { t } = useTranslation()
    const [tasks, setTasks] = useState([])
    const [logs, setLogs] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editId, setEditId] = useState(null)
    const [form, setForm] = useState({ ...defaultForm })
    const [saving, setSaving] = useState(false)
    const [triggerConfirm, setTriggerConfirm] = useState(null)
    const [deleteConfirm, setDeleteConfirm] = useState(null)
    const [expandedLog, setExpandedLog] = useState(null)
    const [activeTab, setActiveTab] = useState('tasks')
    const [logTaskId, setLogTaskId] = useState(null)

    const fetchTasks = useCallback(async () => {
        try {
            const res = await cronjobAPI.listTasks()
            setTasks(res.data || [])
        } catch { /* ignore */ }
    }, [])

    const fetchLogs = useCallback(async (taskId) => {
        try {
            const res = taskId
                ? await cronjobAPI.taskLogs(taskId, 50)
                : await cronjobAPI.allLogs(50)
            setLogs(res.data || [])
        } catch { /* ignore */ }
    }, [])

    const fetchAll = useCallback(async () => {
        setLoading(true)
        await fetchTasks()
        await fetchLogs(logTaskId)
        setLoading(false)
    }, [fetchTasks, fetchLogs, logTaskId])

    useEffect(() => { fetchAll() }, [fetchAll])

    const openCreate = () => {
        setEditId(null)
        setForm({ ...defaultForm })
        setDialogOpen(true)
    }

    const openEdit = (task) => {
        setEditId(task.id)
        const tags = task.tags ? (typeof task.tags === 'string' ? (() => { try { return JSON.parse(task.tags).join(', ') } catch { return task.tags } })() : (Array.isArray(task.tags) ? task.tags.join(', ') : '')) : ''
        setForm({
            name: task.name || '',
            expression: task.expression || '',
            command: task.command || '',
            working_dir: task.working_dir || '',
            tags,
            timeout_sec: task.timeout_sec || 300,
            max_retries: task.max_retries || 0,
            notify_on_failure: !!task.notify_on_failure,
            enabled: task.enabled !== false,
        })
        setDialogOpen(true)
    }

    const handleSave = async () => {
        setSaving(true)
        try {
            const tags = form.tags ? form.tags.split(',').map(s => s.trim()).filter(Boolean) : []
            const data = { ...form, tags, timeout_sec: Number(form.timeout_sec) || 300, max_retries: Number(form.max_retries) || 0 }
            if (editId) {
                await cronjobAPI.updateTask(editId, data)
            } else {
                await cronjobAPI.createTask(data)
            }
            setDialogOpen(false)
            fetchAll()
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        } finally {
            setSaving(false)
        }
    }

    const handleToggle = async (task) => {
        try {
            await cronjobAPI.updateTask(task.id, { enabled: !task.enabled })
            fetchTasks()
        } catch { /* ignore */ }
    }

    const handleDelete = async (id) => {
        try {
            await cronjobAPI.deleteTask(id)
            setDeleteConfirm(null)
            fetchAll()
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        }
    }

    const handleTrigger = async (id) => {
        try {
            await cronjobAPI.triggerTask(id)
            setTriggerConfirm(null)
            setTimeout(() => fetchAll(), 1000)
        } catch (e) {
            alert(e?.response?.data?.error || e.message)
        }
    }

    const viewTaskLogs = (taskId) => {
        setLogTaskId(taskId)
        setActiveTab('logs')
        fetchLogs(taskId)
    }

    return (
        <Box p="4" style={{ maxWidth: 1200, margin: '0 auto' }}>
            <Flex justify="between" align="center" mb="4">
                <Box>
                    <Heading size="5"><Clock size={20} style={{ display: 'inline', marginRight: 8, verticalAlign: 'text-bottom' }} />{t('cronjob.title')}</Heading>
                    <Text size="2" color="gray">{t('cronjob.subtitle')}</Text>
                </Box>
                <Flex gap="2">
                    <Button variant="soft" onClick={fetchAll}><RotateCcw size={14} /></Button>
                    <Button onClick={openCreate}><Plus size={14} /> {t('cronjob.new_task')}</Button>
                </Flex>
            </Flex>

            <Tabs.Root value={activeTab} onValueChange={setActiveTab}>
                <Tabs.List>
                    <Tabs.Trigger value="tasks"><Clock size={14} style={{ marginRight: 4 }} />{t('cronjob.title')}</Tabs.Trigger>
                    <Tabs.Trigger value="logs"><Timer size={14} style={{ marginRight: 4 }} />{t('cronjob.logs')}</Tabs.Trigger>
                </Tabs.List>

                <Tabs.Content value="tasks">
                    <Card mt="3">
                        {tasks.length === 0 ? (
                            <Callout.Root color="gray" mt="2">
                                <Callout.Text>{t('cronjob.no_tasks')}</Callout.Text>
                            </Callout.Root>
                        ) : (
                            <Table.Root>
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>{t('cronjob.task_name')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.expression')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.tags')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.next_run')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.last_status')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.enabled')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.actions')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {tasks.map(task => {
                                        let tagList = []
                                        try { tagList = typeof task.tags === 'string' ? JSON.parse(task.tags || '[]') : (task.tags || []) } catch {}
                                        return (
                                            <Table.Row key={task.id}>
                                                <Table.Cell>
                                                    <Text weight="medium">{task.name}</Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <code style={{ fontSize: 12 }}>{task.expression}</code>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Flex gap="1" wrap="wrap">
                                                        {tagList.map(tag => <Badge key={tag} variant="outline" size="1">{tag}</Badge>)}
                                                    </Flex>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="1" color="gray">{formatDate(task.next_run_at)}</Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Flex direction="column" gap="1">
                                                        {statusBadge(task.last_status, t)}
                                                        <Text size="1" color="gray">{formatDate(task.last_run_at)}</Text>
                                                    </Flex>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Switch checked={task.enabled} onCheckedChange={() => handleToggle(task)} />
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Flex gap="1">
                                                        <Button size="1" variant="ghost" onClick={() => openEdit(task)} title={t('cronjob.edit_task')}><Pencil size={14} /></Button>
                                                        <Button size="1" variant="ghost" color="green" onClick={() => setTriggerConfirm(task.id)} title={t('cronjob.trigger')}><Play size={14} /></Button>
                                                        <Button size="1" variant="ghost" onClick={() => viewTaskLogs(task.id)} title={t('cronjob.logs')}><Timer size={14} /></Button>
                                                        <Button size="1" variant="ghost" color="red" onClick={() => setDeleteConfirm(task.id)} title={t('common.delete')}><Trash2 size={14} /></Button>
                                                    </Flex>
                                                </Table.Cell>
                                            </Table.Row>
                                        )
                                    })}
                                </Table.Body>
                            </Table.Root>
                        )}
                    </Card>
                </Tabs.Content>

                <Tabs.Content value="logs">
                    <Card mt="3">
                        <Flex justify="between" align="center" mb="3">
                            <Text weight="medium">
                                {logTaskId ? t('cronjob.task_logs') : t('cronjob.all_logs')}
                            </Text>
                            {logTaskId && (
                                <Button size="1" variant="soft" onClick={() => { setLogTaskId(null); fetchLogs(null) }}>
                                    {t('cronjob.all_logs')}
                                </Button>
                            )}
                        </Flex>
                        {logs.length === 0 ? (
                            <Callout.Root color="gray">
                                <Callout.Text>{t('cronjob.no_logs')}</Callout.Text>
                            </Callout.Root>
                        ) : (
                            <Table.Root>
                                <Table.Header>
                                    <Table.Row>
                                        <Table.ColumnHeaderCell>{t('cronjob.task_name')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_started')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_duration')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_status')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_exit_code')}</Table.ColumnHeaderCell>
                                        <Table.ColumnHeaderCell>{t('cronjob.log_output')}</Table.ColumnHeaderCell>
                                    </Table.Row>
                                </Table.Header>
                                <Table.Body>
                                    {logs.map(log => (
                                        <Table.Row key={log.id}>
                                            <Table.Cell><Text size="2">{log.task_name || `#${log.task_id}`}</Text></Table.Cell>
                                            <Table.Cell><Text size="1">{formatDate(log.started_at)}</Text></Table.Cell>
                                            <Table.Cell><Text size="2">{formatDuration(log.duration_ms)}</Text></Table.Cell>
                                            <Table.Cell>{statusBadge(log.status, t)}</Table.Cell>
                                            <Table.Cell><Text size="2">{log.exit_code}</Text></Table.Cell>
                                            <Table.Cell>
                                                {log.output ? (
                                                    <Button size="1" variant="ghost" onClick={() => setExpandedLog(expandedLog === log.id ? null : log.id)}>
                                                        {expandedLog === log.id ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
                                                    </Button>
                                                ) : <Text size="1" color="gray">-</Text>}
                                            </Table.Cell>
                                        </Table.Row>
                                    ))}
                                </Table.Body>
                            </Table.Root>
                        )}
                        {expandedLog && (() => {
                            const log = logs.find(l => l.id === expandedLog)
                            if (!log?.output) return null
                            return (
                                <Card mt="2" style={{ background: 'var(--gray-2)' }}>
                                    <ScrollArea style={{ maxHeight: 300 }}>
                                        <pre style={{ fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0 }}>{log.output}</pre>
                                    </ScrollArea>
                                </Card>
                            )
                        })()}
                    </Card>
                </Tabs.Content>
            </Tabs.Root>

            {/* Create / Edit Dialog */}
            <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
                <Dialog.Content style={{ maxWidth: 520 }}>
                    <Dialog.Title>{editId ? t('cronjob.edit_task') : t('cronjob.new_task')}</Dialog.Title>

                    <Flex direction="column" gap="3" mt="3">
                        <label>
                            <Text size="2" weight="medium">{t('cronjob.task_name')}</Text>
                            <TextField.Root mt="1" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="Daily cleanup" />
                        </label>

                        <label>
                            <Text size="2" weight="medium">{t('cronjob.expression')}</Text>
                            <TextField.Root mt="1" value={form.expression} onChange={e => setForm({ ...form, expression: e.target.value })} placeholder="*/5 * * * *" />
                            <Text size="1" color="gray">{t('cronjob.expression_help')}</Text>
                            <Text size="1" color="gray" style={{ display: 'block' }}>{t('cronjob.expression_examples')}</Text>
                        </label>

                        <label>
                            <Text size="2" weight="medium">{t('cronjob.command')}</Text>
                            <TextArea mt="1" value={form.command} onChange={e => setForm({ ...form, command: e.target.value })} placeholder={t('cronjob.command_placeholder')} rows={3} />
                        </label>

                        <label>
                            <Text size="2" weight="medium">{t('cronjob.working_dir')}</Text>
                            <TextField.Root mt="1" value={form.working_dir} onChange={e => setForm({ ...form, working_dir: e.target.value })} placeholder={t('cronjob.working_dir_placeholder')} />
                        </label>

                        <label>
                            <Text size="2" weight="medium">{t('cronjob.tags')}</Text>
                            <TextField.Root mt="1" value={form.tags} onChange={e => setForm({ ...form, tags: e.target.value })} placeholder="backup, cleanup" />
                            <Text size="1" color="gray">{t('cronjob.tags_help')}</Text>
                        </label>

                        <Flex gap="3">
                            <label style={{ flex: 1 }}>
                                <Text size="2" weight="medium">{t('cronjob.timeout')}</Text>
                                <TextField.Root mt="1" type="number" value={form.timeout_sec} onChange={e => setForm({ ...form, timeout_sec: e.target.value })} />
                            </label>
                            <label style={{ flex: 1 }}>
                                <Text size="2" weight="medium">{t('cronjob.max_retries')}</Text>
                                <TextField.Root mt="1" type="number" value={form.max_retries} onChange={e => setForm({ ...form, max_retries: e.target.value })} />
                            </label>
                        </Flex>

                        <Flex gap="4" align="center">
                            <label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                <Switch checked={form.notify_on_failure} onCheckedChange={v => setForm({ ...form, notify_on_failure: v })} />
                                <Text size="2">{t('cronjob.notify_on_failure')}</Text>
                            </label>
                            <label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                <Switch checked={form.enabled} onCheckedChange={v => setForm({ ...form, enabled: v })} />
                                <Text size="2">{t('cronjob.enabled')}</Text>
                            </label>
                        </Flex>
                    </Flex>

                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button onClick={handleSave} disabled={saving || !form.name || !form.expression || !form.command}>
                            {saving ? t('common.loading') : t('common.save')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Trigger Confirm Dialog */}
            <Dialog.Root open={triggerConfirm != null} onOpenChange={() => setTriggerConfirm(null)}>
                <Dialog.Content style={{ maxWidth: 400 }}>
                    <Dialog.Title>{t('cronjob.trigger')}</Dialog.Title>
                    <Text>{t('cronjob.trigger_confirm')}</Text>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button color="green" onClick={() => handleTrigger(triggerConfirm)}>
                            <Play size={14} /> {t('cronjob.trigger')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Delete Confirm Dialog */}
            <Dialog.Root open={deleteConfirm != null} onOpenChange={() => setDeleteConfirm(null)}>
                <Dialog.Content style={{ maxWidth: 400 }}>
                    <Dialog.Title>{t('common.delete')}</Dialog.Title>
                    <Text>{t('cronjob.delete_confirm')}</Text>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button color="red" onClick={() => handleDelete(deleteConfirm)}>
                            <Trash2 size={14} /> {t('common.delete')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
