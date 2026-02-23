import { useState, useEffect, useCallback } from 'react'
import {
    Box, Flex, Text, Button, Card, Badge, Dialog, TextField,
    Select, Switch, Table, IconButton, Callout, Tooltip,
} from '@radix-ui/themes'
import { Plus, Pencil, Trash2, Shield, AlertCircle, Star } from 'lucide-react'
import { dnsProviderAPI } from '../api/index.js'

// Provider 配置字段定义
const PROVIDER_FIELDS = {
    cloudflare: {
        label: 'Cloudflare',
        fields: [{ key: 'api_token', label: 'API Token', placeholder: 'Cloudflare API Token' }],
    },
    alidns: {
        label: '阿里云 DNS',
        fields: [
            { key: 'access_key_id', label: 'Access Key ID', placeholder: 'LTAI...' },
            { key: 'access_key_secret', label: 'Access Key Secret', placeholder: 'AccessKeySecret' },
        ],
    },
    tencentcloud: {
        label: '腾讯云 DNS',
        fields: [
            { key: 'secret_id', label: 'Secret ID', placeholder: 'AKIDxxxxxxxx' },
            { key: 'secret_key', label: 'Secret Key', placeholder: 'SecretKey' },
        ],
    },
    route53: {
        label: 'AWS Route 53',
        fields: [
            { key: 'region', label: 'Region', placeholder: 'us-east-1' },
            { key: 'access_key_id', label: 'Access Key ID', placeholder: 'AKIA...' },
            { key: 'secret_access_key', label: 'Secret Access Key', placeholder: 'SecretAccessKey' },
        ],
    },
}

const DEFAULT_FORM = { name: '', provider: 'cloudflare', config: {}, is_default: false }

export default function DnsProviders() {
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
            setError(e.response?.data?.error || '保存失败')
        }
        setSaving(false)
    }

    const handleDelete = async () => {
        try {
            await dnsProviderAPI.delete(deleteId)
            setDeleteId(null)
            load()
        } catch (e) {
            alert(e.response?.data?.error || '删除失败')
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
                        DNS Providers
                    </Text>
                    <Text size="2" color="gray" as="p">
                        管理 DNS API 提供商，用于 ACME DNS Challenge 申请证书
                    </Text>
                </Box>
                <Button
                    size="2"
                    onClick={openCreate}
                    style={{ background: 'linear-gradient(135deg, #10b981, #059669)' }}
                >
                    <Plus size={14} /> 添加 Provider
                </Button>
            </Flex>

            {loading ? (
                <Text color="gray">加载中...</Text>
            ) : providers.length === 0 ? (
                <Card style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                    <Flex direction="column" align="center" gap="3" py="6">
                        <Shield size={40} color="#52525b" />
                        <Text color="gray">还没有配置 DNS Provider</Text>
                        <Button variant="soft" size="2" onClick={openCreate}>
                            <Plus size={14} /> 添加第一个
                        </Button>
                    </Flex>
                </Card>
            ) : (
                <Table.Root>
                    <Table.Header>
                        <Table.Row>
                            <Table.ColumnHeaderCell>名称</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>Provider</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>默认</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell style={{ width: 100 }}>操作</Table.ColumnHeaderCell>
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
                                        <Tooltip content="默认 Provider">
                                            <Star size={14} color="#f59e0b" fill="#f59e0b" />
                                        </Tooltip>
                                    )}
                                </Table.Cell>
                                <Table.Cell>
                                    <Flex gap="2">
                                        <Tooltip content="编辑">
                                            <IconButton variant="ghost" size="1" onClick={() => openEdit(p)}>
                                                <Pencil size={14} />
                                            </IconButton>
                                        </Tooltip>
                                        <Tooltip content="删除">
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
                    <Dialog.Title>{editId ? '编辑 DNS Provider' : '添加 DNS Provider'}</Dialog.Title>
                    <Dialog.Description size="2" color="gray" mb="4">
                        配置 DNS API 凭据用于证书 DNS 验证
                    </Dialog.Description>

                    <Flex direction="column" gap="3">
                        {error && (
                            <Callout.Root color="red" size="1">
                                <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                                <Callout.Text>{error}</Callout.Text>
                            </Callout.Root>
                        )}

                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">名称</Text>
                            <TextField.Root
                                placeholder="我的 Cloudflare"
                                value={form.name}
                                onChange={(e) => setForm({ ...form, name: e.target.value })}
                            />
                        </Flex>

                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">Provider 类型</Text>
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
                                        API 凭据
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
                                <Text size="2" weight="medium">设为默认</Text>
                                <Text size="1" color="gray">新建站点自动使用此 Provider</Text>
                            </Flex>
                            <Switch
                                checked={form.is_default}
                                onCheckedChange={(v) => setForm({ ...form, is_default: v })}
                            />
                        </Flex>
                    </Flex>

                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">取消</Button>
                        </Dialog.Close>
                        <Button
                            onClick={handleSave}
                            disabled={saving || !form.name || !form.provider}
                            style={{
                                background: 'linear-gradient(135deg, #10b981, #059669)',
                                cursor: saving ? 'not-allowed' : 'pointer',
                            }}
                        >
                            {saving ? '保存中...' : '保存'}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Delete confirm */}
            <Dialog.Root open={!!deleteId} onOpenChange={(o) => !o && setDeleteId(null)}>
                <Dialog.Content maxWidth="400px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>确认删除</Dialog.Title>
                    <Dialog.Description size="2" color="gray">
                        删除后使用此 Provider 的站点将无法续签证书，确定要继续吗？
                    </Dialog.Description>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">取消</Button>
                        </Dialog.Close>
                        <Button color="red" onClick={handleDelete}>确认删除</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
