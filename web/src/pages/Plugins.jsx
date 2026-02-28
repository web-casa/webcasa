import { useState, useEffect } from 'react'
import { Box, Flex, Text, Card, Badge, Switch, Heading, Separator } from '@radix-ui/themes'
import { Package, RefreshCw } from 'lucide-react'
import { pluginAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

const categoryColors = {
    deploy: 'blue',
    database: 'green',
    tool: 'orange',
    monitor: 'purple',
}

export default function Plugins() {
    const { t } = useTranslation()
    const [plugins, setPlugins] = useState([])
    const [loading, setLoading] = useState(true)
    const [toggling, setToggling] = useState(null)

    const fetchPlugins = async () => {
        try {
            const res = await pluginAPI.list()
            setPlugins(res.data?.plugins || [])
        } catch {
            // ignore
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { fetchPlugins() }, [])

    const handleToggle = async (id, currentEnabled) => {
        setToggling(id)
        try {
            if (currentEnabled) {
                await pluginAPI.disable(id)
            } else {
                await pluginAPI.enable(id)
            }
            await fetchPlugins()
        } catch {
            // ignore
        } finally {
            setToggling(null)
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
            <Flex align="center" justify="between" mb="4">
                <Flex align="center" gap="2">
                    <Package size={24} />
                    <Heading size="5">{t('plugins.title')}</Heading>
                </Flex>
                <Badge variant="soft" size="2">{plugins.length} {t('plugins.installed')}</Badge>
            </Flex>

            <Text size="2" color="gray" mb="4" style={{ display: 'block' }}>
                {t('plugins.description')}
            </Text>

            <Separator size="4" mb="4" />

            {plugins.length === 0 ? (
                <Card style={{ padding: 40, textAlign: 'center' }}>
                    <Package size={48} style={{ margin: '0 auto 12px', opacity: 0.3 }} />
                    <Text size="3" color="gray">{t('plugins.empty')}</Text>
                </Card>
            ) : (
                <Flex direction="column" gap="3">
                    {plugins.map((p) => (
                        <Card key={p.id} style={{ padding: 16 }}>
                            <Flex align="center" justify="between">
                                <Flex direction="column" gap="1" style={{ flex: 1 }}>
                                    <Flex align="center" gap="2">
                                        <Text weight="bold" size="3">{p.name}</Text>
                                        <Badge variant="soft" size="1">v{p.version}</Badge>
                                        {p.category && (
                                            <Badge color={categoryColors[p.category] || 'gray'} variant="soft" size="1">
                                                {p.category}
                                            </Badge>
                                        )}
                                    </Flex>
                                    <Text size="2" color="gray">{p.description}</Text>
                                    {p.dependencies?.length > 0 && (
                                        <Text size="1" color="gray">
                                            {t('plugins.depends_on')}: {p.dependencies.join(', ')}
                                        </Text>
                                    )}
                                </Flex>
                                <Switch
                                    checked={p.enabled}
                                    disabled={toggling === p.id}
                                    onCheckedChange={() => handleToggle(p.id, p.enabled)}
                                />
                            </Flex>
                        </Card>
                    ))}
                </Flex>
            )}
        </Box>
    )
}
