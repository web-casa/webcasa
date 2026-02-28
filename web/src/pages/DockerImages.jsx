import { useState, useEffect, useCallback } from 'react'
import { Box, Flex, Text, Heading, Badge, Button, Table, Dialog, TextField, Separator } from '@radix-ui/themes'
import { Image, Trash2, RefreshCw, Download, ArrowLeft, Eraser, Search, Star } from 'lucide-react'
import { dockerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

export default function DockerImages() {
    const { t } = useTranslation()
    const [images, setImages] = useState([])
    const [loading, setLoading] = useState(true)
    const [showPull, setShowPull] = useState(false)
    const [pullImage, setPullImage] = useState('')
    const [pulling, setPulling] = useState(false)
    const [actionLoading, setActionLoading] = useState(null)
    const [searchTerm, setSearchTerm] = useState('')
    const [searchResults, setSearchResults] = useState([])
    const [searching, setSearching] = useState(false)

    const fetchData = useCallback(async () => {
        try {
            const res = await dockerAPI.listImages()
            setImages(res.data?.images || [])
        } catch { /* ignore */ } finally { setLoading(false) }
    }, [])

    useEffect(() => { fetchData() }, [fetchData])

    const handlePull = async (imageName) => {
        const name = imageName || pullImage.trim()
        if (!name) return
        setPulling(true)
        try {
            await dockerAPI.pullImage(name)
            setShowPull(false)
            setPullImage('')
            setSearchResults([])
            setSearchTerm('')
            await fetchData()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setPulling(false) }
    }

    const handleSearch = async () => {
        if (!searchTerm.trim()) return
        setSearching(true)
        try {
            const res = await dockerAPI.searchImages(searchTerm.trim(), 15)
            setSearchResults(res.data?.results || [])
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setSearching(false) }
    }

    const handleRemove = async (id) => {
        if (!confirm(t('docker.confirm_delete'))) return
        setActionLoading(id)
        try {
            await dockerAPI.removeImage(id)
            await fetchData()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally { setActionLoading(null) }
    }

    const handlePrune = async () => {
        if (!confirm(t('docker.confirm_prune_images'))) return
        try {
            const res = await dockerAPI.pruneImages()
            alert(`${t('docker.pruned')}: ${formatBytes(res.data?.space_reclaimed || 0)}`)
            await fetchData()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        }
    }

    const formatBytes = (bytes) => {
        if (!bytes) return '0 B'
        const units = ['B', 'KB', 'MB', 'GB']
        let i = 0; let val = bytes
        while (val >= 1024 && i < units.length - 1) { val /= 1024; i++ }
        return `${val.toFixed(1)} ${units[i]}`
    }

    if (loading) {
        return <Flex align="center" justify="center" style={{ minHeight: 200 }}><RefreshCw size={20} className="spin" /><Text ml="2">{t('common.loading')}</Text></Flex>
    }

    return (
        <Box>
            <Flex align="center" justify="between" mb="4">
                <Flex align="center" gap="2">
                    <Link to="/docker/containers"><Button variant="ghost" size="1"><ArrowLeft size={16} /></Button></Link>
                    <Image size={24} />
                    <Heading size="5">{t('docker.images')}</Heading>
                    <Badge variant="soft" size="2">{images.length}</Badge>
                </Flex>
                <Flex gap="2">
                    <Button variant="soft" size="2" color="red" onClick={handlePrune}><Eraser size={14} /> {t('docker.prune')}</Button>
                    <Button size="2" onClick={() => setShowPull(true)}><Download size={14} /> {t('docker.pull_image')}</Button>
                </Flex>
            </Flex>

            <Table.Root variant="surface">
                <Table.Header>
                    <Table.Row>
                        <Table.ColumnHeaderCell>ID</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('docker.tags')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('docker.size')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                    </Table.Row>
                </Table.Header>
                <Table.Body>
                    {images.map((img) => (
                        <Table.Row key={img.id}>
                            <Table.Cell><Text size="2" style={{ fontFamily: 'monospace' }}>{img.id}</Text></Table.Cell>
                            <Table.Cell>
                                {img.tags?.length > 0 ? img.tags.map((tag, i) => (
                                    <Badge key={i} variant="soft" size="1" mr="1">{tag}</Badge>
                                )) : <Text size="2" color="gray">&lt;none&gt;</Text>}
                            </Table.Cell>
                            <Table.Cell><Text size="2">{formatBytes(img.size)}</Text></Table.Cell>
                            <Table.Cell>
                                <Button size="1" variant="ghost" color="red" disabled={actionLoading === img.id}
                                    onClick={() => handleRemove(img.id)}>
                                    <Trash2 size={12} />
                                </Button>
                            </Table.Cell>
                        </Table.Row>
                    ))}
                </Table.Body>
            </Table.Root>

            {/* Pull Image Dialog with Search */}
            <Dialog.Root open={showPull} onOpenChange={(v) => { if (!v) { setShowPull(false); setSearchResults([]); setSearchTerm('') } }}>
                <Dialog.Content maxWidth="600px">
                    <Dialog.Title>{t('docker.pull_image')}</Dialog.Title>

                    {/* Direct pull */}
                    <Box mt="3">
                        <Text size="2" weight="medium" mb="1" style={{ display: 'block' }}>{t('docker.image_name')}</Text>
                        <Flex gap="2">
                            <TextField.Root style={{ flex: 1 }} placeholder="nginx:latest" value={pullImage} onChange={(e) => setPullImage(e.target.value)}
                                onKeyDown={(e) => { if (e.key === 'Enter') handlePull() }} />
                            <Button disabled={pulling || !pullImage.trim()} onClick={() => handlePull()}>
                                {pulling ? t('docker.pulling') : t('docker.pull')}
                            </Button>
                        </Flex>
                    </Box>

                    <Separator size="4" my="3" />

                    {/* Search Docker Hub */}
                    <Box>
                        <Text size="2" weight="medium" mb="1" style={{ display: 'block' }}>{t('docker.search_hub')}</Text>
                        <Flex gap="2" mb="2">
                            <TextField.Root style={{ flex: 1 }} placeholder={t('docker.search_placeholder')} value={searchTerm} onChange={(e) => setSearchTerm(e.target.value)}
                                onKeyDown={(e) => { if (e.key === 'Enter') handleSearch() }}>
                                <TextField.Slot><Search size={14} /></TextField.Slot>
                            </TextField.Root>
                            <Button variant="soft" disabled={searching || !searchTerm.trim()} onClick={handleSearch}>
                                {searching ? t('common.loading') : t('common.search')}
                            </Button>
                        </Flex>

                        {searchResults.length > 0 && (
                            <Box style={{ maxHeight: 300, overflow: 'auto' }}>
                                {searchResults.map((r, i) => (
                                    <Flex key={i} justify="between" align="center" py="2" style={{ borderBottom: '1px solid var(--gray-4)' }}>
                                        <Box style={{ flex: 1, minWidth: 0 }}>
                                            <Flex align="center" gap="1">
                                                <Text size="2" weight="medium">{r.name}</Text>
                                                {r.is_official && <Badge size="1" color="blue">Official</Badge>}
                                            </Flex>
                                            <Text size="1" color="gray" style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                                {r.description || '—'}
                                            </Text>
                                        </Box>
                                        <Flex align="center" gap="2" ml="2">
                                            <Flex align="center" gap="1"><Star size={12} color="var(--gray-8)" /><Text size="1" color="gray">{r.star_count}</Text></Flex>
                                            <Button size="1" variant="soft" disabled={pulling} onClick={() => { setPullImage(r.name); handlePull(r.name) }}>
                                                <Download size={12} />
                                            </Button>
                                        </Flex>
                                    </Flex>
                                ))}
                            </Box>
                        )}
                    </Box>

                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
