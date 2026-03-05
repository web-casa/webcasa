import { useState, useEffect, useCallback } from 'react'
import { Box, Flex, Heading, Text, Card, Button, TextField, Badge, Separator, Dialog, Switch, Code, ScrollArea, Callout } from '@radix-ui/themes'
import { ArrowLeft, ExternalLink, RefreshCw, Download, Globe, AlertTriangle } from 'lucide-react'
import { useParams, useNavigate } from 'react-router'
import { appstoreAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'
import Markdown from 'react-markdown'

export default function AppDetail() {
    const { t } = useTranslation()
    const { id } = useParams()
    const navigate = useNavigate()
    const [loading, setLoading] = useState(true)
    const [app, setApp] = useState(null)
    const [installOpen, setInstallOpen] = useState(false)
    const [installing, setInstalling] = useState(false)
    const [step, setStep] = useState(0) // 0: configure, 1: options, 2: review
    const [formValues, setFormValues] = useState({})
    const [instanceName, setInstanceName] = useState('')
    const [domain, setDomain] = useState('')
    const [autoUpdate, setAutoUpdate] = useState(false)

    const fetchApp = useCallback(async () => {
        try {
            const res = await appstoreAPI.getApp(id)
            setApp(res.data)
            // Pre-fill instance name from app_id
            setInstanceName(res.data?.app_id || '')
        } catch {
            navigate('/store')
        } finally {
            setLoading(false)
        }
    }, [id, navigate])

    useEffect(() => { fetchApp() }, [fetchApp])

    const formFields = (() => {
        try { return JSON.parse(app?.form_fields || '[]') || [] }
        catch { return [] }
    })()

    const categories = (() => {
        try { return JSON.parse(app?.categories || '[]') }
        catch { return [] }
    })()

    const configurableFields = formFields.filter(f => f.type !== 'random')
    const hasConfig = configurableFields.length > 0
    const securityWarnings = app?.security_warnings || []

    const handleFieldChange = (envVar, value) => {
        setFormValues(prev => ({ ...prev, [envVar]: value }))
    }

    const handleInstall = async () => {
        setInstalling(true)
        try {
            await appstoreAPI.install({
                app_id: app.app_id,
                name: instanceName,
                form_values: formValues,
                domain: domain || undefined,
                auto_update: autoUpdate,
            })
            setInstallOpen(false)
            navigate('/store')
        } catch (err) {
            alert(err.response?.data?.error || 'Installation failed')
        } finally {
            setInstalling(false)
        }
    }

    // Render compose preview
    const renderCompose = () => {
        if (!app?.compose_file) return ''
        let result = app.compose_file
        for (const [k, v] of Object.entries(formValues)) {
            result = result.replaceAll(`{{${k}}}`, v || `<${k}>`)
        }
        return result
    }

    if (loading) {
        return (
            <Flex align="center" justify="center" style={{ minHeight: 200 }}>
                <RefreshCw size={20} className="spin" />
                <Text ml="2">{t('common.loading')}</Text>
            </Flex>
        )
    }

    if (!app) return null

    return (
        <Box>
            {/* Back button */}
            <Button size="2" variant="ghost" onClick={() => navigate('/store')} mb="4">
                <ArrowLeft size={16} /> {t('appstore.back_to_store')}
            </Button>

            {/* App header */}
            <Card mb="4">
                <Flex gap="4" align="start" wrap="wrap">
                    <img
                        src={appstoreAPI.appLogoUrl(app.id)}
                        alt={app.name}
                        style={{ width: 80, height: 80, borderRadius: 12, objectFit: 'cover', background: 'var(--gray-3)' }}
                        onError={(e) => { e.target.style.display = 'none' }}
                    />
                    <Box style={{ flex: 1 }}>
                        <Heading size="5" mb="1">{app.name}</Heading>
                        <Flex gap="2" wrap="wrap" mb="2">
                            {app.version && <Badge variant="soft">{t('appstore.version')}: {app.version}</Badge>}
                            {app.author && <Badge variant="soft" color="gray">{t('appstore.author')}: {app.author}</Badge>}
                            {app.port > 0 && <Badge variant="soft" color="gray">{t('appstore.port')}: {app.port}</Badge>}
                            {categories.map(c => (
                                <Badge key={c} variant="soft" color="blue">{c}</Badge>
                            ))}
                        </Flex>
                        <Flex gap="3" align="center">
                            {app.website && (
                                <Button size="1" variant="ghost" asChild>
                                    <a href={app.website} target="_blank" rel="noopener noreferrer">
                                        <Globe size={12} /> {t('appstore.website')}
                                    </a>
                                </Button>
                            )}
                            {app.source_url && (
                                <Button size="1" variant="ghost" asChild>
                                    <a href={app.source_url} target="_blank" rel="noopener noreferrer">
                                        <ExternalLink size={12} /> {t('appstore.source')}
                                    </a>
                                </Button>
                            )}
                        </Flex>
                    </Box>
                    <Button size="3" onClick={() => { setStep(hasConfig ? 0 : 1); setInstallOpen(true) }}>
                        <Download size={16} /> {t('appstore.install')}
                    </Button>
                </Flex>
            </Card>

            {/* Description */}
            {app.description && (
                <Card mb="4">
                    <Box className="app-description">
                        <Markdown>{app.description}</Markdown>
                    </Box>
                </Card>
            )}

            {app.short_desc && !app.description && (
                <Card mb="4">
                    <Text size="2" color="gray">{app.short_desc}</Text>
                </Card>
            )}

            {/* Install Dialog */}
            <Dialog.Root open={installOpen} onOpenChange={setInstallOpen}>
                <Dialog.Content maxWidth="600px">
                    <Dialog.Title>{t('appstore.install')} — {app.name}</Dialog.Title>

                    {/* Step indicators */}
                    <Flex gap="2" mb="4" mt="2">
                        {hasConfig && (
                            <Badge variant={step === 0 ? 'solid' : 'soft'} style={{ cursor: 'pointer' }} onClick={() => setStep(0)}>
                                1. {t('appstore.step_configure')}
                            </Badge>
                        )}
                        <Badge variant={step === 1 ? 'solid' : 'soft'} style={{ cursor: 'pointer' }} onClick={() => setStep(1)}>
                            {hasConfig ? '2' : '1'}. {t('appstore.step_options')}
                        </Badge>
                        <Badge variant={step === 2 ? 'solid' : 'soft'} style={{ cursor: 'pointer' }} onClick={() => setStep(2)}>
                            {hasConfig ? '3' : '2'}. {t('appstore.step_review')}
                        </Badge>
                    </Flex>

                    {/* Security warnings */}
                    {securityWarnings.length > 0 && (
                        <Callout.Root color="orange" mb="3">
                            <Callout.Icon><AlertTriangle size={16} /></Callout.Icon>
                            <Callout.Text>
                                {t('appstore.security_warning')}: {securityWarnings.map(w => t(`appstore.security_${w}`, w)).join(', ')}
                            </Callout.Text>
                        </Callout.Root>
                    )}

                    {/* Step 0: Configuration form */}
                    {step === 0 && hasConfig && (
                        <Flex direction="column" gap="3">
                            {configurableFields.map(field => (
                                <Box key={field.env_variable}>
                                    <Text size="2" weight="bold" mb="1">{field.label}</Text>
                                    {field.hint && <Text size="1" color="gray" mb="1">{field.hint}</Text>}
                                    {field.type === 'boolean' ? (
                                        <Switch
                                            checked={formValues[field.env_variable] === 'true'}
                                            onCheckedChange={(v) => handleFieldChange(field.env_variable, v ? 'true' : 'false')}
                                        />
                                    ) : field.options?.length > 0 ? (
                                        <select
                                            value={formValues[field.env_variable] || ''}
                                            onChange={e => handleFieldChange(field.env_variable, e.target.value)}
                                            style={{ width: '100%', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--gray-6)', background: 'var(--color-background)' }}
                                        >
                                            <option value="">Select...</option>
                                            {field.options.map(o => (
                                                <option key={o.value} value={o.value}>{o.label}</option>
                                            ))}
                                        </select>
                                    ) : (
                                        <TextField.Root
                                            type={field.type === 'password' ? 'password' : field.type === 'number' ? 'number' : 'text'}
                                            placeholder={field.placeholder || ''}
                                            value={formValues[field.env_variable] || ''}
                                            onChange={e => handleFieldChange(field.env_variable, e.target.value)}
                                        />
                                    )}
                                    {field.required && <Text size="1" color="red">*</Text>}
                                </Box>
                            ))}
                            <Flex justify="end" mt="2">
                                <Button onClick={() => setStep(1)}>{t('common.next')} &rarr;</Button>
                            </Flex>
                        </Flex>
                    )}

                    {/* Step 1: Options (name, domain, auto-update) */}
                    {step === 1 && (
                        <Flex direction="column" gap="3">
                            <Box>
                                <Text size="2" weight="bold" mb="1">{t('appstore.instance_name')} *</Text>
                                <Text size="1" color="gray" mb="1">{t('appstore.instance_name_hint')}</Text>
                                <TextField.Root
                                    value={instanceName}
                                    onChange={e => setInstanceName(e.target.value)}
                                    placeholder="my-app"
                                />
                            </Box>
                            <Box>
                                <Text size="2" weight="bold" mb="1">{t('appstore.domain_optional')}</Text>
                                <Text size="1" color="gray" mb="1">{t('appstore.domain_hint')}</Text>
                                <TextField.Root
                                    value={domain}
                                    onChange={e => setDomain(e.target.value)}
                                    placeholder="app.example.com"
                                />
                            </Box>
                            <Flex align="center" gap="2">
                                <Switch checked={autoUpdate} onCheckedChange={setAutoUpdate} />
                                <Text size="2">{t('appstore.auto_update')}</Text>
                            </Flex>
                            <Flex justify="between" mt="2">
                                {hasConfig && <Button variant="soft" onClick={() => setStep(0)}>&larr; {t('common.previous')}</Button>}
                                <Box style={{ flex: 1 }} />
                                <Button onClick={() => setStep(2)}>{t('common.next')} &rarr;</Button>
                            </Flex>
                        </Flex>
                    )}

                    {/* Step 2: Review */}
                    {step === 2 && (
                        <Flex direction="column" gap="3">
                            <Card variant="surface">
                                <Flex direction="column" gap="1">
                                    <Text size="2"><strong>{t('appstore.instance_name')}:</strong> {instanceName}</Text>
                                    {domain && <Text size="2"><strong>{t('appstore.domain_optional')}:</strong> {domain}</Text>}
                                    <Text size="2"><strong>{t('appstore.auto_update')}:</strong> {autoUpdate ? t('common.yes') : t('common.no')}</Text>
                                </Flex>
                            </Card>

                            {app.compose_file && (
                                <Box>
                                    <Text size="2" weight="bold" mb="1">{t('appstore.review_compose')}</Text>
                                    <ScrollArea style={{ maxHeight: 250 }}>
                                        <Code size="1" style={{ whiteSpace: 'pre', display: 'block', padding: 12, borderRadius: 8, background: 'var(--gray-2)' }}>
                                            {renderCompose()}
                                        </Code>
                                    </ScrollArea>
                                </Box>
                            )}

                            <Flex justify="between" mt="2">
                                <Button variant="soft" onClick={() => setStep(1)}>&larr; {t('common.previous')}</Button>
                                <Button
                                    disabled={!instanceName || installing}
                                    loading={installing}
                                    onClick={handleInstall}
                                >
                                    <Download size={14} /> {installing ? t('appstore.installing') : t('appstore.install')}
                                </Button>
                            </Flex>
                        </Flex>
                    )}
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
