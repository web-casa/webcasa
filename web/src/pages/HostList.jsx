import { useState, useEffect, useCallback } from 'react'
import {
    Box, Flex, Heading, Text, Button, Badge, Switch, Table, Dialog,
    TextField, Callout, IconButton, Card, Tooltip, Spinner, AlertDialog,
} from '@radix-ui/themes'
import {
    Plus, Pencil, Trash2, Power, Globe, AlertCircle, X, ChevronRight,
} from 'lucide-react'
import { hostAPI } from '../api/index.js'

// ============ Host Form Dialog ============
function HostFormDialog({ open, onClose, host, onSaved }) {
    const isEdit = !!host
    const [form, setForm] = useState({
        domain: '',
        tls_enabled: true,
        http_redirect: true,
        websocket: false,
        enabled: true,
        upstreams: [{ address: '' }],
        custom_headers: [],
        access_rules: [],
    })
    const [error, setError] = useState('')
    const [saving, setSaving] = useState(false)

    useEffect(() => {
        if (host) {
            setForm({
                domain: host.domain,
                tls_enabled: host.tls_enabled,
                http_redirect: host.http_redirect,
                websocket: host.websocket,
                enabled: host.enabled,
                upstreams: host.upstreams?.length
                    ? host.upstreams.map((u) => ({ address: u.address }))
                    : [{ address: '' }],
                custom_headers: host.custom_headers || [],
                access_rules: host.access_rules || [],
            })
        } else {
            setForm({
                domain: '',
                tls_enabled: true,
                http_redirect: true,
                websocket: false,
                enabled: true,
                upstreams: [{ address: '' }],
                custom_headers: [],
                access_rules: [],
            })
        }
        setError('')
    }, [host, open])

    const handleSave = async () => {
        setError('')
        setSaving(true)
        try {
            const payload = {
                ...form,
                upstreams: form.upstreams.filter((u) => u.address.trim()),
            }
            if (isEdit) {
                await hostAPI.update(host.id, payload)
            } else {
                await hostAPI.create(payload)
            }
            onSaved()
            onClose()
        } catch (err) {
            setError(err.response?.data?.error || 'Failed to save host')
        } finally {
            setSaving(false)
        }
    }

    const addUpstream = () => {
        setForm({ ...form, upstreams: [...form.upstreams, { address: '' }] })
    }

    const removeUpstream = (idx) => {
        const upstreams = form.upstreams.filter((_, i) => i !== idx)
        setForm({ ...form, upstreams: upstreams.length ? upstreams : [{ address: '' }] })
    }

    const updateUpstream = (idx, value) => {
        const upstreams = [...form.upstreams]
        upstreams[idx] = { address: value }
        setForm({ ...form, upstreams })
    }

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <Dialog.Content maxWidth="520px" style={{ background: '#111113' }}>
                <Dialog.Title>{isEdit ? 'Edit Proxy Host' : 'New Proxy Host'}</Dialog.Title>
                <Dialog.Description size="2" color="gray" mb="4">
                    {isEdit ? 'Modify reverse proxy settings' : 'Add a new reverse proxy host'}
                </Dialog.Description>

                <Flex direction="column" gap="4">
                    {error && (
                        <Callout.Root color="red" size="1">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{error}</Callout.Text>
                        </Callout.Root>
                    )}

                    {/* Domain */}
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">Domain</Text>
                        <TextField.Root
                            placeholder="example.com"
                            value={form.domain}
                            onChange={(e) => setForm({ ...form, domain: e.target.value })}
                            size="2"
                        />
                    </Flex>

                    {/* Upstreams */}
                    <Flex direction="column" gap="2">
                        <Flex justify="between" align="center">
                            <Text size="2" weight="medium">Upstream Servers</Text>
                            <Button variant="ghost" size="1" onClick={addUpstream}>
                                <Plus size={14} /> Add
                            </Button>
                        </Flex>
                        {form.upstreams.map((u, i) => (
                            <Flex key={i} gap="2" align="center">
                                <TextField.Root
                                    style={{ flex: 1 }}
                                    placeholder="localhost:3000"
                                    value={u.address}
                                    onChange={(e) => updateUpstream(i, e.target.value)}
                                    size="2"
                                />
                                {form.upstreams.length > 1 && (
                                    <IconButton
                                        variant="ghost"
                                        color="red"
                                        size="1"
                                        onClick={() => removeUpstream(i)}
                                    >
                                        <X size={14} />
                                    </IconButton>
                                )}
                            </Flex>
                        ))}
                    </Flex>

                    {/* Toggles */}
                    <Card style={{ background: '#18181b', border: '1px solid #27272a' }}>
                        <Flex direction="column" gap="3" p="1">
                            <Flex justify="between" align="center">
                                <Flex direction="column">
                                    <Text size="2" weight="medium">HTTPS (TLS)</Text>
                                    <Text size="1" color="gray">Auto-obtain Let's Encrypt certificate</Text>
                                </Flex>
                                <Switch
                                    checked={form.tls_enabled}
                                    onCheckedChange={(v) => setForm({ ...form, tls_enabled: v })}
                                />
                            </Flex>

                            <Flex justify="between" align="center">
                                <Flex direction="column">
                                    <Text size="2" weight="medium">HTTP → HTTPS Redirect</Text>
                                    <Text size="1" color="gray">Force redirect HTTP to HTTPS</Text>
                                </Flex>
                                <Switch
                                    checked={form.http_redirect}
                                    onCheckedChange={(v) => setForm({ ...form, http_redirect: v })}
                                />
                            </Flex>

                            <Flex justify="between" align="center">
                                <Flex direction="column">
                                    <Text size="2" weight="medium">WebSocket Support</Text>
                                    <Text size="1" color="gray">Enable WebSocket proxy</Text>
                                </Flex>
                                <Switch
                                    checked={form.websocket}
                                    onCheckedChange={(v) => setForm({ ...form, websocket: v })}
                                />
                            </Flex>
                        </Flex>
                    </Card>
                </Flex>

                <Flex gap="3" mt="5" justify="end">
                    <Dialog.Close>
                        <Button variant="soft" color="gray">Cancel</Button>
                    </Dialog.Close>
                    <Button onClick={handleSave} disabled={saving || !form.domain}>
                        {saving ? <Spinner size="1" /> : null}
                        {isEdit ? 'Save Changes' : 'Create Host'}
                    </Button>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}

// ============ Delete Confirmation ============
function DeleteDialog({ open, onClose, host, onConfirm }) {
    const [deleting, setDeleting] = useState(false)
    const handleDelete = async () => {
        setDeleting(true)
        await onConfirm()
        setDeleting(false)
    }
    return (
        <AlertDialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <AlertDialog.Content maxWidth="400px" style={{ background: '#111113' }}>
                <AlertDialog.Title>Delete Host</AlertDialog.Title>
                <AlertDialog.Description size="2">
                    Are you sure you want to delete <strong>{host?.domain}</strong>? This action cannot be
                    undone and the Caddyfile will be updated immediately.
                </AlertDialog.Description>
                <Flex gap="3" mt="4" justify="end">
                    <AlertDialog.Cancel>
                        <Button variant="soft" color="gray">Cancel</Button>
                    </AlertDialog.Cancel>
                    <AlertDialog.Action>
                        <Button color="red" onClick={handleDelete} disabled={deleting}>
                            {deleting ? <Spinner size="1" /> : <Trash2 size={14} />}
                            Delete
                        </Button>
                    </AlertDialog.Action>
                </Flex>
            </AlertDialog.Content>
        </AlertDialog.Root>
    )
}

// ============ Host List Page ============
export default function HostList() {
    const [hosts, setHosts] = useState([])
    const [loading, setLoading] = useState(true)
    const [editHost, setEditHost] = useState(null)
    const [showForm, setShowForm] = useState(false)
    const [deleteHost, setDeleteHost] = useState(null)
    const [toggling, setToggling] = useState(null)

    const fetchHosts = useCallback(async () => {
        try {
            const res = await hostAPI.list()
            setHosts(res.data.hosts || [])
        } catch (err) {
            console.error('Failed to fetch hosts:', err)
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => {
        fetchHosts()
    }, [fetchHosts])

    const handleToggle = async (host) => {
        setToggling(host.id)
        try {
            await hostAPI.toggle(host.id)
            fetchHosts()
        } catch (err) {
            console.error('Failed to toggle host:', err)
        } finally {
            setToggling(null)
        }
    }

    const handleDelete = async () => {
        try {
            await hostAPI.delete(deleteHost.id)
            setDeleteHost(null)
            fetchHosts()
        } catch (err) {
            console.error('Failed to delete host:', err)
        }
    }

    const openCreate = () => {
        setEditHost(null)
        setShowForm(true)
    }

    const openEdit = (host) => {
        setEditHost(host)
        setShowForm(true)
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="5">
                <Box>
                    <Heading size="6" style={{ color: '#fafafa' }}>Proxy Hosts</Heading>
                    <Text size="2" color="gray">
                        Manage your reverse proxy configurations
                    </Text>
                </Box>
                <Button size="2" onClick={openCreate}>
                    <Plus size={16} />
                    Add Host
                </Button>
            </Flex>

            {loading ? (
                <Flex justify="center" p="9">
                    <Spinner size="3" />
                </Flex>
            ) : hosts.length === 0 ? (
                <Card style={{ background: '#111113', border: '1px solid #1e1e22' }}>
                    <Flex direction="column" align="center" gap="3" p="6">
                        <Globe size={48} strokeWidth={1} color="#3f3f46" />
                        <Text size="3" color="gray">No proxy hosts configured</Text>
                        <Button onClick={openCreate}>
                            <Plus size={16} /> Add Your First Host
                        </Button>
                    </Flex>
                </Card>
            ) : (
                <Card style={{ background: '#111113', border: '1px solid #1e1e22', padding: 0 }}>
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>Domain</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>Upstream</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>TLS</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>Status</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell style={{ width: 120 }}>Actions</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {hosts.map((host) => (
                                <Table.Row
                                    key={host.id}
                                    style={{ opacity: host.enabled ? 1 : 0.5 }}
                                >
                                    <Table.Cell>
                                        <Flex align="center" gap="2">
                                            <Globe size={14} color="#10b981" />
                                            <Text weight="medium">{host.domain}</Text>
                                        </Flex>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Flex direction="column" gap="1">
                                            {(host.upstreams || []).map((u, i) => (
                                                <Flex key={i} align="center" gap="1">
                                                    <ChevronRight size={12} color="#52525b" />
                                                    <Text size="2" color="gray">{u.address}</Text>
                                                </Flex>
                                            ))}
                                        </Flex>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Badge
                                            color={host.tls_enabled ? 'green' : 'gray'}
                                            variant="soft"
                                            size="1"
                                        >
                                            {host.tls_enabled ? 'HTTPS' : 'HTTP'}
                                        </Badge>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Tooltip content={host.enabled ? 'Click to disable' : 'Click to enable'}>
                                            <Switch
                                                checked={host.enabled}
                                                onCheckedChange={() => handleToggle(host)}
                                                disabled={toggling === host.id}
                                                size="1"
                                            />
                                        </Tooltip>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Flex gap="2">
                                            <Tooltip content="Edit">
                                                <IconButton
                                                    variant="ghost"
                                                    size="1"
                                                    onClick={() => openEdit(host)}
                                                >
                                                    <Pencil size={14} />
                                                </IconButton>
                                            </Tooltip>
                                            <Tooltip content="Delete">
                                                <IconButton
                                                    variant="ghost"
                                                    color="red"
                                                    size="1"
                                                    onClick={() => setDeleteHost(host)}
                                                >
                                                    <Trash2 size={14} />
                                                </IconButton>
                                            </Tooltip>
                                        </Flex>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                </Card>
            )}

            {/* Form Dialog */}
            <HostFormDialog
                open={showForm}
                onClose={() => setShowForm(false)}
                host={editHost}
                onSaved={fetchHosts}
            />

            {/* Delete Confirmation */}
            <DeleteDialog
                open={!!deleteHost}
                onClose={() => setDeleteHost(null)}
                host={deleteHost}
                onConfirm={handleDelete}
            />
        </Box>
    )
}
