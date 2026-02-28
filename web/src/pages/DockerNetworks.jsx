import { useState, useEffect, useCallback } from 'react'
import { Box, Flex, Text, Heading, Badge, Button, Table, Dialog, TextField } from '@radix-ui/themes'
import { Network, Trash2, RefreshCw, Plus, ArrowLeft } from 'lucide-react'
import { dockerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

export default function DockerNetworks() {
    const { t } = useTranslation()
    const [networks, setNetworks] = useState([])
    const [loading, setLoading] = useState(true)
    const [showCreate, setShowCreate] = useState(false)
    const [name, setName] = useState('')
    const [creating, setCreating] = useState(false)

    const fetchData = useCallback(async () => {
        try {
            const res = await dockerAPI.listNetworks()
            setNetworks(res.data?.networks || [])
        } catch { /* ignore */ } finally { setLoading(false) }
    }, [])

    useEffect(() => { fetchData() }, [fetchData])

    const handleCreate = async () => {
        if (!name.trim()) return
        setCreating(true)
        try {
            await dockerAPI.createNetwork(name.trim())
            setShowCreate(false); setName('')
            await fetchData()
        } catch (e) { alert(e.response?.data?.error || e.message) }
        finally { setCreating(false) }
    }

    const handleRemove = async (id) => {
        if (!confirm(t('docker.confirm_delete'))) return
        try {
            await dockerAPI.removeNetwork(id)
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
                    <Network size={24} />
                    <Heading size="5">{t('docker.networks')}</Heading>
                    <Badge variant="soft" size="2">{networks.length}</Badge>
                </Flex>
                <Button size="2" onClick={() => setShowCreate(true)}><Plus size={14} /> {t('docker.create_network')}</Button>
            </Flex>

            <Table.Root variant="surface">
                <Table.Header>
                    <Table.Row>
                        <Table.ColumnHeaderCell>ID</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('docker.name')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('docker.driver')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('docker.scope')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('docker.containers')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                    </Table.Row>
                </Table.Header>
                <Table.Body>
                    {networks.map((n) => (
                        <Table.Row key={n.id}>
                            <Table.Cell><Text size="2" style={{ fontFamily: 'monospace' }}>{n.id}</Text></Table.Cell>
                            <Table.Cell><Text size="2" weight="bold">{n.name}</Text></Table.Cell>
                            <Table.Cell><Badge variant="soft" size="1">{n.driver}</Badge></Table.Cell>
                            <Table.Cell><Text size="2">{n.scope}</Text></Table.Cell>
                            <Table.Cell><Text size="2">{n.containers}</Text></Table.Cell>
                            <Table.Cell>
                                <Button size="1" variant="ghost" color="red"
                                    disabled={['bridge', 'host', 'none'].includes(n.name)}
                                    onClick={() => handleRemove(n.id)}>
                                    <Trash2 size={12} />
                                </Button>
                            </Table.Cell>
                        </Table.Row>
                    ))}
                </Table.Body>
            </Table.Root>

            <Dialog.Root open={showCreate} onOpenChange={setShowCreate}>
                <Dialog.Content maxWidth="400px">
                    <Dialog.Title>{t('docker.create_network')}</Dialog.Title>
                    <Box mt="3">
                        <TextField.Root placeholder="my-network" value={name} onChange={(e) => setName(e.target.value)}
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
