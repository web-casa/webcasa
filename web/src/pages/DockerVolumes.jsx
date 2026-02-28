import { useState, useEffect, useCallback } from 'react'
import { Box, Flex, Text, Heading, Badge, Button, Table, Dialog, TextField } from '@radix-ui/themes'
import { HardDrive, Trash2, RefreshCw, Plus, ArrowLeft } from 'lucide-react'
import { dockerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

export default function DockerVolumes() {
    const { t } = useTranslation()
    const [volumes, setVolumes] = useState([])
    const [loading, setLoading] = useState(true)
    const [showCreate, setShowCreate] = useState(false)
    const [name, setName] = useState('')
    const [creating, setCreating] = useState(false)

    const fetchData = useCallback(async () => {
        try {
            const res = await dockerAPI.listVolumes()
            setVolumes(res.data?.volumes || [])
        } catch { /* ignore */ } finally { setLoading(false) }
    }, [])

    useEffect(() => { fetchData() }, [fetchData])

    const handleCreate = async () => {
        if (!name.trim()) return
        setCreating(true)
        try {
            await dockerAPI.createVolume(name.trim())
            setShowCreate(false); setName('')
            await fetchData()
        } catch (e) { alert(e.response?.data?.error || e.message) }
        finally { setCreating(false) }
    }

    const handleRemove = async (name) => {
        if (!confirm(t('docker.confirm_delete'))) return
        try {
            await dockerAPI.removeVolume(name)
            await fetchData()
        } catch (e) { alert(e.response?.data?.error || e.message) }
    }

    if (loading) {
        return <Flex align="center" justify="center" style={{ minHeight: 200 }}><RefreshCw size={20} className="spin" /><Text ml="2">{t('common.loading')}</Text></Flex>
    }

    return (
        <Box>
            <Flex align="center" justify="between" mb="4">
                <Flex align="center" gap="2">
                    <Link to="/docker/containers"><Button variant="ghost" size="1"><ArrowLeft size={16} /></Button></Link>
                    <HardDrive size={24} />
                    <Heading size="5">{t('docker.volumes')}</Heading>
                    <Badge variant="soft" size="2">{volumes.length}</Badge>
                </Flex>
                <Button size="2" onClick={() => setShowCreate(true)}><Plus size={14} /> {t('docker.create_volume')}</Button>
            </Flex>

            <Table.Root variant="surface">
                <Table.Header>
                    <Table.Row>
                        <Table.ColumnHeaderCell>{t('docker.name')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('docker.driver')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('docker.mountpoint')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                    </Table.Row>
                </Table.Header>
                <Table.Body>
                    {volumes.map((v) => (
                        <Table.Row key={v.name}>
                            <Table.Cell><Text size="2" weight="bold">{v.name}</Text></Table.Cell>
                            <Table.Cell><Badge variant="soft" size="1">{v.driver}</Badge></Table.Cell>
                            <Table.Cell><Text size="1" color="gray" style={{ fontFamily: 'monospace' }}>{v.mountpoint}</Text></Table.Cell>
                            <Table.Cell>
                                <Button size="1" variant="ghost" color="red" onClick={() => handleRemove(v.name)}>
                                    <Trash2 size={12} />
                                </Button>
                            </Table.Cell>
                        </Table.Row>
                    ))}
                </Table.Body>
            </Table.Root>

            <Dialog.Root open={showCreate} onOpenChange={setShowCreate}>
                <Dialog.Content maxWidth="400px">
                    <Dialog.Title>{t('docker.create_volume')}</Dialog.Title>
                    <Box mt="3">
                        <TextField.Root placeholder="my-volume" value={name} onChange={(e) => setName(e.target.value)}
                            onKeyDown={(e) => { if (e.key === 'Enter') handleCreate() }} />
                    </Box>
                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button disabled={creating || !name.trim()} onClick={handleCreate}>{t('common.create')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
