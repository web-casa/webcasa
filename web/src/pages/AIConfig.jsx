import { useState, useEffect } from 'react'
import { Box, Flex, Text, Heading, Button, TextField, Card, Badge, Separator } from '@radix-ui/themes'
import { Bot, Save, TestTube, Check, ArrowLeft } from 'lucide-react'
import { aiAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

export default function AIConfig() {
    const { t } = useTranslation()
    const [config, setConfig] = useState({ base_url: '', api_key: '', model: '' })
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [testing, setTesting] = useState(false)
    const [testResult, setTestResult] = useState(null)

    useEffect(() => {
        aiAPI.getConfig().then(res => {
            setConfig(res.data || { base_url: '', api_key: '', model: '' })
        }).catch(() => {}).finally(() => setLoading(false))
    }, [])

    const handleSave = async () => {
        setSaving(true)
        setTestResult(null)
        try {
            await aiAPI.updateConfig(config)
            setTestResult({ ok: true, msg: t('ai.config_saved') })
        } catch (e) {
            setTestResult({ ok: false, msg: e.response?.data?.error || e.message })
        } finally { setSaving(false) }
    }

    const handleTest = async () => {
        setTesting(true)
        setTestResult(null)
        try {
            await aiAPI.testConnection()
            setTestResult({ ok: true, msg: t('ai.connection_ok') })
        } catch (e) {
            setTestResult({ ok: false, msg: e.response?.data?.error || e.message })
        } finally { setTesting(false) }
    }

    if (loading) return <Text>{t('common.loading')}</Text>

    return (
        <Box>
            <Flex align="center" gap="2" mb="4">
                <Link to="/"><Button variant="ghost" size="1"><ArrowLeft size={16} /></Button></Link>
                <Bot size={24} />
                <Heading size="5">{t('ai.settings_title')}</Heading>
            </Flex>

            <Card style={{ maxWidth: 600, padding: 24 }}>
                <Flex direction="column" gap="4">
                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('ai.base_url')}</Text>
                        <TextField.Root
                            placeholder="https://api.openai.com"
                            value={config.base_url}
                            onChange={(e) => setConfig(prev => ({ ...prev, base_url: e.target.value }))}
                        />
                        <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.base_url_hint')}</Text>
                    </Box>

                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('ai.api_key')}</Text>
                        <TextField.Root
                            type="password"
                            placeholder="sk-..."
                            value={config.api_key}
                            onChange={(e) => setConfig(prev => ({ ...prev, api_key: e.target.value }))}
                        />
                        <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.api_key_hint')}</Text>
                    </Box>

                    <Box>
                        <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>{t('ai.model')}</Text>
                        <TextField.Root
                            placeholder="gpt-4o / claude-3.5-sonnet / deepseek-chat"
                            value={config.model}
                            onChange={(e) => setConfig(prev => ({ ...prev, model: e.target.value }))}
                        />
                        <Text size="1" color="gray" mt="1" style={{ display: 'block' }}>{t('ai.model_hint')}</Text>
                    </Box>

                    <Separator size="4" />

                    {testResult && (
                        <Badge size="2" color={testResult.ok ? 'green' : 'red'} variant="soft" style={{ padding: '8px 12px' }}>
                            {testResult.ok ? <Check size={14} /> : null}
                            {testResult.msg}
                        </Badge>
                    )}

                    <Flex gap="2">
                        <Button onClick={handleSave} disabled={saving}>
                            <Save size={14} /> {saving ? t('common.saving') : t('common.save')}
                        </Button>
                        <Button variant="soft" onClick={handleTest} disabled={testing}>
                            <TestTube size={14} /> {testing ? t('common.loading') : t('ai.test_connection')}
                        </Button>
                    </Flex>
                </Flex>
            </Card>
        </Box>
    )
}
