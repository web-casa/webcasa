import { useState, useEffect } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Badge, Callout, Code,
    TextField, IconButton, Dialog, AlertDialog, Spinner,
    Select, Table, Tooltip,
} from '@radix-ui/themes'
import {
    AlertCircle, CheckCircle2, Copy, Plus, Trash2, Check, Cpu, Key,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { mcpAPI } from '../api/index.js'

export default function MCPManager() {
    const { t } = useTranslation()
    const [tokens, setTokens] = useState([])
    const [loading, setLoading] = useState(true)
    const [createOpen, setCreateOpen] = useState(false)
    const [form, setForm] = useState({ name: '', expires_in: 0, permissions: [] })
    const [creating, setCreating] = useState(false)
    const [newToken, setNewToken] = useState(null)
    const [copied, setCopied] = useState(false)
    const [deleteTarget, setDeleteTarget] = useState(null)
    const [message, setMessage] = useState(null)

    const showMsg = (type, text) => { setMessage({ type, text }); setTimeout(() => setMessage(null), 5000) }

    const fetchTokens = async () => {
        try { const res = await mcpAPI.listTokens(); setTokens(res.data.tokens || res.data || []) }
        catch { /* ignore */ }
        finally { setLoading(false) }
    }

    useEffect(() => { fetchTokens() }, [])

    const handleCreate = async () => {
        if (!form.name.trim()) return
        setCreating(true)
        try {
            const res = await mcpAPI.createToken({ name: form.name.trim(), permissions: form.permissions, expires_in: form.expires_in })
            setNewToken(res.data.token)
            setCopied(false)
            setForm({ name: '', expires_in: 0, permissions: [] })
            fetchTokens()
        } catch (err) {
            showMsg('error', err.response?.data?.error || t('common.operation_failed'))
            setCreateOpen(false)
        } finally { setCreating(false) }
    }

    const handleDelete = async () => {
        if (!deleteTarget) return
        try { await mcpAPI.deleteToken(deleteTarget.id); showMsg('success', t('mcp.token_revoked')); setDeleteTarget(null); fetchTokens() }
        catch (err) { showMsg('error', err.response?.data?.error || t('common.delete_failed')); setDeleteTarget(null) }
    }

    const handleCopy = (text) => {
        navigator.clipboard.writeText(text)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
    }

    const mcpEndpoint = `${window.location.origin}/api/plugins/mcpserver/mcp`

    return (
        <Box>
            <Flex justify="between" align="center" mb="4">
                <Flex direction="column" gap="1">
                    <Heading size="5">{t('mcp.title')}</Heading>
                    <Text size="2" color="gray">{t('mcp.subtitle')}</Text>
                </Flex>
                <Button size="2" onClick={() => { setCreateOpen(true); setNewToken(null) }}>
                    <Plus size={14} /> {t('mcp.create_token')}
                </Button>
            </Flex>

            {message && (
                <Callout.Root color={message.type === 'success' ? 'green' : 'red'} size="1" mb="4">
                    <Callout.Icon>{message.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}</Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            {/* MCP Endpoint Info */}
            <Card mb="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Flex direction="column" gap="3">
                    <Flex align="center" gap="2">
                        <Cpu size={16} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="2" weight="bold">{t('mcp.mcp_endpoint')}</Text>
                    </Flex>
                    <Flex align="center" gap="2">
                        <Code size="2" style={{ flex: 1, fontFamily: 'monospace', padding: '8px 12px', background: 'var(--cp-code-bg)', borderRadius: 6 }}>
                            {mcpEndpoint}
                        </Code>
                        <Tooltip content={t('common.copy')}>
                            <IconButton size="1" variant="ghost" onClick={() => { navigator.clipboard.writeText(mcpEndpoint) }}>
                                <Copy size={14} />
                            </IconButton>
                        </Tooltip>
                    </Flex>
                    <Text size="1" color="gray">{t('mcp.ide_config')}</Text>
                    <Box style={{ background: 'var(--cp-code-bg)', border: '1px solid var(--cp-border)', borderRadius: 8, padding: 12 }}>
                        <pre className="log-viewer" style={{ margin: 0, fontSize: 12, color: 'var(--cp-text)' }}>{`{
  "mcpServers": {
    "webcasa": {
      "type": "streamable-http",
      "url": "${mcpEndpoint}",
      "headers": {
        "Authorization": "Bearer wc_YOUR_TOKEN_HERE"
      }
    }
  }
}`}</pre>
                    </Box>
                </Flex>
            </Card>

            {/* Token List */}
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                {loading ? (
                    <Flex justify="center" p="6"><Spinner size="3" /></Flex>
                ) : tokens.length === 0 ? (
                    <Flex direction="column" align="center" justify="center" py="8">
                        <Key size={40} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="2" color="gray" mt="3">{t('mcp.no_tokens')}</Text>
                    </Flex>
                ) : (
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('mcp.token_name')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('mcp.token_prefix')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('mcp.created_at')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('mcp.last_used')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('mcp.expires')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell width="60"></Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {tokens.map((token) => (
                                <Table.Row key={token.id}>
                                    <Table.Cell><Flex align="center" gap="2"><Key size={14} style={{ color: 'var(--cp-text-muted)' }} /><Text weight="medium">{token.name}</Text></Flex></Table.Cell>
                                    <Table.Cell><Code size="1" style={{ fontFamily: 'monospace' }}>{token.prefix}...</Code></Table.Cell>
                                    <Table.Cell><Text size="1" color="gray">{new Date(token.created_at).toLocaleDateString()}</Text></Table.Cell>
                                    <Table.Cell><Text size="1" color="gray">{token.last_used_at ? new Date(token.last_used_at).toLocaleDateString() : t('mcp.never_used')}</Text></Table.Cell>
                                    <Table.Cell>
                                        {token.expires_at
                                            ? <Badge color={new Date(token.expires_at) < new Date() ? 'red' : 'gray'} size="1" variant="soft">
                                                {new Date(token.expires_at).toLocaleDateString()}
                                              </Badge>
                                            : <Badge color="green" size="1" variant="soft">{t('mcp.no_expiry')}</Badge>
                                        }
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Tooltip content={t('mcp.revoke_token')}>
                                            <IconButton size="1" variant="ghost" color="red" onClick={() => setDeleteTarget(token)}>
                                                <Trash2 size={14} />
                                            </IconButton>
                                        </Tooltip>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                )}
            </Card>

            {/* Create Token Dialog */}
            <Dialog.Root open={createOpen} onOpenChange={(o) => { if (!o) { setCreateOpen(false); setNewToken(null) } }}>
                <Dialog.Content maxWidth="500px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>{t('mcp.create_token')}</Dialog.Title>

                    {newToken ? (
                        <Flex direction="column" gap="3" mt="3">
                            <Callout.Root color="orange" size="1">
                                <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                                <Callout.Text>{t('mcp.token_created_warning')}</Callout.Text>
                            </Callout.Root>
                            <Text size="2" weight="medium">{t('mcp.your_token')}:</Text>
                            <Flex align="center" gap="2">
                                <Code size="2" style={{ flex: 1, fontFamily: 'monospace', padding: '10px 14px', background: 'var(--cp-code-bg)', borderRadius: 6, wordBreak: 'break-all' }}>
                                    {newToken}
                                </Code>
                                <Button variant="soft" size="2" onClick={() => handleCopy(newToken)}>
                                    {copied ? <><Check size={14} /> {t('common.copied')}</> : <><Copy size={14} /> {t('common.copy')}</>}
                                </Button>
                            </Flex>
                            <Flex justify="end" mt="2">
                                <Button onClick={() => { setCreateOpen(false); setNewToken(null) }}>{t('common.close')}</Button>
                            </Flex>
                        </Flex>
                    ) : (
                        <Flex direction="column" gap="3" mt="3">
                            <Flex direction="column" gap="1">
                                <Text size="2" weight="medium">{t('mcp.token_name')}</Text>
                                <TextField.Root placeholder={t('mcp.token_name_placeholder')} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
                            </Flex>
                            <Flex direction="column" gap="1">
                                <Text size="2" weight="medium">{t('mcp.expires_in')}</Text>
                                <Select.Root value={String(form.expires_in)} onValueChange={(v) => setForm({ ...form, expires_in: Number(v) })}>
                                    <Select.Trigger />
                                    <Select.Content>
                                        <Select.Item value="0">{t('mcp.no_expiry')}</Select.Item>
                                        <Select.Item value="30">{t('mcp.days_30')}</Select.Item>
                                        <Select.Item value="90">{t('mcp.days_90')}</Select.Item>
                                        <Select.Item value="180">{t('mcp.days_180')}</Select.Item>
                                        <Select.Item value="365">{t('mcp.days_365')}</Select.Item>
                                    </Select.Content>
                                </Select.Root>
                            </Flex>
                            <Flex direction="column" gap="1">
                                <Text size="2" weight="medium">{t('mcp.permissions', 'Permissions')}</Text>
                                <Text size="1" color="gray">{t('mcp.permissions_hint', 'Leave empty for full access')}</Text>
                                <Flex wrap="wrap" gap="2" mt="1">
                                    {[
                                        { scope: 'hosts:read', label: 'Hosts Read' }, { scope: 'hosts:write', label: 'Hosts Write' },
                                        { scope: 'deploy:read', label: 'Deploy Read' }, { scope: 'deploy:write', label: 'Deploy Write' },
                                        { scope: 'docker:read', label: 'Docker Read' }, { scope: 'docker:write', label: 'Docker Write' },
                                        { scope: 'database:read', label: 'Database Read' }, { scope: 'database:write', label: 'Database Write' },
                                        { scope: 'monitoring:read', label: 'Monitoring Read' }, { scope: 'monitoring:write', label: 'Monitoring Write' },
                                        { scope: 'files:read', label: 'Files Read' }, { scope: 'files:write', label: 'Files Write' },
                                        { scope: 'system:read', label: 'System Read' }, { scope: 'system:write', label: 'System Write' },
                                        { scope: 'backup:write', label: 'Backup' },
                                        { scope: 'firewall:read', label: 'Firewall Read' }, { scope: 'firewall:write', label: 'Firewall Write' },
                                        { scope: 'notify:read', label: 'Notify Read' },
                                        { scope: 'ai:read', label: 'AI' },
                                    ].map(({ scope, label }) => (
                                        <label key={scope} style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 12, cursor: 'pointer' }}>
                                            <input type="checkbox" checked={form.permissions.includes(scope)}
                                                onChange={(e) => {
                                                    const perms = e.target.checked
                                                        ? [...form.permissions, scope]
                                                        : form.permissions.filter(p => p !== scope)
                                                    setForm({ ...form, permissions: perms })
                                                }} />
                                            {label}
                                        </label>
                                    ))}
                                </Flex>
                            </Flex>
                            <Flex gap="3" mt="2" justify="end">
                                <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                                <Button onClick={handleCreate} disabled={creating || !form.name.trim()}>
                                    {creating ? t('common.creating') : t('common.create')}
                                </Button>
                            </Flex>
                        </Flex>
                    )}
                </Dialog.Content>
            </Dialog.Root>

            {/* Revoke Confirmation */}
            <AlertDialog.Root open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
                <AlertDialog.Content maxWidth="400px">
                    <AlertDialog.Title>{t('mcp.revoke_title')}</AlertDialog.Title>
                    <AlertDialog.Description>{t('mcp.revoke_confirm', { name: deleteTarget?.name })}</AlertDialog.Description>
                    <Flex gap="3" mt="4" justify="end">
                        <AlertDialog.Cancel><Button variant="soft" color="gray">{t('common.cancel')}</Button></AlertDialog.Cancel>
                        <AlertDialog.Action><Button color="red" onClick={handleDelete}>{t('mcp.revoke_token')}</Button></AlertDialog.Action>
                    </Flex>
                </AlertDialog.Content>
            </AlertDialog.Root>
        </Box>
    )
}
