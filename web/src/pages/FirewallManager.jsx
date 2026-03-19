import { useState, useEffect, useCallback, useRef } from 'react'
import {
    Box, Flex, Text, Card, Badge, Button, Table, Dialog, TextField,
    Select, Tabs, Callout, Heading,
} from '@radix-ui/themes'
import {
    Shield, Plus, Trash2, RefreshCw, AlertTriangle, Info,
    Download, CheckCircle2, AlertCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { firewallAPI } from '../api/index.js'

export default function FirewallManager() {
    const { t } = useTranslation()

    const [status, setStatus] = useState(null)
    const [zones, setZones] = useState([])
    const [activeTab, setActiveTab] = useState('')
    const [loading, setLoading] = useState(true)
    const [availableServices, setAvailableServices] = useState([])

    // Add rule dialog
    const [addOpen, setAddOpen] = useState(false)
    const [ruleType, setRuleType] = useState('port')
    const [ruleZone, setRuleZone] = useState('')
    const [rulePort, setRulePort] = useState('')
    const [ruleProtocol, setRuleProtocol] = useState('tcp')
    const [ruleService, setRuleService] = useState('')
    const [ruleRichRule, setRuleRichRule] = useState('')
    const [submitting, setSubmitting] = useState(false)
    const [starting, setStarting] = useState(false)

    // Confirm remove dialog
    const [removeOpen, setRemoveOpen] = useState(false)
    const [removeTarget, setRemoveTarget] = useState(null)

    // Install state
    const [installing, setInstalling] = useState(false)
    const [installLogs, setInstallLogs] = useState([])
    const [installDone, setInstallDone] = useState(false)
    const [installError, setInstallError] = useState(false)
    const installLogsEndRef = useRef(null)

    useEffect(() => {
        if (installLogsEndRef.current) {
            installLogsEndRef.current.scrollIntoView({ behavior: 'smooth' })
        }
    }, [installLogs])

    const handleInstallFirewalld = () => {
        setInstalling(true)
        setInstallLogs([])
        setInstallDone(false)
        setInstallError(false)

        const token = localStorage.getItem('token')
        fetch('/api/plugins/firewall/install', {
            method: 'POST',
            headers: { 'Authorization': `Bearer ${token}` },
        }).then(async (response) => {
            const reader = response.body.getReader()
            const decoder = new TextDecoder()
            let buffer = ''

            while (true) {
                const { done, value } = await reader.read()
                if (done) break
                buffer += decoder.decode(value, { stream: true })

                const lines = buffer.split('\n')
                buffer = lines.pop() || ''

                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        setInstallLogs(prev => [...prev, line.slice(6)])
                    } else if (line.startsWith('event: done')) {
                        setInstallDone(true)
                    } else if (line.startsWith('event: error')) {
                        setInstallError(true)
                    }
                }
            }
            setInstallDone(prev => prev || true)
        }).catch((err) => {
            setInstallLogs(prev => [...prev, `ERROR: ${err.message}`])
            setInstallError(true)
        }).finally(() => {
            setInstalling(false)
        })
    }

    const fetchData = useCallback(async () => {
        try {
            const statusRes = await firewallAPI.status()
            setStatus(statusRes.data)

            if (statusRes.data?.running) {
                const [zonesRes, servicesRes] = await Promise.allSettled([
                    firewallAPI.zones(),
                    firewallAPI.availableServices(),
                ])
                if (zonesRes.status === 'fulfilled') {
                    const z = zonesRes.value.data?.zones || []
                    setZones(z)
                    if (z.length > 0) {
                        setActiveTab(prev => {
                            if (prev) return prev
                            const active = z.find((zone) => zone.active)
                            return active ? active.name : z[0].name
                        })
                    }
                }
                if (servicesRes.status === 'fulfilled') {
                    setAvailableServices(servicesRes.value.data?.services || [])
                }
            }
        } catch (e) {
            console.error('Failed to fetch firewall data:', e)
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => { fetchData() }, [])

    const handleReload = async () => {
        try {
            await firewallAPI.reload()
            await fetchData()
        } catch (e) {
            console.error('Reload failed:', e)
        }
    }

    const handleAddRule = async () => {
        setSubmitting(true)
        try {
            const zone = ruleZone || activeTab
            if (ruleType === 'port') {
                await firewallAPI.addPort({ zone, port: rulePort, protocol: ruleProtocol })
            } else if (ruleType === 'service') {
                await firewallAPI.addService({ zone, service: ruleService })
            } else if (ruleType === 'rich_rule') {
                await firewallAPI.addRichRule({ zone, rule: ruleRichRule })
            }
            setAddOpen(false)
            resetForm()
            await fetchData()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally {
            setSubmitting(false)
        }
    }

    const handleRemove = async () => {
        if (!removeTarget) return
        try {
            const { type, zone, value, protocol } = removeTarget
            if (type === 'port') {
                const [p, proto] = value.split('/')
                await firewallAPI.removePort({ zone, port: p, protocol: proto || protocol })
            } else if (type === 'service') {
                await firewallAPI.removeService({ zone, service: value })
            } else if (type === 'rich_rule') {
                await firewallAPI.removeRichRule({ zone, rule: value })
            }
            setRemoveOpen(false)
            setRemoveTarget(null)
            await fetchData()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        }
    }

    const resetForm = () => {
        setRuleType('port')
        setRulePort('')
        setRuleProtocol('tcp')
        setRuleService('')
        setRuleRichRule('')
        setRuleZone('')
    }

    const openRemoveDialog = (type, zone, value, protocol) => {
        setRemoveTarget({ type, zone, value, protocol })
        setRemoveOpen(true)
    }

    if (loading) {
        return (
            <Box p="6">
                <Flex align="center" gap="2">
                    <Shield size={20} style={{ color: 'var(--cp-text-secondary)' }} />
                    <Text color="gray">{t('common.loading')}</Text>
                </Flex>
            </Box>
        )
    }

    // Not installed — show install UI
    if (status && !status.installed) {
        return (
            <Box p="6" style={{ maxWidth: 800, margin: '0 auto' }}>
                <Heading size="5" mb="4">{t('firewall.title')}</Heading>
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex direction="column" gap="3" p="2">
                        <Flex align="center" gap="2">
                            <AlertTriangle size={20} style={{ color: 'var(--orange-9)' }} />
                            <Text size="3" weight="bold">{t('firewall.not_installed')}</Text>
                        </Flex>
                        <Text size="2" color="gray">{t('firewall.install_hint')}</Text>

                        {installLogs.length > 0 && (
                            <Box style={{
                                background: 'var(--gray-2)', borderRadius: 8, padding: 12,
                                maxHeight: 300, overflowY: 'auto', fontFamily: 'monospace', fontSize: 12,
                            }}>
                                {installLogs.map((line, i) => (
                                    <Text key={i} size="1" as="div" style={{
                                        color: line.startsWith('ERROR') ? 'var(--red-9)' : 'var(--cp-text)',
                                        whiteSpace: 'pre-wrap', wordBreak: 'break-all',
                                    }}>{line}</Text>
                                ))}
                                <div ref={installLogsEndRef} />
                            </Box>
                        )}

                        <Flex gap="2" align="center">
                            {!installDone && !installError && (
                                <Button onClick={handleInstallFirewalld} disabled={installing}>
                                    <Download size={14} />
                                    {installing ? t('firewall.installing') : t('firewall.install_firewalld')}
                                </Button>
                            )}
                            {installDone && !installError && (
                                <Flex align="center" gap="2">
                                    <CheckCircle2 size={18} style={{ color: 'var(--green-9)' }} />
                                    <Text size="2" color="green" weight="medium">{t('firewall.install_success')}</Text>
                                    <Button variant="soft" onClick={() => window.location.reload()}>
                                        <RefreshCw size={14} /> {t('firewall.reload')}
                                    </Button>
                                </Flex>
                            )}
                            {installError && (
                                <Flex align="center" gap="2">
                                    <AlertCircle size={18} style={{ color: 'var(--red-9)' }} />
                                    <Text size="2" color="red" weight="medium">{t('firewall.install_failed')}</Text>
                                    <Button variant="soft" color="red" onClick={handleInstallFirewalld} disabled={installing}>
                                        <RefreshCw size={14} /> {t('firewall.retry_install')}
                                    </Button>
                                </Flex>
                            )}
                        </Flex>
                    </Flex>
                </Card>
            </Box>
        )
    }

    // Not running
    const handleStartFirewalld = async () => {
        setStarting(true)
        try {
            await firewallAPI.start()
            await fetchData()
        } catch (e) {
            alert(e.response?.data?.error || e.message)
        } finally {
            setStarting(false)
        }
    }

    if (status && !status.running) {
        return (
            <Box p="6" style={{ maxWidth: 800, margin: '0 auto' }}>
                <Heading size="5" mb="4">{t('firewall.title')}</Heading>
                <Callout.Root color="yellow" size="2">
                    <Callout.Icon><AlertTriangle size={18} /></Callout.Icon>
                    <Callout.Text>
                        <Text weight="bold">{t('firewall.not_running')}</Text>
                        <br />
                        {t('firewall.not_running_hint')}
                    </Callout.Text>
                </Callout.Root>
                <Flex mt="3" gap="2">
                    <Button onClick={handleStartFirewalld} disabled={starting}>
                        <Shield size={14} /> {starting ? t('common.loading') : t('firewall.start_firewalld')}
                    </Button>
                </Flex>
            </Box>
        )
    }

    const currentZone = zones.find((z) => z.name === activeTab) || zones[0]

    return (
        <Box p="6" style={{ maxWidth: 1100, margin: '0 auto' }}>
            {/* Header */}
            <Flex justify="between" align="center" mb="4" wrap="wrap" gap="3">
                <Box>
                    <Heading size="5">{t('firewall.title')}</Heading>
                    <Text size="2" color="gray">{t('firewall.subtitle')}</Text>
                </Box>
                <Flex gap="2">
                    <Button variant="soft" onClick={handleReload}>
                        <RefreshCw size={14} /> {t('firewall.reload')}
                    </Button>
                    <Button onClick={() => { resetForm(); setRuleZone(activeTab); setAddOpen(true) }}>
                        <Plus size={14} /> {t('firewall.add_rule')}
                    </Button>
                </Flex>
            </Flex>

            {/* Status bar */}
            <Card mb="4" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                <Flex gap="5" p="1" wrap="wrap">
                    <Flex direction="column" gap="1">
                        <Text size="1" color="gray">Status</Text>
                        <Badge color="green" size="1">{t('firewall.status_running')}</Badge>
                    </Flex>
                    {status?.version && (
                        <Flex direction="column" gap="1">
                            <Text size="1" color="gray">{t('firewall.version')}</Text>
                            <Text size="2" weight="medium">{status.version}</Text>
                        </Flex>
                    )}
                    {status?.default_zone && (
                        <Flex direction="column" gap="1">
                            <Text size="1" color="gray">{t('firewall.default_zone')}</Text>
                            <Text size="2" weight="medium">{status.default_zone}</Text>
                        </Flex>
                    )}
                </Flex>
            </Card>

            {/* Docker warning */}
            <Callout.Root color="blue" size="1" mb="4">
                <Callout.Icon><Info size={16} /></Callout.Icon>
                <Callout.Text>{t('firewall.docker_warning')}</Callout.Text>
            </Callout.Root>

            {/* Zone tabs */}
            {zones.length > 0 && (
                <Tabs.Root value={activeTab} onValueChange={setActiveTab}>
                    <Tabs.List>
                        {zones.map((z) => (
                            <Tabs.Trigger key={z.name} value={z.name}>
                                {z.name}
                                {z.active && <Badge color="green" size="1" ml="1">{t('firewall.active')}</Badge>}
                            </Tabs.Trigger>
                        ))}
                    </Tabs.List>

                    {zones.map((z) => (
                        <Tabs.Content key={z.name} value={z.name}>
                            <Box mt="4">
                                {/* Zone info */}
                                <Flex gap="4" mb="4" wrap="wrap">
                                    {z.target && (
                                        <Text size="2" color="gray">
                                            {t('firewall.target')}: <Text weight="medium">{z.target}</Text>
                                        </Text>
                                    )}
                                    {z.interfaces?.length > 0 && (
                                        <Text size="2" color="gray">
                                            {t('firewall.interfaces')}: {z.interfaces.map((i) => (
                                                <Badge key={i} variant="outline" size="1" mr="1">{i}</Badge>
                                            ))}
                                        </Text>
                                    )}
                                    {z.sources?.length > 0 && (
                                        <Text size="2" color="gray">
                                            {t('firewall.sources')}: {z.sources.map((s) => (
                                                <Badge key={s} variant="outline" size="1" mr="1">{s}</Badge>
                                            ))}
                                        </Text>
                                    )}
                                </Flex>

                                {/* Ports */}
                                <Card mb="3" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                                    <Flex justify="between" align="center" mb="2">
                                        <Text size="3" weight="bold">{t('firewall.ports')}</Text>
                                    </Flex>
                                    {z.ports?.length > 0 ? (
                                        <Table.Root variant="surface" size="1">
                                            <Table.Header>
                                                <Table.Row>
                                                    <Table.ColumnHeaderCell>{t('firewall.port')}</Table.ColumnHeaderCell>
                                                    <Table.ColumnHeaderCell>{t('firewall.protocol')}</Table.ColumnHeaderCell>
                                                    <Table.ColumnHeaderCell width="60px"></Table.ColumnHeaderCell>
                                                </Table.Row>
                                            </Table.Header>
                                            <Table.Body>
                                                {z.ports.map((portStr) => {
                                                    const [port, proto] = portStr.split('/')
                                                    return (
                                                        <Table.Row key={portStr}>
                                                            <Table.Cell><Text size="2" weight="medium">{port}</Text></Table.Cell>
                                                            <Table.Cell><Badge size="1" variant="soft">{proto}</Badge></Table.Cell>
                                                            <Table.Cell>
                                                                <Button
                                                                    size="1" variant="ghost" color="red"
                                                                    onClick={() => openRemoveDialog('port', z.name, portStr)}
                                                                >
                                                                    <Trash2 size={14} />
                                                                </Button>
                                                            </Table.Cell>
                                                        </Table.Row>
                                                    )
                                                })}
                                            </Table.Body>
                                        </Table.Root>
                                    ) : (
                                        <Text size="2" color="gray">{t('firewall.no_ports')}</Text>
                                    )}
                                </Card>

                                {/* Services */}
                                <Card mb="3" style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                                    <Flex justify="between" align="center" mb="2">
                                        <Text size="3" weight="bold">{t('firewall.services')}</Text>
                                    </Flex>
                                    {z.services?.length > 0 ? (
                                        <Flex gap="2" wrap="wrap">
                                            {z.services.map((svc) => (
                                                <Badge key={svc} size="2" variant="soft" color="blue" style={{ cursor: 'default' }}>
                                                    {svc}
                                                    <Button
                                                        size="1" variant="ghost" color="red"
                                                        style={{ marginLeft: 4, padding: 0, minWidth: 'auto', height: 'auto' }}
                                                        onClick={() => openRemoveDialog('service', z.name, svc)}
                                                    >
                                                        <Trash2 size={12} />
                                                    </Button>
                                                </Badge>
                                            ))}
                                        </Flex>
                                    ) : (
                                        <Text size="2" color="gray">{t('firewall.no_services')}</Text>
                                    )}
                                </Card>

                                {/* Rich Rules */}
                                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                                    <Flex justify="between" align="center" mb="2">
                                        <Text size="3" weight="bold">{t('firewall.rich_rules')}</Text>
                                    </Flex>
                                    {z.rich_rules?.length > 0 ? (
                                        <Table.Root variant="surface" size="1">
                                            <Table.Header>
                                                <Table.Row>
                                                    <Table.ColumnHeaderCell>{t('firewall.rich_rule_text')}</Table.ColumnHeaderCell>
                                                    <Table.ColumnHeaderCell width="60px"></Table.ColumnHeaderCell>
                                                </Table.Row>
                                            </Table.Header>
                                            <Table.Body>
                                                {z.rich_rules.map((rule, idx) => (
                                                    <Table.Row key={idx}>
                                                        <Table.Cell>
                                                            <Text size="1" style={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>
                                                                {rule}
                                                            </Text>
                                                        </Table.Cell>
                                                        <Table.Cell>
                                                            <Button
                                                                size="1" variant="ghost" color="red"
                                                                onClick={() => openRemoveDialog('rich_rule', z.name, rule)}
                                                            >
                                                                <Trash2 size={14} />
                                                            </Button>
                                                        </Table.Cell>
                                                    </Table.Row>
                                                ))}
                                            </Table.Body>
                                        </Table.Root>
                                    ) : (
                                        <Text size="2" color="gray">{t('firewall.no_rich_rules')}</Text>
                                    )}
                                </Card>
                            </Box>
                        </Tabs.Content>
                    ))}
                </Tabs.Root>
            )}

            {/* Add Rule Dialog */}
            <Dialog.Root open={addOpen} onOpenChange={setAddOpen}>
                <Dialog.Content maxWidth="480px">
                    <Dialog.Title>{t('firewall.add_rule')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        <Box>
                            <Text size="2" weight="medium" mb="1">{t('firewall.zone')}</Text>
                            <Select.Root value={ruleZone || activeTab} onValueChange={setRuleZone}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    {zones.map((z) => (
                                        <Select.Item key={z.name} value={z.name}>{z.name}</Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </Box>
                        <Box>
                            <Text size="2" weight="medium" mb="1">{t('firewall.rule_type')}</Text>
                            <Select.Root value={ruleType} onValueChange={setRuleType}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    <Select.Item value="port">{t('firewall.port')}</Select.Item>
                                    <Select.Item value="service">{t('firewall.services')}</Select.Item>
                                    <Select.Item value="rich_rule">{t('firewall.rich_rules')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Box>

                        {ruleType === 'port' && (
                            <>
                                <Box>
                                    <Text size="2" weight="medium" mb="1">{t('firewall.port')}</Text>
                                    <TextField.Root
                                        placeholder={t('firewall.port_placeholder')}
                                        value={rulePort}
                                        onChange={(e) => setRulePort(e.target.value)}
                                    />
                                </Box>
                                <Box>
                                    <Text size="2" weight="medium" mb="1">{t('firewall.protocol')}</Text>
                                    <Select.Root value={ruleProtocol} onValueChange={setRuleProtocol}>
                                        <Select.Trigger style={{ width: '100%' }} />
                                        <Select.Content>
                                            <Select.Item value="tcp">TCP</Select.Item>
                                            <Select.Item value="udp">UDP</Select.Item>
                                        </Select.Content>
                                    </Select.Root>
                                </Box>
                            </>
                        )}

                        {ruleType === 'service' && (
                            <Box>
                                <Text size="2" weight="medium" mb="1">{t('firewall.service_name')}</Text>
                                <Select.Root value={ruleService} onValueChange={setRuleService}>
                                    <Select.Trigger placeholder={t('firewall.service_name')} style={{ width: '100%' }} />
                                    <Select.Content>
                                        {availableServices.map((s) => (
                                            <Select.Item key={s} value={s}>{s}</Select.Item>
                                        ))}
                                    </Select.Content>
                                </Select.Root>
                            </Box>
                        )}

                        {ruleType === 'rich_rule' && (
                            <Box>
                                <Text size="2" weight="medium" mb="1">{t('firewall.rich_rule_text')}</Text>
                                <TextField.Root
                                    placeholder={t('firewall.rich_rule_placeholder')}
                                    value={ruleRichRule}
                                    onChange={(e) => setRuleRichRule(e.target.value)}
                                />
                            </Box>
                        )}
                    </Flex>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button onClick={handleAddRule} disabled={submitting}>
                            {submitting ? t('common.saving') : t('firewall.add_rule')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Confirm Remove Dialog */}
            <Dialog.Root open={removeOpen} onOpenChange={setRemoveOpen}>
                <Dialog.Content maxWidth="420px">
                    <Dialog.Title>{t('firewall.remove')}</Dialog.Title>
                    <Text size="2" color="gray" mt="2">
                        {removeTarget?.type === 'port' && t('firewall.confirm_remove_port', { port: removeTarget?.value })}
                        {removeTarget?.type === 'service' && t('firewall.confirm_remove_service', { service: removeTarget?.value })}
                        {removeTarget?.type === 'rich_rule' && t('firewall.confirm_remove_rich_rule')}
                    </Text>
                    <Flex gap="3" mt="4" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button color="red" onClick={handleRemove}>{t('firewall.remove')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
