import { useState, useEffect, useCallback, useRef } from 'react'
import {
    Box, Flex, Heading, Text, Button, Badge, Card, Dialog,
    TextField, Callout, IconButton, Spinner, AlertDialog, Tooltip,
} from '@radix-ui/themes'
import {
    Plus, Pencil, Trash2, Download, Upload, Globe, AlertCircle,
    FileJson, Layers, X,
} from 'lucide-react'
import { templateAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

// Map preset DB names to i18n keys
const PRESET_KEY_MAP = {
    'WordPress Reverse Proxy': 'wordpress',
    'SPA Static Site': 'spa',
    'API Reverse Proxy': 'api',
    'PHP-FPM Site': 'php',
    'Static File Download Site': 'download',
    'WebSocket Application': 'websocket',
}

function getPresetDisplay(tpl, t) {
    const key = PRESET_KEY_MAP[tpl.name]
    if (key) {
        return {
            name: t(`template.preset.${key}.name`),
            description: t(`template.preset.${key}.description`),
        }
    }
    return { name: tpl.name, description: tpl.description }
}

// ============ Create Host Dialog ============
function CreateHostDialog({ open, onClose, template, t }) {
    const [domain, setDomain] = useState('')
    const [creating, setCreating] = useState(false)
    const [error, setError] = useState('')

    useEffect(() => {
        if (open) { setDomain(''); setError(''); setCreating(false) }
    }, [open])

    const handleCreate = async () => {
        if (!domain.trim()) return
        setError('')
        setCreating(true)
        try {
            await templateAPI.createHost(template.id, { domain: domain.trim() })
            onClose(true)
        } catch (err) {
            setError(err.response?.data?.error || t('template.create_host_failed'))
        } finally {
            setCreating(false)
        }
    }

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose(false)}>
            <Dialog.Content maxWidth="420px" style={{ background: 'var(--cp-card)' }}>
                <Dialog.Title>{t('template.create_host_title')}</Dialog.Title>
                <Flex direction="column" gap="4" mt="2">
                    {error && (
                        <Callout.Root color="red" size="1">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{error}</Callout.Text>
                        </Callout.Root>
                    )}
                    <Text size="2" color="gray">{t('template.create_host_hint')}</Text>
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('host.domain')}</Text>
                        <TextField.Root
                            placeholder={t('template.domain_placeholder')}
                            value={domain}
                            onChange={(e) => setDomain(e.target.value)}
                            size="2"
                            onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
                        />
                    </Flex>
                    <Flex gap="3" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button onClick={handleCreate} disabled={creating || !domain.trim()}>
                            {creating ? <Spinner size="1" /> : <Globe size={14} />}
                            {creating ? t('template.creating_host') : t('template.create_host')}
                        </Button>
                    </Flex>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}

// ============ Template Form Dialog ============
function TemplateFormDialog({ open, onClose, template, t }) {
    const isEdit = !!template
    const [name, setName] = useState('')
    const [description, setDescription] = useState('')
    const [config, setConfig] = useState('')
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')

    useEffect(() => {
        if (open) {
            if (template) {
                setName(template.name)
                setDescription(template.description || '')
                setConfig(template.config || '')
            } else {
                setName('')
                setDescription('')
                setConfig('')
            }
            setError('')
            setSaving(false)
        }
    }, [open, template])

    const handleSave = async () => {
        setError('')
        setSaving(true)
        try {
            if (isEdit) {
                await templateAPI.update(template.id, { name, description, config })
            } else {
                await templateAPI.create({ name, description, config })
            }
            onClose(true)
        } catch (err) {
            setError(err.response?.data?.error || t('common.save_failed'))
        } finally {
            setSaving(false)
        }
    }

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose(false)}>
            <Dialog.Content maxWidth="520px" style={{ background: 'var(--cp-card)' }}>
                <Dialog.Title>{isEdit ? t('template.edit') : t('template.add')}</Dialog.Title>
                <Flex direction="column" gap="4" mt="2">
                    {error && (
                        <Callout.Root color="red" size="1">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{error}</Callout.Text>
                        </Callout.Root>
                    )}
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('template.name')}</Text>
                        <TextField.Root
                            placeholder={t('template.name_placeholder')}
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            size="2"
                        />
                    </Flex>
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('template.description')}</Text>
                        <TextField.Root
                            placeholder={t('template.description_placeholder')}
                            value={description}
                            onChange={(e) => setDescription(e.target.value)}
                            size="2"
                        />
                    </Flex>
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('template.config')}</Text>
                        <textarea
                            className="custom-textarea"
                            placeholder={t('template.config_placeholder')}
                            value={config}
                            onChange={(e) => setConfig(e.target.value)}
                            rows={8}
                        />
                    </Flex>
                    <Flex gap="3" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button onClick={handleSave} disabled={saving || !name.trim() || !config.trim()}>
                            {saving ? <Spinner size="1" /> : null}
                            {isEdit ? t('common.save') : t('common.create')}
                        </Button>
                    </Flex>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}

// ============ Delete Confirmation ============
function DeleteTemplateDialog({ open, onClose, template, t }) {
    const [deleting, setDeleting] = useState(false)
    const handleDelete = async () => {
        setDeleting(true)
        try {
            await templateAPI.delete(template.id)
            onClose(true)
        } catch {
            onClose(false)
        } finally {
            setDeleting(false)
        }
    }
    return (
        <AlertDialog.Root open={open} onOpenChange={(o) => !o && onClose(false)}>
            <AlertDialog.Content maxWidth="400px" style={{ background: 'var(--cp-card)' }}>
                <AlertDialog.Title>{t('common.delete')}</AlertDialog.Title>
                <AlertDialog.Description size="2">
                    {t('template.delete_confirm', { name: template?.name })}
                </AlertDialog.Description>
                <Flex gap="3" mt="4" justify="end">
                    <AlertDialog.Cancel>
                        <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                    </AlertDialog.Cancel>
                    <AlertDialog.Action>
                        <Button color="red" onClick={handleDelete} disabled={deleting}>
                            {deleting ? <Spinner size="1" /> : <Trash2 size={14} />}
                            {t('common.delete')}
                        </Button>
                    </AlertDialog.Action>
                </Flex>
            </AlertDialog.Content>
        </AlertDialog.Root>
    )
}

// ============ Template Card ============
function TemplateCard({ tpl, t, onEdit, onDelete, onExport, onCreateHost }) {
    const isPreset = tpl.type === 'preset'
    const display = isPreset ? getPresetDisplay(tpl, t) : { name: tpl.name, description: tpl.description }

    return (
        <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
            <Flex direction="column" gap="2" p="1">
                <Flex justify="between" align="start">
                    <Flex align="center" gap="2">
                        <Layers size={16} style={{ color: isPreset ? '#10b981' : '#3b82f6' }} />
                        <Text size="3" weight="bold" style={{ color: 'var(--cp-text)' }}>
                            {display.name}
                        </Text>
                    </Flex>
                    <Badge
                        color={isPreset ? 'green' : 'blue'}
                        variant="soft"
                        size="1"
                    >
                        {isPreset ? t('template.preset_badge') : t('template.custom_badge')}
                    </Badge>
                </Flex>

                {display.description && (
                    <Text size="2" color="gray" style={{ minHeight: 20 }}>
                        {display.description}
                    </Text>
                )}

                <Flex gap="2" mt="2" wrap="wrap">
                    <Tooltip content={t('template.create_host')}>
                        <Button variant="soft" size="1" onClick={() => onCreateHost(tpl)}>
                            <Globe size={14} /> {t('template.create_host')}
                        </Button>
                    </Tooltip>
                    <Tooltip content={t('template.export')}>
                        <IconButton variant="soft" size="1" onClick={() => onExport(tpl)}>
                            <Download size={14} />
                        </IconButton>
                    </Tooltip>
                    {!isPreset && (
                        <>
                            <Tooltip content={t('common.edit')}>
                                <IconButton variant="soft" size="1" onClick={() => onEdit(tpl)}>
                                    <Pencil size={14} />
                                </IconButton>
                            </Tooltip>
                            <Tooltip content={t('common.delete')}>
                                <IconButton variant="soft" color="red" size="1" onClick={() => onDelete(tpl)}>
                                    <Trash2 size={14} />
                                </IconButton>
                            </Tooltip>
                        </>
                    )}
                </Flex>
            </Flex>
        </Card>
    )
}

// ============ Templates Page ============
export default function Templates() {
    const { t } = useTranslation()
    const [templates, setTemplates] = useState([])
    const [loading, setLoading] = useState(true)
    const [showForm, setShowForm] = useState(false)
    const [editTemplate, setEditTemplate] = useState(null)
    const [deleteTemplate, setDeleteTemplate] = useState(null)
    const [createHostTemplate, setCreateHostTemplate] = useState(null)
    const fileInputRef = useRef(null)

    const fetchTemplates = useCallback(async () => {
        try {
            const res = await templateAPI.list()
            setTemplates(res.data.templates || [])
        } catch (err) {
            console.error('Failed to fetch templates:', err)
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => { fetchTemplates() }, [fetchTemplates])

    const handleExport = async (tpl) => {
        try {
            const res = await templateAPI.export(tpl.id)
            const url = window.URL.createObjectURL(new Blob([res.data]))
            const a = document.createElement('a')
            a.href = url
            a.download = `template_${tpl.id}.json`
            document.body.appendChild(a)
            a.click()
            a.remove()
            window.URL.revokeObjectURL(url)
        } catch (err) {
            console.error('Failed to export template:', err)
        }
    }

    const handleImport = async (e) => {
        const file = e.target.files?.[0]
        if (!file) return
        try {
            const fd = new FormData()
            fd.append('file', file)
            await templateAPI.import(fd)
            fetchTemplates()
        } catch (err) {
            console.error('Failed to import template:', err)
        }
        if (fileInputRef.current) fileInputRef.current.value = ''
    }

    const openCreate = () => { setEditTemplate(null); setShowForm(true) }
    const openEdit = (tpl) => { setEditTemplate(tpl); setShowForm(true) }

    const presets = templates.filter(t => t.type === 'preset')
    const customs = templates.filter(t => t.type === 'custom')

    return (
        <Box>
            <Flex justify="between" align="center" mb="5">
                <Box>
                    <Heading size="6" style={{ color: 'var(--cp-text)' }}>{t('template.title')}</Heading>
                    <Text size="2" color="gray">{t('template.subtitle')}</Text>
                </Box>
                <Flex gap="2">
                    <Button variant="soft" size="2" onClick={() => fileInputRef.current?.click()}>
                        <Upload size={16} /> {t('template.import')}
                    </Button>
                    <input
                        ref={fileInputRef}
                        type="file"
                        accept=".json"
                        onChange={handleImport}
                        style={{ display: 'none' }}
                    />
                    <Button size="2" onClick={openCreate}>
                        <Plus size={16} /> {t('template.add')}
                    </Button>
                </Flex>
            </Flex>

            {loading ? (
                <Flex justify="center" p="9"><Spinner size="3" /></Flex>
            ) : templates.length === 0 ? (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex direction="column" align="center" gap="3" p="6">
                        <FileJson size={48} strokeWidth={1} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="3" color="gray">{t('template.no_templates')}</Text>
                        <Button onClick={openCreate}>
                            <Plus size={16} /> {t('template.add')}
                        </Button>
                    </Flex>
                </Card>
            ) : (
                <Flex direction="column" gap="5">
                    {presets.length > 0 && (
                        <Box>
                            <Text size="3" weight="bold" mb="3" style={{ color: 'var(--cp-text)' }}>
                                {t('template.type_preset')}
                            </Text>
                            <Box style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 12 }} mt="2">
                                {presets.map(tpl => (
                                    <TemplateCard
                                        key={tpl.id}
                                        tpl={tpl}
                                        t={t}
                                        onEdit={openEdit}
                                        onDelete={setDeleteTemplate}
                                        onExport={handleExport}
                                        onCreateHost={setCreateHostTemplate}
                                    />
                                ))}
                            </Box>
                        </Box>
                    )}
                    {customs.length > 0 && (
                        <Box>
                            <Text size="3" weight="bold" mb="3" style={{ color: 'var(--cp-text)' }}>
                                {t('template.type_custom')}
                            </Text>
                            <Box style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 12 }} mt="2">
                                {customs.map(tpl => (
                                    <TemplateCard
                                        key={tpl.id}
                                        tpl={tpl}
                                        t={t}
                                        onEdit={openEdit}
                                        onDelete={setDeleteTemplate}
                                        onExport={handleExport}
                                        onCreateHost={setCreateHostTemplate}
                                    />
                                ))}
                            </Box>
                        </Box>
                    )}
                </Flex>
            )}

            <TemplateFormDialog
                open={showForm}
                onClose={(saved) => { setShowForm(false); if (saved) fetchTemplates() }}
                template={editTemplate}
                t={t}
            />

            <DeleteTemplateDialog
                open={!!deleteTemplate}
                onClose={(deleted) => { setDeleteTemplate(null); if (deleted) fetchTemplates() }}
                template={deleteTemplate}
                t={t}
            />

            <CreateHostDialog
                open={!!createHostTemplate}
                onClose={(created) => { setCreateHostTemplate(null) }}
                template={createHostTemplate}
                t={t}
            />
        </Box>
    )
}
