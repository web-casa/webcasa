import { useState, useEffect } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Table, Badge, Dialog,
    TextField, Select, IconButton, Callout,
} from '@radix-ui/themes'
import { Plus, Pencil, Trash2, Shield, Eye, AlertCircle, CheckCircle2 } from 'lucide-react'
import { userAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

export default function Users() {
    const { t } = useTranslation()
    const [users, setUsers] = useState([])
    const [loading, setLoading] = useState(true)
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editUser, setEditUser] = useState(null)
    const [form, setForm] = useState({ username: '', password: '', role: 'viewer' })
    const [message, setMessage] = useState(null)

    const fetchUsers = async () => {
        try {
            const res = await userAPI.list()
            setUsers(res.data.users || [])
        } catch (err) {
            console.error('Failed to fetch users:', err)
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { fetchUsers() }, [])

    const showMessage = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 3000)
    }

    const openCreate = () => {
        setEditUser(null)
        setForm({ username: '', password: '', role: 'viewer' })
        setDialogOpen(true)
    }

    const openEdit = (user) => {
        setEditUser(user)
        setForm({ username: user.username, password: '', role: user.role })
        setDialogOpen(true)
    }

    const handleSubmit = async () => {
        try {
            if (editUser) {
                const data = {}
                if (form.password) data.password = form.password
                if (form.role !== editUser.role) data.role = form.role
                await userAPI.update(editUser.id, data)
                showMessage('success', t('user.update_success'))
            } else {
                await userAPI.create(form)
                showMessage('success', t('user.create_success'))
            }
            setDialogOpen(false)
            fetchUsers()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.operation_failed'))
        }
    }

    const handleDelete = async (user) => {
        if (!confirm(t('user.confirm_delete', { username: user.username }))) return
        try {
            await userAPI.delete(user.id)
            showMessage('success', t('user.delete_success'))
            fetchUsers()
        } catch (err) {
            showMessage('error', err.response?.data?.error || t('common.delete_failed'))
        }
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="4">
                <Box>
                    <Heading size="6" style={{ color: 'var(--cp-text)' }}>{t('user.title')}</Heading>
                    <Text size="2" color="gray">{t('user.subtitle')}</Text>
                </Box>
                <Button onClick={openCreate}>
                    <Plus size={16} /> {t('user.add_user')}
                </Button>
            </Flex>

            {message && (
                <Callout.Root
                    color={message.type === 'error' ? 'red' : 'green'}
                    mb="4"
                >
                    <Callout.Icon>
                        {message.type === 'error' ? <AlertCircle size={16} /> : <CheckCircle2 size={16} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Table.Root>
                    <Table.Header>
                        <Table.Row>
                            <Table.ColumnHeaderCell>ID</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('user.username')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('user.role')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('user.created_at')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                        </Table.Row>
                    </Table.Header>
                    <Table.Body>
                        {users.map((user) => (
                            <Table.Row key={user.id}>
                                <Table.Cell>{user.id}</Table.Cell>
                                <Table.Cell>
                                    <Text weight="medium">{user.username}</Text>
                                </Table.Cell>
                                <Table.Cell>
                                    <Badge color={user.role === 'admin' ? 'blue' : 'gray'} size="1">
                                        {user.role === 'admin' ? (
                                            <Flex align="center" gap="1"><Shield size={12} /> {t('user.admin')}</Flex>
                                        ) : (
                                            <Flex align="center" gap="1"><Eye size={12} /> {t('user.viewer')}</Flex>
                                        )}
                                    </Badge>
                                </Table.Cell>
                                <Table.Cell>
                                    <Text size="1" color="gray">
                                        {new Date(user.created_at).toLocaleDateString()}
                                    </Text>
                                </Table.Cell>
                                <Table.Cell>
                                    <Flex gap="2">
                                        <IconButton
                                            size="1"
                                            variant="ghost"
                                            onClick={() => openEdit(user)}
                                        >
                                            <Pencil size={14} />
                                        </IconButton>
                                        <IconButton
                                            size="1"
                                            variant="ghost"
                                            color="red"
                                            onClick={() => handleDelete(user)}
                                        >
                                            <Trash2 size={14} />
                                        </IconButton>
                                    </Flex>
                                </Table.Cell>
                            </Table.Row>
                        ))}
                        {users.length === 0 && !loading && (
                            <Table.Row>
                                <Table.Cell colSpan={5}>
                                    <Text color="gray" size="2">{t('common.no_data')}</Text>
                                </Table.Cell>
                            </Table.Row>
                        )}
                    </Table.Body>
                </Table.Root>
            </Card>

            {/* Create/Edit Dialog */}
            <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
                <Dialog.Content style={{ maxWidth: 420 }}>
                    <Dialog.Title>{editUser ? t('user.edit_user') : t('user.add_user')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        {!editUser && (
                            <Box>
                                <Text size="2" mb="1" weight="medium">{t('user.username')}</Text>
                                <TextField.Root
                                    value={form.username}
                                    onChange={(e) => setForm({ ...form, username: e.target.value })}
                                    placeholder={t('user.username')}
                                />
                            </Box>
                        )}
                        <Box>
                            <Text size="2" mb="1" weight="medium">
                                {editUser ? t('user.new_password_hint') : t('user.password')}
                            </Text>
                            <TextField.Root
                                type="password"
                                value={form.password}
                                onChange={(e) => setForm({ ...form, password: e.target.value })}
                                placeholder={editUser ? t('user.leave_blank_to_keep') : t('user.password_min_length')}
                            />
                        </Box>
                        <Box>
                            <Text size="2" mb="1" weight="medium">{t('user.role')}</Text>
                            <Select.Root value={form.role} onValueChange={(v) => setForm({ ...form, role: v })}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    <Select.Item value="admin">{t('user.admin_full')}</Select.Item>
                                    <Select.Item value="viewer">{t('user.viewer_only')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Box>
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button onClick={handleSubmit}>
                            {editUser ? t('common.save') : t('common.create')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
