import { useState, useEffect } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Table, Badge, Dialog,
    TextField, Select, IconButton, Callout,
} from '@radix-ui/themes'
import { Plus, Pencil, Trash2, Shield, Eye, AlertCircle, CheckCircle2 } from 'lucide-react'
import { userAPI } from '../api/index.js'

export default function Users() {
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
                showMessage('success', '用户已更新')
            } else {
                await userAPI.create(form)
                showMessage('success', '用户已创建')
            }
            setDialogOpen(false)
            fetchUsers()
        } catch (err) {
            showMessage('error', err.response?.data?.error || '操作失败')
        }
    }

    const handleDelete = async (user) => {
        if (!confirm(`确定删除用户 "${user.username}"？`)) return
        try {
            await userAPI.delete(user.id)
            showMessage('success', '用户已删除')
            fetchUsers()
        } catch (err) {
            showMessage('error', err.response?.data?.error || '删除失败')
        }
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="4">
                <Box>
                    <Heading size="6" style={{ color: '#fafafa' }}>用户管理</Heading>
                    <Text size="2" color="gray">管理面板用户和权限</Text>
                </Box>
                <Button onClick={openCreate}>
                    <Plus size={16} /> 添加用户
                </Button>
            </Flex>

            {message && (
                <Callout.Root
                    color={message.type === 'error' ? 'red' : 'green'}
                    mb="4"
                    style={{ background: message.type === 'error' ? '#1a0505' : '#051a05' }}
                >
                    <Callout.Icon>
                        {message.type === 'error' ? <AlertCircle size={16} /> : <CheckCircle2 size={16} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Card style={{ background: '#111113', border: '1px solid #1e1e22' }}>
                <Table.Root>
                    <Table.Header>
                        <Table.Row>
                            <Table.ColumnHeaderCell>ID</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>用户名</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>角色</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>创建时间</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>操作</Table.ColumnHeaderCell>
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
                                            <Flex align="center" gap="1"><Shield size={12} /> 管理员</Flex>
                                        ) : (
                                            <Flex align="center" gap="1"><Eye size={12} /> 只读</Flex>
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
                                    <Text color="gray" size="2">暂无用户</Text>
                                </Table.Cell>
                            </Table.Row>
                        )}
                    </Table.Body>
                </Table.Root>
            </Card>

            {/* Create/Edit Dialog */}
            <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
                <Dialog.Content style={{ maxWidth: 420 }}>
                    <Dialog.Title>{editUser ? '编辑用户' : '添加用户'}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        {!editUser && (
                            <Box>
                                <Text size="2" mb="1" weight="medium">用户名</Text>
                                <TextField.Root
                                    value={form.username}
                                    onChange={(e) => setForm({ ...form, username: e.target.value })}
                                    placeholder="用户名"
                                />
                            </Box>
                        )}
                        <Box>
                            <Text size="2" mb="1" weight="medium">
                                {editUser ? '新密码（留空不修改）' : '密码'}
                            </Text>
                            <TextField.Root
                                type="password"
                                value={form.password}
                                onChange={(e) => setForm({ ...form, password: e.target.value })}
                                placeholder={editUser ? '留空不修改' : '至少6位'}
                            />
                        </Box>
                        <Box>
                            <Text size="2" mb="1" weight="medium">角色</Text>
                            <Select.Root value={form.role} onValueChange={(v) => setForm({ ...form, role: v })}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    <Select.Item value="admin">管理员 — 完全控制</Select.Item>
                                    <Select.Item value="viewer">只读 — 仅查看</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Box>
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">取消</Button>
                        </Dialog.Close>
                        <Button onClick={handleSubmit}>
                            {editUser ? '保存' : '创建'}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
