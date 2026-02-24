import { useState, useEffect, useCallback } from 'react'
import {
    Box, Flex, Text, Button, Card, Badge, Dialog, TextField,
    Select, Switch, Table, IconButton, Callout, Tooltip,
} from '@radix-ui/themes'
import { Plus, Pencil, Trash2, Shield, AlertCircle, Star } from 'lucide-react'
import { dnsProviderAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

// Provider 配置字段定义
const getProviderFields = (t) => ({
    cloudflare: {
        label: t('dns.cloudflare'),
        fields: [{ key: 'api_token', label: t('dns.api_token'), placeholder: 'Cloudflare API Token' }],
    },
    alidns: {
        label: t('dns.alidns'),
        fields: [
            { key: 'access_key_id', label: t('dns.access_key_id'), placeholder: 'LTAI...' },
            { key: 'access_key_secret', label: t('dns.access_key_secret'), placeholder: 'AccessKeySecret' },
        ],
    },
    tencentcloud: {
        label: t('dns.tencentcloud'),
        fields: [
            { key: 'secret_id', label: t('dns.secret_id'), placeholder: 'AKIDxxxxxxxx' },
            { key: 'secret_key', label: t('dns.secret_key'), placeholder: 'SecretKey' },
        ],
    },
    route53: {
        label: t('dns.route53'),
        fields: [
            { key: 'region', label: t('dns.region'), placeholder: 'us-east-1' },
            { key: 'access_key_id', label: t('dns.access_key_id'), placeholder: 'AKIA...' },
            { key: 'secret_access_key', label: t('dns.access_key_secret'), placeholder: 'SecretAccessKey' },
        ],
    },
})

const DEFAULT_FORM = { name: '', provider: 'cloudflare', config: {}, is_default: false }

export default function DnsProviders() {
    const { t } = useTranslation()
    const PROVIDER_FIELDS = getProviderFields(t)
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
        try {
            const res = await dnsProviderAPI.list()
            setProviders(res.data.providers || [])
        } catch { /* ignore */ }
        setLoading(false)
    }, [])

    useEffect(() => { load() }, [load])

    const openCreate = () => {
        setEditId(null)
        setForm({ ...DEFAULT_FORM })
        setError('')
        setDialogOpen(true)
    }

    const openEdit = async (p) => {
        setEditId(p.id)
        setError('')
        try {
            const res = await dnsProviderAPI.get(p.id)
            const data = res.data
            let cfg = {}
            try { cfg = JSON.parse(data.config) } catch { /* */ }
            setForm({
                name: data.name,
                provider: data.provider,
                config: cfg,
                is_default: data.is_default || false,
            })
        } catch {
            setForm({ name: p.name, provider: p.provider, config: {}, is_default: p.is_default || false })
        }
        setDialogOpen(true)
    }

    const handleSave = async () => {
        setError('')
        setSaving(true)
        try {
            const payload = {
                name: form.name,
                provider: form.provider,
                config: JSON.stringify(form.config),
                is_default: form.is_default,
            }
            if (editId) {
                await dnsProviderAPI.update(editId, payload)
            } else {
                await dnsProviderAPI.create(payload)
            }
            setDialogOpen(false)
            load()
        } catch (e) {
            setError(e.response?.data?.error || t('common.save_failed'))
        }
        setSaving(false)
    }

    const handleDelete = async () => {
        try {
            await dnsProviderAPI.delete(deleteId)
            setDeleteId(null)
            load()
        } catch (e) {
            alert(e.response?.data?.error || t('common.delete_failed'))
            setDeleteId(null)
        }
    }

    const setConfigField = (key, value) => {
        setForm({ ...form, config: { ...form.config, [key]: value } })
    }

    const providerDef = PROVIDER_FIELDS[form.provider]

    return (
        <Box>
            <Flex justify="between" align="center" mb="5">
                <Box>
                    <Text size="5" weight="bold" style={{ color: 'var(--cp-text)' }}>
                        {t('dns.title')}
                    </Text>
                    <Text size="2" color="gray" as="p">
                        {t('dns.subtitle')}
                    </Text>
                </Box>
                <Button
                    size="2"
                    onClick={openCreate}
                    style={{ background: 'linear-gradient(135deg, #10b981, #059669)' }}
                >
                    <Plus size={14} /> {t('dns.add_provider')}
                </Button>
            </Flex>

            {loading ? (
                <Text color="gray">{t('common.loading')}</Text>
            ) : providers.length === 0 ? (
                <Card style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                    <Flex direction="column" align="center" gap="3" py="6">
                        <Shield size={40} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text color="gray">{t('dns.no_providers')}</Text>
                        <Button variant="soft" size="2" onClick={openCreate}>
                            <Plus size={14} /> {t('dns.add_first')}
                        </Button>
                    </Flex>
                </Card>
            ) : (
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
                                <Table.Cell>
                                    <Flex align="center" gap="2">
                                        <Shield size={14} color="#10b981" />
                                        <Text weight="medium">{p.name}</Text>
                                    </Flex>
                                </Table.Cell>
                                <Table.Cell>
                                    <Badge variant="soft" size="1">
                                        {PROVIDER_FIELDS[p.provider]?.label || p.provider}
                                    </Badge>
                                </Table.Cell>
                                <Table.Cell>
                                    {p.is_default && (
                                        <Tooltip content={t('dns.default_provider_tooltip')}>
                                            <Star size={14} color="#f59e0b" fill="#f59e0b" />
                                        </Tooltip>
                                    )}
                                </Table.Cell>
                                <Table.Cell>
                                    <Flex gap="2">
                                        <Tooltip content={t('common.edit')}>
                                            <IconButton variant="ghost" size="1" onClick={() => openEdit(p)}>
                                                <Pencil size={14} />
                                            </IconButton>
                                        </Tooltip>
                                        <Tooltip content={t('common.delete')}>
                                            <IconButton variant="ghost" size="1" color="red" onClick={() => setDeleteId(p.id)}>
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

            {/* Create/Edit Dialog */}
            <Dialog.Root open={dialogOpen} onOpenChange={(o) => !o && setDialogOpen(false)}>
                <Dialog.Content maxWidth="480px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>{editId ? t('dns.edit_provider') : t('dns.add_provider')}</Dialog.Title>
                    <Dialog.Description size="2" color="gray" mb="4">
                        {t('dns.dialog_description')}
                    </Dialog.Description>

                    <Flex direction="column" gap="3">
                        {error && (
                            <Callout.Root color="red" size="1">
                                <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                                <Callout.Text>{error}</Callout.Text>
                            </Callout.Root>
                        )}

                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('common.name')}</Text>
                            <TextField.Root
                                placeholder={t('dns.name_placeholder')}
                                value={form.name}
                                onChange={(e) => setForm({ ...form, name: e.target.value })}
                            />
                        </Flex>

                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('dns.provider_type')}</Text>
                            <Select.Root
                                value={form.provider}
                                onValueChange={(v) => setForm({ ...form, provider: v, config: {} })}
                            >
                                <Select.Trigger />
                                <Select.Content>
                                    {Object.entries(PROVIDER_FIELDS).map(([k, v]) => (
                                        <Select.Item key={k} value={k}>{v.label}</Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </Flex>

                        {providerDef && (
                            <Card style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                                <Flex direction="column" gap="2">
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>
                                        {t('dns.api_credentials')}
                                    </Text>
                                    {providerDef.fields.map((f) => (
                                        <Flex direction="column" gap="1" key={f.key}>
                                            <Text size="1" color="gray">{f.label}</Text>
                                            <TextField.Root
                                                type="password"
                                                placeholder={f.placeholder}
                                                value={form.config[f.key] || ''}
                                                onChange={(e) => setConfigField(f.key, e.target.value)}
                                            />
                                        </Flex>
                                    ))}
                                </Flex>
                            </Card>
                        )}

                        <Flex justify="between" align="center">
                            <Flex direction="column">
                                <Text size="2" weight="medium">{t('dns.set_default')}</Text>
                                <Text size="1" color="gray">{t('dns.set_default_hint')}</Text>
                            </Flex>
                            <Switch
                                checked={form.is_default}
                                onCheckedChange={(v) => setForm({ ...form, is_default: v })}
                            />
                        </Flex>
                    </Flex>

                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button
                            onClick={handleSave}
                            disabled={saving || !form.name || !form.provider}
                            style={{
                                background: 'linear-gradient(135deg, #10b981, #059669)',
                                cursor: saving ? 'not-allowed' : 'pointer',
                            }}
                        >
                            {saving ? t('common.saving') : t('common.save')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Delete confirm */}
            <Dialog.Root open={!!deleteId} onOpenChange={(o) => !o && setDeleteId(null)}>
                <Dialog.Content maxWidth="400px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>{t('dns.confirm_delete_title')}</Dialog.Title>
                    <Dialog.Description size="2" color="gray">
                        {t('dns.confirm_delete_desc')}
                    </Dialog.Description>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button color="red" onClick={handleDelete}>{t('dns.confirm_delete_btn')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
