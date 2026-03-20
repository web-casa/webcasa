import { useState, useEffect, useCallback } from 'react'
import { Box, Flex, Heading, Text, Card, Button, TextField, Badge, Select, Dialog } from '@radix-ui/themes'
import { LayoutTemplate, Search, RefreshCw, Rocket, Package } from 'lucide-react'
import { useNavigate } from 'react-router'
import { appstoreAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

// Framework color mapping
const frameworkColors = {
    nextjs: 'gray',
    nuxt: 'green',
    vite: 'purple',
    remix: 'blue',
    express: 'yellow',
    go: 'cyan',
    laravel: 'red',
    flask: 'green',
    django: 'green',
}

export default function TemplateMarket() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const [loading, setLoading] = useState(true)
    const [templates, setTemplates] = useState([])
    const [frameworks, setFrameworks] = useState([])
    const [search, setSearch] = useState('')
    const [framework, setFramework] = useState('')
    const [deployOpen, setDeployOpen] = useState(false)
    const [selectedTemplate, setSelectedTemplate] = useState(null)
    const [projectName, setProjectName] = useState('')
    const [domain, setDomain] = useState('')
    const [deploying, setDeploying] = useState(false)

    const fetchTemplates = useCallback(async () => {
        try {
            const res = await appstoreAPI.listTemplates({ framework, search })
            setTemplates(res.data?.templates || [])
            setFrameworks(res.data?.frameworks || [])
        } catch { /* ignore */ }
        finally { setLoading(false) }
    }, [framework, search])

    useEffect(() => { fetchTemplates() }, [fetchTemplates])

    const handleSearch = (e) => { setSearch(e.target.value) }
    const handleFrameworkChange = (val) => { setFramework(val === 'all' ? '' : val) }

    const openDeploy = (tpl) => {
        setSelectedTemplate(tpl)
        setProjectName(tpl.template_id || tpl.name?.toLowerCase().replace(/\s+/g, '-') || '')
        setDomain('')
        setDeployOpen(true)
    }

    const handleDeploy = async () => {
        if (!selectedTemplate || !projectName) return
        setDeploying(true)
        try {
            const res = await appstoreAPI.deployTemplate({
                template_id: selectedTemplate.id,
                name: projectName,
                domain: domain || undefined,
            })
            setDeployOpen(false)
            const projectId = res.data?.project_id
            if (projectId) {
                navigate(`/deploy/${projectId}`)
            } else {
                navigate('/deploy')
            }
        } catch (err) {
            alert(err.response?.data?.error || 'Deployment failed')
        } finally {
            setDeploying(false)
        }
    }

    if (loading) {
        return (
            <Flex align="center" justify="center" style={{ minHeight: 200 }}>
                <RefreshCw size={20} className="spin" />
                <Text ml="2">{t('common.loading')}</Text>
            </Flex>
        )
    }

    return (
        <Box>
            {/* Header */}
            <Flex align="center" justify="between" mb="4" wrap="wrap" gap="3">
                <Flex align="center" gap="2">
                    <LayoutTemplate size={24} />
                    <Box>
                        <Heading size="5">{t('appstore.templates_title')}</Heading>
                        <Text size="2" color="gray">{t('appstore.templates_subtitle')}</Text>
                    </Box>
                </Flex>
                <Button size="2" variant="soft" onClick={fetchTemplates}>
                    <RefreshCw size={14} /> {t('common.refresh')}
                </Button>
            </Flex>

            {/* Filters */}
            <Flex gap="3" mb="4" wrap="wrap">
                <Box style={{ flex: 1, minWidth: 200 }}>
                    <TextField.Root
                        placeholder={t('appstore.search_placeholder')}
                        value={search}
                        onChange={handleSearch}
                    >
                        <TextField.Slot><Search size={14} /></TextField.Slot>
                    </TextField.Root>
                </Box>
                {frameworks.length > 0 && (
                    <Select.Root value={framework || 'all'} onValueChange={handleFrameworkChange}>
                        <Select.Trigger placeholder={t('appstore.framework_filter')} />
                        <Select.Content>
                            <Select.Item value="all">{t('appstore.all_frameworks')}</Select.Item>
                            {frameworks.map(f => (
                                <Select.Item key={f} value={f}>{f}</Select.Item>
                            ))}
                        </Select.Content>
                    </Select.Root>
                )}
            </Flex>

            {/* Template Grid */}
            {templates.length === 0 ? (
                <Card>
                    <Flex align="center" justify="center" direction="column" gap="2" p="6">
                        <Package size={40} style={{ opacity: 0.3 }} />
                        <Text size="3" color="gray">{t('appstore.no_templates')}</Text>
                    </Flex>
                </Card>
            ) : (
                <Box style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
                    gap: 'var(--space-3)',
                }}>
                    {templates.map(tpl => {
                        const tags = (() => { try { return JSON.parse(tpl.tags || '[]') } catch { return [] } })()
                        return (
                            <Card key={tpl.id}>
                                <Flex direction="column" gap="2">
                                    <Flex align="center" justify="between">
                                        <Box>
                                            <Text size="3" weight="bold">{tpl.name}</Text>
                                            {tpl.framework && (
                                                <Badge
                                                    ml="2"
                                                    size="1"
                                                    color={frameworkColors[tpl.framework] || 'gray'}
                                                >
                                                    {tpl.framework}
                                                </Badge>
                                            )}
                                        </Box>
                                        <Button size="1" onClick={() => openDeploy(tpl)}>
                                            <Rocket size={12} /> {t('appstore.deploy_template')}
                                        </Button>
                                    </Flex>
                                    {tpl.description && (
                                        <Text size="2" color="gray" style={{
                                            display: '-webkit-box',
                                            WebkitLineClamp: 2,
                                            WebkitBoxOrient: 'vertical',
                                            overflow: 'hidden',
                                        }}>
                                            {tpl.description}
                                        </Text>
                                    )}
                                    <Flex gap="1" wrap="wrap">
                                        {tags.slice(0, 4).map(tag => (
                                            <Badge key={tag} variant="outline" size="1">{tag}</Badge>
                                        ))}
                                    </Flex>
                                    {tpl.git_url && (
                                        <Text size="1" color="gray" style={{ wordBreak: 'break-all' }}>{tpl.git_url}</Text>
                                    )}
                                </Flex>
                            </Card>
                        )
                    })}
                </Box>
            )}

            {/* Deploy Dialog */}
            <Dialog.Root open={deployOpen} onOpenChange={setDeployOpen}>
                <Dialog.Content maxWidth="480px" aria-describedby={undefined}>
                    <Dialog.Title>{t('appstore.deploy_template')} — {selectedTemplate?.name}</Dialog.Title>

                    <Flex direction="column" gap="3" mt="3">
                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('appstore.project_name')} *</Text>
                            <TextField.Root
                                value={projectName}
                                onChange={e => setProjectName(e.target.value)}
                                placeholder="my-project"
                            />
                        </Box>
                        <Box>
                            <Text size="2" weight="bold" mb="1">{t('appstore.domain_optional')}</Text>
                            <Text size="1" color="gray" mb="1">{t('appstore.domain_hint')}</Text>
                            <TextField.Root
                                value={domain}
                                onChange={e => setDomain(e.target.value)}
                                placeholder="project.example.com"
                            />
                        </Box>

                        {selectedTemplate && (
                            <Card variant="surface">
                                <Flex direction="column" gap="1">
                                    {selectedTemplate.framework && (
                                        <Text size="2"><strong>{t('appstore.framework_filter')}:</strong> {selectedTemplate.framework}</Text>
                                    )}
                                    <Text size="2"><strong>Git:</strong> {selectedTemplate.git_url}</Text>
                                    <Text size="2"><strong>Branch:</strong> {selectedTemplate.branch || 'main'}</Text>
                                </Flex>
                            </Card>
                        )}
                    </Flex>

                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close>
                            <Button variant="soft">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button
                            disabled={!projectName || deploying}
                            loading={deploying}
                            onClick={handleDeploy}
                        >
                            <Rocket size={14} /> {deploying ? t('appstore.installing') : t('appstore.deploy_template')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
