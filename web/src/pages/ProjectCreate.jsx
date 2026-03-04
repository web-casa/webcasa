import { useState, useEffect } from 'react'
import { Box, Flex, Text, Button, Card, Heading, TextField, Select, Switch, TextArea, Separator, Callout, Badge } from '@radix-ui/themes'
import { ArrowLeft, Rocket, Plus, Trash2, Loader2, Sparkles, Key, Github, Container, Server, Wand2 } from 'lucide-react'
import { useNavigate } from 'react-router'
import { deployAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

export default function ProjectCreate() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const [step, setStep] = useState(1)
    const [frameworks, setFrameworks] = useState([])
    const [detecting, setDetecting] = useState(false)
    const [submitting, setSubmitting] = useState(false)
    const [form, setForm] = useState({
        name: '',
        git_url: '',
        git_branch: 'main',
        deploy_key: '',
        auth_method: 'ssh_key',
        github_app_id: '',
        github_private_key: '',
        github_installation_id: '',
        framework: '',
        install_command: '',
        build_command: '',
        start_command: '',
        domain: '',
        port: 0,
        deploy_mode: '',
        health_check_path: '/',
        health_check_timeout: 30,
        health_check_retries: 3,
        memory_limit: 0,
        cpu_limit: 0,
        build_timeout: 30,
        auto_deploy: false,
        env_vars: [],
    })

    useEffect(() => {
        deployAPI.frameworks().then(res => setFrameworks(res.data)).catch(() => {})
    }, [])

    const updateForm = (key, value) => setForm(prev => ({ ...prev, [key]: value }))

    const detectFramework = async () => {
        if (!form.git_url) return
        setDetecting(true)
        try {
            const res = await deployAPI.detect(form.git_url, form.git_branch || 'main')
            const preset = res.data
            setForm(prev => ({
                ...prev,
                framework: preset.framework,
                install_command: preset.install_command || '',
                build_command: preset.build_command || '',
                start_command: preset.start_command || '',
                port: preset.port || 0,
                deploy_mode: preset.framework === 'dockerfile' ? 'docker' : 'bare',
            }))
        } catch (e) {
            alert(e.response?.data?.error || t('deploy.detect_failed'))
        } finally {
            setDetecting(false)
        }
    }

    const addEnvVar = () => updateForm('env_vars', [...form.env_vars, { key: '', value: '' }])
    const removeEnvVar = (i) => updateForm('env_vars', form.env_vars.filter((_, idx) => idx !== i))
    const updateEnvVar = (i, field, val) => {
        const vars = [...form.env_vars]
        vars[i] = { ...vars[i], [field]: val }
        updateForm('env_vars', vars)
    }

    const suggestEnvVars = async () => {
        if (!form.framework || form.framework === 'custom' || form.framework === 'dockerfile') return
        try {
            const res = await deployAPI.suggestEnv(form.framework)
            const suggestions = res.data || []
            if (suggestions.length === 0) return
            const existingKeys = new Set(form.env_vars.map(e => e.key))
            const newVars = suggestions
                .filter(s => !existingKeys.has(s.key))
                .map(s => ({ key: s.key, value: s.default_value || '' }))
            if (newVars.length > 0) {
                updateForm('env_vars', [...form.env_vars, ...newVars])
            }
        } catch { /* ignore */ }
    }

    const handleSubmit = async () => {
        if (!form.name || !form.git_url) {
            alert(t('deploy.name_url_required'))
            return
        }
        setSubmitting(true)
        try {
            const envVars = form.env_vars.filter(e => e.key)
            const payload = { ...form, env_vars: envVars }
            // Convert numeric fields
            if (payload.github_app_id) payload.github_app_id = parseInt(payload.github_app_id) || 0
            if (payload.github_installation_id) payload.github_installation_id = parseInt(payload.github_installation_id) || 0
            const res = await deployAPI.createProject(payload)
            navigate(`/deploy/${res.data.id}`)
        } catch (e) {
            alert(e.response?.data?.error || t('common.operation_failed'))
        } finally {
            setSubmitting(false)
        }
    }

    return (
        <Box>
            <Flex align="center" gap="2" mb="4">
                <Button variant="ghost" size="1" onClick={() => navigate('/deploy')}>
                    <ArrowLeft size={16} />
                </Button>
                <Heading size="5">{t('deploy.create_project')}</Heading>
            </Flex>

            {/* Step indicators */}
            <Flex gap="2" mb="5">
                {[1, 2, 3].map(s => (
                    <Box key={s} style={{ flex: 1, height: 4, borderRadius: 2, background: s <= step ? 'var(--accent-9)' : 'var(--gray-4)' }} />
                ))}
            </Flex>

            {/* Step 1: Source */}
            {step === 1 && (
                <Card>
                    <Heading size="3" mb="3">{t('deploy.step_source')}</Heading>
                    <Flex direction="column" gap="3">
                        <label>
                            <Text size="2" weight="medium" mb="1">{t('deploy.project_name')} *</Text>
                            <TextField.Root placeholder="my-app" value={form.name} onChange={e => updateForm('name', e.target.value)} />
                        </label>
                        <label>
                            <Text size="2" weight="medium" mb="1">{t('deploy.git_url')} *</Text>
                            <TextField.Root placeholder="https://github.com/user/repo.git" value={form.git_url} onChange={e => updateForm('git_url', e.target.value)} />
                        </label>
                        <label>
                            <Text size="2" weight="medium" mb="1">{t('deploy.git_branch')}</Text>
                            <TextField.Root placeholder="main" value={form.git_branch} onChange={e => updateForm('git_branch', e.target.value)} />
                        </label>

                        <Separator />

                        {/* Auth Method Selection */}
                        <Text size="2" weight="medium">{t('deploy.auth_method')}</Text>
                        <Flex gap="2">
                            <Button
                                variant={form.auth_method === 'ssh_key' ? 'solid' : 'outline'}
                                size="2"
                                onClick={() => updateForm('auth_method', 'ssh_key')}
                            >
                                <Key size={14} />
                                {t('deploy.auth_ssh_key')}
                            </Button>
                            <Button
                                variant={form.auth_method === 'github_app' ? 'solid' : 'outline'}
                                size="2"
                                onClick={() => updateForm('auth_method', 'github_app')}
                            >
                                <Github size={14} />
                                {t('deploy.auth_github_app')}
                            </Button>
                        </Flex>

                        {/* SSH Deploy Key */}
                        {form.auth_method === 'ssh_key' && (
                            <label>
                                <Text size="2" weight="medium" mb="1">{t('deploy.deploy_key')}</Text>
                                <Text size="1" color="gray">{t('deploy.deploy_key_hint')}</Text>
                                <TextArea placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" value={form.deploy_key} onChange={e => updateForm('deploy_key', e.target.value)} rows={3} style={{ fontFamily: 'monospace', fontSize: 12 }} />
                                <Text size="1" color="gray">{t('deploy.deploy_key_encrypted')}</Text>
                            </label>
                        )}

                        {/* GitHub App Auth */}
                        {form.auth_method === 'github_app' && (
                            <Flex direction="column" gap="3">
                                <Callout.Root size="1">
                                    <Callout.Text>{t('deploy.github_app_hint')}</Callout.Text>
                                </Callout.Root>
                                <label>
                                    <Text size="2" weight="medium" mb="1">{t('deploy.github_app_id')} *</Text>
                                    <TextField.Root type="number" placeholder="123456" value={form.github_app_id} onChange={e => updateForm('github_app_id', e.target.value)} />
                                </label>
                                <label>
                                    <Text size="2" weight="medium" mb="1">{t('deploy.github_installation_id')} *</Text>
                                    <TextField.Root type="number" placeholder="12345678" value={form.github_installation_id} onChange={e => updateForm('github_installation_id', e.target.value)} />
                                </label>
                                <label>
                                    <Text size="2" weight="medium" mb="1">{t('deploy.github_private_key')} *</Text>
                                    <Text size="1" color="gray">{t('deploy.github_private_key_hint')}</Text>
                                    <TextArea placeholder="-----BEGIN RSA PRIVATE KEY-----" value={form.github_private_key} onChange={e => updateForm('github_private_key', e.target.value)} rows={4} style={{ fontFamily: 'monospace', fontSize: 12 }} />
                                </label>
                            </Flex>
                        )}
                    </Flex>
                    <Flex justify="end" mt="4">
                        <Button onClick={() => setStep(2)} disabled={!form.name || !form.git_url}>
                            {t('common.next')}
                        </Button>
                    </Flex>
                </Card>
            )}

            {/* Step 2: Build Config */}
            {step === 2 && (
                <Card>
                    <Flex justify="between" align="center" mb="3">
                        <Heading size="3">{t('deploy.step_build')}</Heading>
                        <Button variant="soft" size="1" onClick={detectFramework} disabled={detecting}>
                            {detecting ? <Loader2 size={14} className="animate-spin" /> : <Sparkles size={14} />}
                            {t('deploy.auto_detect')}
                        </Button>
                    </Flex>
                    <Flex direction="column" gap="3">
                        <label>
                            <Text size="2" weight="medium" mb="1">{t('deploy.framework')}</Text>
                            <Select.Root value={form.framework} onValueChange={v => {
                                updateForm('framework', v)
                                const preset = frameworks.find(f => f.framework === v)
                                if (preset) {
                                    updateForm('install_command', preset.install_command || '')
                                    updateForm('build_command', preset.build_command || '')
                                    updateForm('start_command', preset.start_command || '')
                                    updateForm('port', preset.port || 0)
                                    updateForm('deploy_mode', v === 'dockerfile' ? 'docker' : (form.deploy_mode || 'bare'))
                                }
                            }}>
                                <Select.Trigger placeholder={t('deploy.select_framework')} />
                                <Select.Content>
                                    {frameworks.map(f => (
                                        <Select.Item key={f.framework} value={f.framework}>{f.name}</Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </label>

                        {/* Deploy Mode */}
                        <Box>
                            <Text size="2" weight="medium" mb="1">{t('deploy.deploy_mode')}</Text>
                            <Flex gap="2" mt="1">
                                <Button
                                    variant={(form.deploy_mode || 'bare') === 'bare' ? 'solid' : 'outline'}
                                    size="2"
                                    onClick={() => updateForm('deploy_mode', 'bare')}
                                    disabled={form.framework === 'dockerfile'}
                                >
                                    <Server size={14} />
                                    {t('deploy.mode_bare')}
                                </Button>
                                <Button
                                    variant={form.deploy_mode === 'docker' ? 'solid' : 'outline'}
                                    size="2"
                                    onClick={() => updateForm('deploy_mode', 'docker')}
                                >
                                    <Container size={14} />
                                    {t('deploy.mode_docker')}
                                </Button>
                            </Flex>
                            <Text size="1" color="gray" mt="1">
                                {form.deploy_mode === 'docker' ? t('deploy.mode_docker_hint') : t('deploy.mode_bare_hint')}
                            </Text>
                        </Box>

                        {form.deploy_mode === 'docker' && form.framework !== 'dockerfile' && (
                            <Callout.Root size="1">
                                <Callout.Text>{t('deploy.docker_needs_dockerfile')}</Callout.Text>
                            </Callout.Root>
                        )}

                        {form.deploy_mode !== 'docker' && (
                            <>
                                <label>
                                    <Text size="2" weight="medium" mb="1">{t('deploy.install_command')}</Text>
                                    <TextField.Root placeholder="npm install" value={form.install_command} onChange={e => updateForm('install_command', e.target.value)} style={{ fontFamily: 'monospace' }} />
                                </label>
                                <label>
                                    <Text size="2" weight="medium" mb="1">{t('deploy.build_command')}</Text>
                                    <TextField.Root placeholder="npm run build" value={form.build_command} onChange={e => updateForm('build_command', e.target.value)} style={{ fontFamily: 'monospace' }} />
                                </label>
                                <label>
                                    <Text size="2" weight="medium" mb="1">{t('deploy.start_command')}</Text>
                                    <TextField.Root placeholder="npm start" value={form.start_command} onChange={e => updateForm('start_command', e.target.value)} style={{ fontFamily: 'monospace' }} />
                                </label>
                            </>
                        )}

                        <label>
                            <Text size="2" weight="medium" mb="1">{t('deploy.port')}</Text>
                            <TextField.Root type="number" placeholder="3000" value={form.port || ''} onChange={e => updateForm('port', parseInt(e.target.value) || 0)} />
                            {form.deploy_mode === 'docker' && (
                                <Text size="1" color="gray">{t('deploy.docker_port_hint')}</Text>
                            )}
                        </label>

                        <Separator />

                        {/* Resource Limits */}
                        <Text size="2" weight="medium">{t('deploy.resource_limits')}</Text>
                        <Flex gap="2">
                            <label style={{ flex: 1 }}>
                                <Text size="1" color="gray">{t('deploy.memory_limit')}</Text>
                                <TextField.Root type="number" placeholder="0" value={form.memory_limit || ''} onChange={e => updateForm('memory_limit', parseInt(e.target.value) || 0)} />
                            </label>
                            <label style={{ flex: 1 }}>
                                <Text size="1" color="gray">{t('deploy.cpu_limit')}</Text>
                                <TextField.Root type="number" placeholder="0" value={form.cpu_limit || ''} onChange={e => updateForm('cpu_limit', parseInt(e.target.value) || 0)} />
                            </label>
                            <label style={{ flex: 1 }}>
                                <Text size="1" color="gray">{t('deploy.build_timeout')}</Text>
                                <TextField.Root type="number" placeholder="30" value={form.build_timeout || ''} onChange={e => updateForm('build_timeout', parseInt(e.target.value) || 30)} />
                            </label>
                        </Flex>
                        <Text size="1" color="gray">{t('deploy.resource_limits_hint')}</Text>
                    </Flex>
                    <Flex justify="between" mt="4">
                        <Button variant="soft" onClick={() => setStep(1)}>{t('common.previous')}</Button>
                        <Button onClick={() => setStep(3)}>{t('common.next')}</Button>
                    </Flex>
                </Card>
            )}

            {/* Step 3: Domain & Options */}
            {step === 3 && (
                <Card>
                    <Heading size="3" mb="3">{t('deploy.step_options')}</Heading>
                    <Flex direction="column" gap="3">
                        <label>
                            <Text size="2" weight="medium" mb="1">{t('deploy.domain')}</Text>
                            <Text size="1" color="gray">{t('deploy.domain_hint')}</Text>
                            <TextField.Root placeholder="app.example.com" value={form.domain} onChange={e => updateForm('domain', e.target.value)} />
                        </label>
                        <Flex align="center" gap="2">
                            <Switch checked={form.auto_deploy} onCheckedChange={v => updateForm('auto_deploy', v)} />
                            <Text size="2">{t('deploy.auto_deploy')}</Text>
                        </Flex>

                        <Separator />

                        {/* Health Check */}
                        <Text size="2" weight="medium">{t('deploy.health_check')}</Text>
                        <Flex gap="2">
                            <label style={{ flex: 2 }}>
                                <Text size="1" color="gray">{t('deploy.health_check_path')}</Text>
                                <TextField.Root placeholder="/" value={form.health_check_path} onChange={e => updateForm('health_check_path', e.target.value)} style={{ fontFamily: 'monospace' }} />
                            </label>
                            <label style={{ flex: 1 }}>
                                <Text size="1" color="gray">{t('deploy.health_check_timeout')}</Text>
                                <TextField.Root type="number" placeholder="30" value={form.health_check_timeout} onChange={e => updateForm('health_check_timeout', parseInt(e.target.value) || 30)} />
                            </label>
                            <label style={{ flex: 1 }}>
                                <Text size="1" color="gray">{t('deploy.health_check_retries')}</Text>
                                <TextField.Root type="number" placeholder="3" value={form.health_check_retries} onChange={e => updateForm('health_check_retries', parseInt(e.target.value) || 3)} />
                            </label>
                        </Flex>

                        <Separator />

                        <Flex justify="between" align="center">
                            <Text size="2" weight="medium">{t('deploy.env_vars')}</Text>
                            <Flex gap="2">
                                {form.framework && form.framework !== 'custom' && form.framework !== 'dockerfile' && (
                                    <Button variant="ghost" size="1" onClick={suggestEnvVars}>
                                        <Wand2 size={14} /> {t('deploy.suggest_env')}
                                    </Button>
                                )}
                                <Button variant="ghost" size="1" onClick={addEnvVar}>
                                    <Plus size={14} /> {t('common.add')}
                                </Button>
                            </Flex>
                        </Flex>
                        {form.env_vars.map((ev, i) => (
                            <Flex key={i} gap="2" align="center">
                                <TextField.Root placeholder="KEY" value={ev.key} onChange={e => updateEnvVar(i, 'key', e.target.value)} style={{ flex: 1, fontFamily: 'monospace' }} />
                                <TextField.Root placeholder="value" value={ev.value} onChange={e => updateEnvVar(i, 'value', e.target.value)} style={{ flex: 2, fontFamily: 'monospace' }} />
                                <Button variant="ghost" color="red" size="1" onClick={() => removeEnvVar(i)}>
                                    <Trash2 size={14} />
                                </Button>
                            </Flex>
                        ))}
                    </Flex>
                    <Flex justify="between" mt="4">
                        <Button variant="soft" onClick={() => setStep(2)}>{t('common.previous')}</Button>
                        <Button onClick={handleSubmit} disabled={submitting}>
                            {submitting ? <Loader2 size={14} className="animate-spin" /> : <Rocket size={14} />}
                            {t('deploy.create_and_deploy')}
                        </Button>
                    </Flex>
                </Card>
            )}
        </Box>
    )
}
