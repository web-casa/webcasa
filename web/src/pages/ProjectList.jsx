import { useState, useEffect } from 'react'
import { Box, Flex, Text, Button, Badge, Table, Card, Heading, Dialog, TextField, IconButton, DropdownMenu, Tooltip } from '@radix-ui/themes'
import { Rocket, Plus, Play, Square, RotateCw, Trash2, ExternalLink, MoreVertical, Search } from 'lucide-react'
import { useNavigate } from 'react-router'
import { deployAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

const statusColors = {
    pending: 'gray',
    building: 'blue',
    running: 'green',
    stopped: 'orange',
    error: 'red',
}

export default function ProjectList() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const [projects, setProjects] = useState([])
    const [loading, setLoading] = useState(true)
    const [filter, setFilter] = useState('')

    const fetchProjects = async () => {
        try {
            const res = await deployAPI.listProjects()
            setProjects(res.data)
        } catch (e) {
            console.error(e)
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { fetchProjects() }, [])

    // Auto-refresh for building projects
    useEffect(() => {
        const hasBuilding = projects.some(p => p.status === 'building')
        if (!hasBuilding) return
        const timer = setInterval(fetchProjects, 3000)
        return () => clearInterval(timer)
    }, [projects])

    const handleAction = async (id, action) => {
        try {
            if (action === 'build') await deployAPI.build(id)
            else if (action === 'start') await deployAPI.start(id)
            else if (action === 'stop') await deployAPI.stop(id)
            else if (action === 'delete') {
                if (!confirm(t('deploy.confirm_delete'))) return
                await deployAPI.deleteProject(id)
            }
            fetchProjects()
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        }
    }

    const filtered = projects.filter(p =>
        !filter || p.name.toLowerCase().includes(filter.toLowerCase()) || (p.domain && p.domain.toLowerCase().includes(filter.toLowerCase()))
    )

    return (
        <Box>
            <Flex justify="between" align="center" mb="4">
                <Box>
                    <Heading size="5">{t('deploy.title')}</Heading>
                    <Text size="2" color="gray">{t('deploy.subtitle')}</Text>
                </Box>
                <Button onClick={() => navigate('/deploy/create')}>
                    <Plus size={16} /> {t('deploy.create_project')}
                </Button>
            </Flex>

            {projects.length > 3 && (
                <Flex mb="3">
                    <TextField.Root placeholder={t('common.search')} value={filter} onChange={e => setFilter(e.target.value)} style={{ maxWidth: 300 }}>
                        <TextField.Slot><Search size={14} /></TextField.Slot>
                    </TextField.Root>
                </Flex>
            )}

            {loading ? (
                <Text color="gray">{t('common.loading')}</Text>
            ) : filtered.length === 0 ? (
                <Card>
                    <Flex direction="column" align="center" gap="3" py="6">
                        <Rocket size={48} strokeWidth={1} color="var(--gray-8)" />
                        <Text size="3" color="gray">{t('deploy.no_projects')}</Text>
                        <Button onClick={() => navigate('/deploy/create')}>
                            <Plus size={16} /> {t('deploy.create_project')}
                        </Button>
                    </Flex>
                </Card>
            ) : (
                <Table.Root variant="surface">
                    <Table.Header>
                        <Table.Row>
                            <Table.ColumnHeaderCell>{t('common.name')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('deploy.domain')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('deploy.framework')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('deploy.build')}</Table.ColumnHeaderCell>
                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                        </Table.Row>
                    </Table.Header>
                    <Table.Body>
                        {filtered.map(project => (
                            <Table.Row key={project.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/deploy/${project.id}`)}>
                                <Table.Cell>
                                    <Text weight="medium">{project.name}</Text>
                                </Table.Cell>
                                <Table.Cell>
                                    {project.domain ? (
                                        <Flex align="center" gap="1">
                                            <Text size="2">{project.domain}</Text>
                                            <ExternalLink size={12} color="var(--gray-8)" />
                                        </Flex>
                                    ) : (
                                        <Text size="2" color="gray">—</Text>
                                    )}
                                </Table.Cell>
                                <Table.Cell>
                                    <Badge variant="soft" color="gray">{project.framework || 'custom'}</Badge>
                                </Table.Cell>
                                <Table.Cell>
                                    <Badge color={statusColors[project.status] || 'gray'}>
                                        {t(`deploy.status_${project.status}`)}
                                    </Badge>
                                </Table.Cell>
                                <Table.Cell>
                                    <Text size="2" color="gray">#{project.current_build || '—'}</Text>
                                </Table.Cell>
                                <Table.Cell onClick={e => e.stopPropagation()}>
                                    <Flex gap="1">
                                        <Tooltip content={t('deploy.build_deploy')}>
                                            <IconButton size="1" variant="ghost" onClick={() => handleAction(project.id, 'build')} disabled={project.status === 'building'}>
                                                <Rocket size={14} />
                                            </IconButton>
                                        </Tooltip>
                                        {project.status === 'running' ? (
                                            <Tooltip content={t('docker.stop')}>
                                                <IconButton size="1" variant="ghost" color="orange" onClick={() => handleAction(project.id, 'stop')}>
                                                    <Square size={14} />
                                                </IconButton>
                                            </Tooltip>
                                        ) : project.status === 'stopped' ? (
                                            <Tooltip content={t('docker.start')}>
                                                <IconButton size="1" variant="ghost" color="green" onClick={() => handleAction(project.id, 'start')}>
                                                    <Play size={14} />
                                                </IconButton>
                                            </Tooltip>
                                        ) : null}
                                        <DropdownMenu.Root>
                                            <DropdownMenu.Trigger>
                                                <IconButton size="1" variant="ghost"><MoreVertical size={14} /></IconButton>
                                            </DropdownMenu.Trigger>
                                            <DropdownMenu.Content>
                                                <DropdownMenu.Item color="red" onClick={() => handleAction(project.id, 'delete')}>
                                                    <Trash2 size={14} /> {t('common.delete')}
                                                </DropdownMenu.Item>
                                            </DropdownMenu.Content>
                                        </DropdownMenu.Root>
                                    </Flex>
                                </Table.Cell>
                            </Table.Row>
                        ))}
                    </Table.Body>
                </Table.Root>
            )}
        </Box>
    )
}
