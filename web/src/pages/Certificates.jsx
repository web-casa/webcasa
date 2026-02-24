import { useState, useEffect, useRef } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Table, Badge, Dialog,
    TextField, Callout, IconButton, AlertDialog,
} from '@radix-ui/themes'
import {
    Plus, Trash2, Upload, ShieldCheck, AlertCircle, CheckCircle2,
} from 'lucide-react'
import { certificateAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

export default function Certificates() {
    const { t } = useTranslation()
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
            showMsg('success', t('cert.delete_success'))
            setDeleteTarget(null)
            fetchCerts()
        } catch (err) {
            showMsg('error', err.response?.data?.error || t('common.delete_failed'))
            setDeleteTarget(null)
        }
    }

    const formatDate = (d) => {
        if (!d) return '—'
        const date = new Date(d)
        const now = new Date()
        const days = Math.floor((date - now) / 86400000)
        const str = date.toLocaleDateString()
        if (days < 0) return <Badge color="red" size="1">{t('cert.expired')} {str}</Badge>
        if (days < 30) return <Badge color="orange" size="1">{str} ({days} {t('common.days')})</Badge>
        return <Badge color="green" size="1" variant="soft">{str} ({days} {t('common.days')})</Badge>
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="4">
                <Box>
                    <Heading size="6" mb="1" style={{ color: 'var(--cp-text)' }}>{t('cert.title')}</Heading>
                    <Text size="2" color="gray">{t('cert.subtitle')}</Text>
                </Box>
                <Button onClick={() => setUploadOpen(true)}>
                    <Plus size={14} /> {t('cert.upload')}
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

            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                {certs.length === 0 ? (
                    <Flex direction="column" align="center" justify="center" py="8">
                        <ShieldCheck size={40} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="2" color="gray" mt="3">
                            {loading ? t('common.loading') : t('cert.no_certs_hint')}
                        </Text>
                    </Flex>
                ) : (
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('common.name')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('cert.domain')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('cert.expires')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('cert.linked_hosts')}</Table.ColumnHeaderCell>
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
                                        <Text size="1" style={{ fontFamily: 'monospace', color: 'var(--cp-text-secondary)' }}>
                                            {cert.domains || '—'}
                                        </Text>
                                    </Table.Cell>
                                    <Table.Cell>{formatDate(cert.expires_at)}</Table.Cell>
                                    <Table.Cell>
                                        <Badge variant="soft" size="1">{t('common.host_count', { count: cert.host_count || 0 })}</Badge>
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
                onUploaded={() => { setUploadOpen(false); fetchCerts(); showMsg('success', t('common.save_success')) }}
                onError={(msg) => showMsg('error', msg)}
            />

            {/* Delete Confirmation */}
            <AlertDialog.Root open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
                <AlertDialog.Content maxWidth="400px">
                    <AlertDialog.Title>{t('cert.delete_title')}</AlertDialog.Title>
                    <AlertDialog.Description dangerouslySetInnerHTML={{ __html: t('cert.confirm_delete_desc', { name: deleteTarget?.name }) }} />
                    <Flex gap="3" mt="4" justify="end">
                        <AlertDialog.Cancel>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </AlertDialog.Cancel>
                        <AlertDialog.Action>
                            <Button color="red" onClick={handleDelete}>{t('common.delete')}</Button>
                        </AlertDialog.Action>
                    </Flex>
                </AlertDialog.Content>
            </AlertDialog.Root>
        </Box>
    )
}

// ============ Upload Dialog ============
function UploadDialog({ open, onClose, onUploaded, onError }) {
    const { t } = useTranslation()
    const [name, setName] = useState('')
    const [certFile, setCertFile] = useState(null)
    const [keyFile, setKeyFile] = useState(null)
    const [uploading, setUploading] = useState(false)
    const certInputRef = useRef(null)
    const keyInputRef = useRef(null)

    const handleUpload = async () => {
        if (!name.trim()) { onError(t('cert.error_no_name')); return }
        if (!certFile) { onError(t('cert.error_no_cert')); return }
        if (!keyFile) { onError(t('cert.error_no_key')); return }

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
            onError(err.response?.data?.error || t('common.operation_failed'))
        }
        setUploading(false)
    }

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <Dialog.Content maxWidth="480px">
                <Dialog.Title>{t('cert.upload')}</Dialog.Title>
                <Dialog.Description size="2" color="gray">
                    {t('cert.upload_description')}
                </Dialog.Description>

                <Flex direction="column" gap="3" mt="4">
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('cert.name')}</Text>
                        <TextField.Root
                            placeholder={t('cert.name_placeholder')}
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                        />
                    </Flex>

                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('cert.cert_file')}</Text>
                        <Button
                            variant="soft" color="gray" size="2"
                            onClick={() => certInputRef.current?.click()}
                        >
                            <Upload size={14} />
                            {certFile ? certFile.name : t('cert.choose_cert')}
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
                        <Text size="2" weight="medium">{t('cert.key_file')}</Text>
                        <Button
                            variant="soft" color="gray" size="2"
                            onClick={() => keyInputRef.current?.click()}
                        >
                            <Upload size={14} />
                            {keyFile ? keyFile.name : t('cert.choose_key')}
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
                        <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                    </Dialog.Close>
                    <Button onClick={handleUpload} disabled={uploading}>
                        {uploading ? t('common.uploading') : t('common.upload')}
                    </Button>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}
