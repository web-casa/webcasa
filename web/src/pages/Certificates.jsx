import { useState, useEffect, useRef } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Table, Badge, Dialog,
    TextField, Callout, IconButton, AlertDialog,
} from '@radix-ui/themes'
import {
    Plus, Trash2, Upload, ShieldCheck, AlertCircle, CheckCircle2, X,
} from 'lucide-react'
import { certificateAPI } from '../api/index.js'

export default function Certificates() {
    const [certs, setCerts] = useState([])
    const [loading, setLoading] = useState(true)
    const [message, setMessage] = useState(null)
    const [uploadOpen, setUploadOpen] = useState(false)
    const [deleteTarget, setDeleteTarget] = useState(null)

    const fetchCerts = async () => {
        try {
            const res = await certificateAPI.list()
            setCerts(res.data.certificates || [])
        } catch { /* ignore */ }
        setLoading(false)
    }

    useEffect(() => { fetchCerts() }, [])

    const showMsg = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 5000)
    }

    const handleDelete = async () => {
        if (!deleteTarget) return
        try {
            await certificateAPI.delete(deleteTarget.id)
            showMsg('success', '证书已删除')
            setDeleteTarget(null)
            fetchCerts()
        } catch (err) {
            showMsg('error', err.response?.data?.error || '删除失败')
            setDeleteTarget(null)
        }
    }

    const formatDate = (d) => {
        if (!d) return '—'
        const date = new Date(d)
        const now = new Date()
        const days = Math.floor((date - now) / 86400000)
        const str = date.toLocaleDateString('zh-CN')
        if (days < 0) return <Badge color="red" size="1">已过期 {str}</Badge>
        if (days < 30) return <Badge color="orange" size="1">{str} ({days}天)</Badge>
        return <Badge color="green" size="1" variant="soft">{str} ({days}天)</Badge>
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="4">
                <Box>
                    <Heading size="6" mb="1" style={{ color: '#fafafa' }}>证书管理</Heading>
                    <Text size="2" color="gray">管理 SSL/TLS 证书，可在站点配置中引用</Text>
                </Box>
                <Button onClick={() => setUploadOpen(true)}>
                    <Plus size={14} /> 上传证书
                </Button>
            </Flex>

            {message && (
                <Callout.Root color={message.type === 'success' ? 'green' : 'red'} size="1" mb="4">
                    <Callout.Icon>
                        {message.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Card style={{ background: '#111113', border: '1px solid #1e1e22' }}>
                {certs.length === 0 ? (
                    <Flex direction="column" align="center" justify="center" py="8">
                        <ShieldCheck size={40} color="#3f3f46" />
                        <Text size="2" color="gray" mt="3">
                            {loading ? '加载中...' : '暂无证书，点击上方"上传证书"按钮添加'}
                        </Text>
                    </Flex>
                ) : (
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>名称</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>域名</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>过期时间</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>关联站点</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell width="60"></Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {certs.map((cert) => (
                                <Table.Row key={cert.id}>
                                    <Table.Cell>
                                        <Flex align="center" gap="2">
                                            <ShieldCheck size={14} color="#10b981" />
                                            <Text size="2">{cert.name}</Text>
                                        </Flex>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Text size="1" style={{ fontFamily: 'monospace', color: '#a1a1aa' }}>
                                            {cert.domains || '—'}
                                        </Text>
                                    </Table.Cell>
                                    <Table.Cell>{formatDate(cert.expires_at)}</Table.Cell>
                                    <Table.Cell>
                                        <Badge variant="soft" size="1">{cert.host_count || 0} 个站点</Badge>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <IconButton
                                            size="1" variant="ghost" color="red"
                                            onClick={() => setDeleteTarget(cert)}
                                        >
                                            <Trash2 size={14} />
                                        </IconButton>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                )}
            </Card>

            {/* Upload Dialog */}
            <UploadDialog
                open={uploadOpen}
                onClose={() => setUploadOpen(false)}
                onUploaded={() => { setUploadOpen(false); fetchCerts(); showMsg('success', '证书上传成功') }}
                onError={(msg) => showMsg('error', msg)}
            />

            {/* Delete Confirmation */}
            <AlertDialog.Root open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
                <AlertDialog.Content maxWidth="400px">
                    <AlertDialog.Title>删除证书</AlertDialog.Title>
                    <AlertDialog.Description>
                        确定要删除证书 <strong>{deleteTarget?.name}</strong> 吗？此操作不可撤销。
                    </AlertDialog.Description>
                    <Flex gap="3" mt="4" justify="end">
                        <AlertDialog.Cancel>
                            <Button variant="soft" color="gray">取消</Button>
                        </AlertDialog.Cancel>
                        <AlertDialog.Action>
                            <Button color="red" onClick={handleDelete}>删除</Button>
                        </AlertDialog.Action>
                    </Flex>
                </AlertDialog.Content>
            </AlertDialog.Root>
        </Box>
    )
}

// ============ Upload Dialog ============
function UploadDialog({ open, onClose, onUploaded, onError }) {
    const [name, setName] = useState('')
    const [certFile, setCertFile] = useState(null)
    const [keyFile, setKeyFile] = useState(null)
    const [uploading, setUploading] = useState(false)
    const certInputRef = useRef(null)
    const keyInputRef = useRef(null)

    const handleUpload = async () => {
        if (!name.trim()) { onError('请输入证书名称'); return }
        if (!certFile) { onError('请选择证书文件'); return }
        if (!keyFile) { onError('请选择密钥文件'); return }

        setUploading(true)
        try {
            const formData = new FormData()
            formData.append('name', name.trim())
            formData.append('cert', certFile)
            formData.append('key', keyFile)
            await certificateAPI.upload(formData)
            setName('')
            setCertFile(null)
            setKeyFile(null)
            onUploaded()
        } catch (err) {
            onError(err.response?.data?.error || '上传失败')
        }
        setUploading(false)
    }

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <Dialog.Content maxWidth="480px">
                <Dialog.Title>上传证书</Dialog.Title>
                <Dialog.Description size="2" color="gray">
                    上传 PEM 格式的 SSL 证书和私钥文件。系统会自动解析域名和过期时间。
                </Dialog.Description>

                <Flex direction="column" gap="3" mt="4">
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">证书名称</Text>
                        <TextField.Root
                            placeholder="如: example.com 通配符"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                        />
                    </Flex>

                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">证书文件 (.pem / .crt)</Text>
                        <Button
                            variant="soft" color="gray" size="2"
                            onClick={() => certInputRef.current?.click()}
                        >
                            <Upload size={14} />
                            {certFile ? certFile.name : '选择证书文件'}
                        </Button>
                        <input
                            ref={certInputRef}
                            type="file"
                            accept=".pem,.crt,.cer"
                            onChange={(e) => setCertFile(e.target.files?.[0] || null)}
                            style={{ display: 'none' }}
                        />
                    </Flex>

                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">私钥文件 (.pem / .key)</Text>
                        <Button
                            variant="soft" color="gray" size="2"
                            onClick={() => keyInputRef.current?.click()}
                        >
                            <Upload size={14} />
                            {keyFile ? keyFile.name : '选择私钥文件'}
                        </Button>
                        <input
                            ref={keyInputRef}
                            type="file"
                            accept=".pem,.key"
                            onChange={(e) => setKeyFile(e.target.files?.[0] || null)}
                            style={{ display: 'none' }}
                        />
                    </Flex>
                </Flex>

                <Flex gap="3" mt="4" justify="end">
                    <Dialog.Close>
                        <Button variant="soft" color="gray">取消</Button>
                    </Dialog.Close>
                    <Button onClick={handleUpload} disabled={uploading}>
                        {uploading ? '上传中...' : '上传'}
                    </Button>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}
