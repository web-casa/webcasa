import { useState, useEffect, useCallback, useRef as useReactRef } from 'react'
import {
    Box, Flex, Heading, Text, Button, Badge, Switch, Table, Dialog,
    TextField, Callout, IconButton, Card, Tooltip, Spinner, AlertDialog,
    Select, Tabs, Separator,
} from '@radix-ui/themes'
import {
    Plus, Pencil, Trash2, Globe, AlertCircle, X, ChevronRight,
    ArrowRightLeft, Shield, Lock, Copy, CheckCircle, AlertTriangle, Circle,
    FolderOpen, Tags, Layers,
} from 'lucide-react'
import { hostAPI, dnsProviderAPI, settingAPI, certificateAPI, dnsCheckAPI, groupAPI, tagAPI, templateAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

const DEFAULT_FORM = {
    domain: '',
    host_type: 'proxy',
    tls_enabled: true,
    http_redirect: true,
    websocket: false,
    enabled: true,
    upstreams: [{ address: '' }],
    redirect_url: '',
    redirect_code: 301,
    custom_headers: [],
    access_rules: [],
    basic_auths: [],
    custom_directives: '',
    compression: false,
    cors_enabled: false,
    cors_origins: '*',
    cors_methods: 'GET, POST, PUT, DELETE, OPTIONS',
    cors_headers: 'Content-Type, Authorization',
    security_headers: false,
    error_page_path: '',
    cache_enabled: false,
    cache_ttl: 300,
    tls_mode: 'auto',
    dns_provider_id: null,
    group_id: null,
    tag_ids: [],
}

// ============ Host Form Dialog ============
function HostFormDialog({ open, onClose, onSaved, host }) {
    const { t } = useTranslation()
    const [form, setForm] = useState({ ...DEFAULT_FORM })
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    const [dnsProviders, setDnsProviders] = useState([])
    const [serverIPs, setServerIPs] = useState({ ipv4: '', ipv6: '' })
    const [certificates, setCertificates] = useState([])
    const [certFile, setCertFile] = useState(null)
    const [keyFile, setKeyFile] = useState(null)
    const [uploadingCert, setUploadingCert] = useState(false)
    const certFileRef = useReactRef(null)
    const keyFileRef = useReactRef(null)
    const dnsTimerRef = useReactRef(null)
    const isEdit = !!host
    const [dnsResult, setDnsResult] = useState(null)
    const [dnsChecking, setDnsChecking] = useState(false)
    const [groups, setGroups] = useState([])
    const [allTags, setAllTags] = useState([])

    useEffect(() => {
        dnsProviderAPI.list().then(res => setDnsProviders(res.data.providers || [])).catch(() => { })
        settingAPI.getAll().then(res => {
            const s = res.data.settings || {}
            setServerIPs({ ipv4: s.server_ipv4 || '', ipv6: s.server_ipv6 || '' })
        }).catch(() => { })
        certificateAPI.list().then(res => setCertificates(res.data.certificates || [])).catch(() => { })
        groupAPI.list().then(res => setGroups(res.data.groups || [])).catch(() => { })
        tagAPI.list().then(res => setAllTags(res.data.tags || [])).catch(() => { })
    }, [])

    useEffect(() => {
        if (host) {
            setForm({
                domain: host.domain,
                host_type: host.host_type || 'proxy',
                tls_enabled: host.tls_enabled,
                http_redirect: host.http_redirect,
                websocket: host.websocket,
                enabled: host.enabled,
                upstreams: host.upstreams?.length
                    ? host.upstreams.map((u) => ({ address: u.address }))
                    : [{ address: '' }],
                redirect_url: host.redirect_url || '',
                redirect_code: host.redirect_code || 301,
                custom_headers: host.custom_headers || [],
                access_rules: host.access_rules || [],
                basic_auths: [], // never pre-fill passwords
                custom_directives: host.custom_directives || '',
                compression: host.compression || false,
                cors_enabled: host.cors_enabled || false,
                cors_origins: host.cors_origins || '*',
                cors_methods: host.cors_methods || 'GET, POST, PUT, DELETE, OPTIONS',
                cors_headers: host.cors_headers || 'Content-Type, Authorization',
                security_headers: host.security_headers || false,
                error_page_path: host.error_page_path || '',
                cache_enabled: host.cache_enabled || false,
                cache_ttl: host.cache_ttl || 300,
                tls_mode: host.tls_mode || 'auto',
                dns_provider_id: host.dns_provider_id || null,
                group_id: host.group_id || null,
                tag_ids: host.tags?.map(t => t.id) || [],
            })
        } else {
            setForm({ ...DEFAULT_FORM })
        }
        setError('')
        setDnsResult(null)
        setDnsChecking(false)
    }, [host, open])

    // Debounced DNS check on domain change
    useEffect(() => {
        if (dnsTimerRef.current) clearTimeout(dnsTimerRef.current)
        setDnsResult(null)
        if (!form.domain || form.domain.trim().length < 3) return
        setDnsChecking(true)
        dnsTimerRef.current = setTimeout(() => {
            dnsCheckAPI.check(form.domain.trim())
                .then((res) => setDnsResult(res.data))
                .catch(() => setDnsResult(null))
                .finally(() => setDnsChecking(false))
        }, 800)
        return () => {
            if (dnsTimerRef.current) clearTimeout(dnsTimerRef.current)
        }
    }, [form.domain])

    const handleUploadCert = async () => {
        setUploadingCert(true)
        try {
            const fd = new FormData()
            fd.append('name', form.domain || 'cert-' + Date.now())
            fd.append('cert', certFile)
            fd.append('key', keyFile)
            const res = await certificateAPI.upload(fd)
            const newCert = res.data.certificate
            setForm({ ...form, certificate_id: newCert.id })
            setCertFile(null)
            setKeyFile(null)
            certificateAPI.list().then(r => setCertificates(r.data.certificates || []))
        } catch (err) {
            setError(err.response?.data?.error || t('cert.upload_failed'))
        }
        setUploadingCert(false)
    }

    const handleSave = async () => {
        setError('')
        setSaving(true)
        try {
            const payload = {
                ...form,
                upstreams: form.host_type === 'proxy'
                    ? form.upstreams.filter((u) => u.address.trim())
                    : [],
                basic_auths: form.basic_auths.filter((a) => a.username && a.password),
                group_id: form.group_id || 0,
                tag_ids: form.tag_ids || [],
            }
            if (isEdit) {
                await hostAPI.update(host.id, payload)
            } else {
                await hostAPI.create(payload)
            }
            onSaved()
            onClose()
        } catch (err) {
            setError(err.response?.data?.error || t('host.save_failed'))
        } finally {
            setSaving(false)
        }
    }

    const addUpstream = () => {
        setForm({ ...form, upstreams: [...form.upstreams, { address: '' }] })
    }

    const removeUpstream = (idx) => {
        const upstreams = form.upstreams.filter((_, i) => i !== idx)
        setForm({ ...form, upstreams: upstreams.length ? upstreams : [{ address: '' }] })
    }

    const updateUpstream = (idx, value) => {
        const upstreams = [...form.upstreams]
        upstreams[idx] = { address: value }
        setForm({ ...form, upstreams })
    }

    const addBasicAuth = () => {
        setForm({ ...form, basic_auths: [...form.basic_auths, { username: '', password: '' }] })
    }

    const removeBasicAuth = (idx) => {
        setForm({ ...form, basic_auths: form.basic_auths.filter((_, i) => i !== idx) })
    }

    const isProxy = form.host_type === 'proxy'
    const isStatic = form.host_type === 'static'
    const isPHP = form.host_type === 'php'
    const isRedirect = form.host_type === 'redirect'
    const needsRoot = isStatic || isPHP

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <Dialog.Content maxWidth="560px" style={{ background: 'var(--cp-card)' }}>
                <Dialog.Title>
                    {isEdit ? t('host.edit_host') : t('host.add_host')}
                </Dialog.Title>
                <Dialog.Description size="2" color="gray" mb="4">
                    {isProxy ? t('host.proxy') : isRedirect ? t('host.redirect') : isStatic ? t('host.static') : t('host.php')}
                </Dialog.Description>

                <Flex direction="column" gap="4">
                    {error && (
                        <Callout.Root color="red" size="1">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{error}</Callout.Text>
                        </Callout.Root>
                    )}

                    {/* Host Type + Domain */}
                    <Flex gap="3" align="end">
                        <Flex direction="column" gap="1" style={{ width: 140 }}>
                            <Text size="2" weight="medium">{t('host.type')}</Text>
                            <Select.Root
                                value={form.host_type}
                                onValueChange={(v) => setForm({ ...form, host_type: v })}
                                size="2"
                            >
                                <Select.Trigger />
                                <Select.Content>
                                    <Select.Item value="proxy">{t('host.proxy')}</Select.Item>
                                    <Select.Item value="redirect">{t('host.redirect')}</Select.Item>
                                    <Select.Item value="static">{t('host.static')}</Select.Item>
                                    <Select.Item value="php">{t('host.php')}</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Flex>
                        <Flex direction="column" gap="1" style={{ flex: 1 }}>
                            <Text size="2" weight="medium">{t('host.domain')}</Text>
                            <TextField.Root
                                placeholder="example.com"
                                value={form.domain}
                                onChange={(e) => setForm({ ...form, domain: e.target.value })}
                                size="2"
                            />
                            {dnsChecking && (
                                <Flex align="center" gap="1">
                                    <Spinner size="1" />
                                    <Text size="1" color="gray">{t('dns_check.checking')}</Text>
                                </Flex>
                            )}
                            {!dnsChecking && dnsResult && (
                                <Flex align="center" gap="1">
                                    <DnsStatusIcon status={dnsResult.status} dnsResult={dnsResult} t={t} />
                                    <Text size="1" color={
                                        dnsResult.status === 'matched' ? 'green' :
                                        dnsResult.status === 'mismatched' ? 'yellow' : 'gray'
                                    }>
                                        {t(`dns_check.${dnsResult.status}`)}
                                    </Text>
                                </Flex>
                            )}
                        </Flex>
                    </Flex>

                    {/* Group & Tags */}
                    <Flex gap="3" align="end">
                        <Flex direction="column" gap="1" style={{ flex: 1 }}>
                            <Text size="2" weight="medium">{t('group.label')}</Text>
                            <Select.Root
                                value={form.group_id ? String(form.group_id) : ''}
                                onValueChange={(v) => setForm({ ...form, group_id: v ? Number(v) : null })}
                                size="2"
                            >
                                <Select.Trigger placeholder={t('group.select_placeholder')} />
                                <Select.Content>
                                    <Select.Item value="">{t('group.none')}</Select.Item>
                                    {groups.map(g => (
                                        <Select.Item key={g.id} value={String(g.id)}>
                                            <Flex align="center" gap="2">
                                                <Box style={{ width: 8, height: 8, borderRadius: '50%', background: g.color || '#9ca3af' }} />
                                                {g.name}
                                            </Flex>
                                        </Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </Flex>
                        <Flex direction="column" gap="1" style={{ flex: 1 }}>
                            <Text size="2" weight="medium">{t('tag.label')}</Text>
                            <Flex gap="1" wrap="wrap" style={{ minHeight: 32, padding: '4px 0' }}>
                                {allTags.map(tag => {
                                    const selected = form.tag_ids.includes(tag.id)
                                    return (
                                        <Badge
                                            key={tag.id}
                                            size="1"
                                            variant={selected ? 'solid' : 'outline'}
                                            color={tag.color || 'gray'}
                                            style={{ cursor: 'pointer', userSelect: 'none' }}
                                            onClick={() => {
                                                const ids = selected
                                                    ? form.tag_ids.filter(id => id !== tag.id)
                                                    : [...form.tag_ids, tag.id]
                                                setForm({ ...form, tag_ids: ids })
                                            }}
                                        >
                                            {tag.name}
                                        </Badge>
                                    )
                                })}
                                {allTags.length === 0 && (
                                    <Text size="1" color="gray">{t('tag.no_tags')}</Text>
                                )}
                            </Flex>
                        </Flex>
                    </Flex>

                    <Tabs.Root defaultValue="main">
                        <Tabs.List>
                            <Tabs.Trigger value="main">
                                {isProxy ? t('host.upstream') : isRedirect ? t('host.redirect') : t('host.options')}
                            </Tabs.Trigger>
                            <Tabs.Trigger value="options">{t('host.options')}</Tabs.Trigger>
                            <Tabs.Trigger value="auth">
                                <Lock size={12} style={{ marginRight: 4 }} />
                                {t('common.auth')}
                            </Tabs.Trigger>
                        </Tabs.List>

                        {/* Tab 1: Main config */}
                        <Tabs.Content value="main">
                            <Box pt="3">
                                {isProxy ? (
                                    /* Proxy: Upstreams */
                                    <Flex direction="column" gap="2">
                                        <Flex justify="between" align="center">
                                            <Text size="2" weight="medium">{t('host.upstreams')}</Text>
                                            <Button variant="ghost" size="1" onClick={addUpstream}>
                                                <Plus size={14} /> {t('common.add')}
                                            </Button>
                                        </Flex>
                                        {form.upstreams.map((u, i) => (
                                            <Flex key={i} gap="2" align="center">
                                                <TextField.Root
                                                    style={{ flex: 1 }}
                                                    placeholder="localhost:3000 or https://example.com"
                                                    value={u.address}
                                                    onChange={(e) => updateUpstream(i, e.target.value)}
                                                    size="2"
                                                />
                                                {form.upstreams.length > 1 && (
                                                    <IconButton
                                                        variant="ghost"
                                                        color="red"
                                                        size="1"
                                                        onClick={() => removeUpstream(i)}
                                                    >
                                                        <X size={14} />
                                                    </IconButton>
                                                )}
                                            </Flex>
                                        ))}
                                    </Flex>
                                ) : isRedirect ? (
                                    /* Redirect: Target URL + Code */
                                    <Flex direction="column" gap="3">
                                        <Flex direction="column" gap="1">
                                            <Text size="2" weight="medium">{t('host.redirect_url')}</Text>
                                            <TextField.Root
                                                placeholder="https://new-site.com"
                                                value={form.redirect_url}
                                                onChange={(e) => setForm({ ...form, redirect_url: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">
                                                {t('host.redirect_url_hint')}
                                            </Text>
                                        </Flex>
                                        <Flex direction="column" gap="1" style={{ width: 200 }}>
                                            <Text size="2" weight="medium">{t('host.redirect_code')}</Text>
                                            <Select.Root
                                                value={String(form.redirect_code)}
                                                onValueChange={(v) => setForm({ ...form, redirect_code: Number(v) })}
                                                size="2"
                                            >
                                                <Select.Trigger />
                                                <Select.Content>
                                                    <Select.Item value="301">{t('host.301_permanent')}</Select.Item>
                                                    <Select.Item value="302">{t('host.302_temporary')}</Select.Item>
                                                </Select.Content>
                                            </Select.Root>
                                        </Flex>
                                    </Flex>
                                ) : (
                                    /* Static / PHP: Root path + options */
                                    <Flex direction="column" gap="3">
                                        <Flex direction="column" gap="1">
                                            <Text size="2" weight="medium">{t('host.root_path')}</Text>
                                            <TextField.Root
                                                placeholder="/var/www/my-site"
                                                value={form.root_path || ''}
                                                onChange={(e) => setForm({ ...form, root_path: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">
                                                {isStatic ? t('host.static_root_hint') : t('host.php_root_hint')}
                                            </Text>
                                        </Flex>
                                        {isPHP && (
                                            <Flex direction="column" gap="1">
                                                <Text size="2" weight="medium">{t('host.php_fastcgi')}</Text>
                                                <TextField.Root
                                                    placeholder="localhost:9000"
                                                    value={form.php_fastcgi || ''}
                                                    onChange={(e) => setForm({ ...form, php_fastcgi: e.target.value })}
                                                    size="2"
                                                />
                                                <Text size="1" color="gray">
                                                    {t('host.php_fastcgi_hint')}
                                                </Text>
                                            </Flex>
                                        )}
                                        {isStatic && (
                                            <Flex justify="between" align="center">
                                                <Flex direction="column">
                                                    <Text size="2" weight="medium">{t('host.directory_browse')}</Text>
                                                    <Text size="1" color="gray">{t('host.directory_browse_hint')}</Text>
                                                </Flex>
                                                <Switch
                                                    checked={form.directory_browse || false}
                                                    onCheckedChange={(v) => setForm({ ...form, directory_browse: v })}
                                                />
                                            </Flex>
                                        )}
                                        <Flex direction="column" gap="1">
                                            <Text size="2" weight="medium">{t('host.index_files')}</Text>
                                            <TextField.Root
                                                placeholder="index.html index.htm"
                                                value={form.index_files || ''}
                                                onChange={(e) => setForm({ ...form, index_files: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">{t('host.index_files_hint')}</Text>
                                        </Flex>
                                    </Flex>
                                )}
                            </Box>
                        </Tabs.Content>

                        {/* Tab 2: Options */}
                        <Tabs.Content value="options">
                            <Card mt="3" style={{ background: 'var(--cp-input-bg)', border: '1px solid var(--cp-border-subtle)' }}>
                                <Flex direction="column" gap="3" p="1">
                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.tls_mode')}</Text>
                                            <Text size="1" color="gray">{t('host.tls_mode_hint')}</Text>
                                        </Flex>
                                        <Select.Root
                                            value={form.tls_mode || 'auto'}
                                            onValueChange={(v) => setForm({ ...form, tls_mode: v, tls_enabled: v !== 'off' })}
                                            size="2"
                                        >
                                            <Select.Trigger style={{ width: 160 }} />
                                            <Select.Content>
                                                <Select.Item value="auto">{t('host.tls_auto')}</Select.Item>
                                                <Select.Item value="dns">{t('host.tls_dns')}</Select.Item>
                                                <Select.Item value="wildcard">{t('host.tls_wildcard')}</Select.Item>
                                                <Select.Item value="custom">{t('host.tls_custom')}</Select.Item>
                                                <Select.Item value="off">{t('host.tls_off')}</Select.Item>
                                            </Select.Content>
                                        </Select.Root>
                                    </Flex>

                                    {form.tls_mode === 'auto' && (serverIPs.ipv4 || serverIPs.ipv6) && form.domain && (
                                        <Callout.Root color="blue" size="1">
                                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                                            <Callout.Text>
                                                <Text size="1">{t('host.dns_record_hint')}</Text>
                                                {serverIPs.ipv4 && (
                                                    <Flex align="center" gap="1" mt="1">
                                                        <code style={{ fontSize: '0.75rem' }}>{form.domain} → {serverIPs.ipv4} (A)</code>
                                                        <IconButton size="1" variant="ghost" onClick={() => navigator.clipboard.writeText(serverIPs.ipv4)}>
                                                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" /></svg>
                                                        </IconButton>
                                                    </Flex>
                                                )}
                                                {serverIPs.ipv6 && (
                                                    <Flex align="center" gap="1" mt="1">
                                                        <code style={{ fontSize: '0.75rem' }}>{form.domain} → {serverIPs.ipv6} (AAAA)</code>
                                                        <IconButton size="1" variant="ghost" onClick={() => navigator.clipboard.writeText(serverIPs.ipv6)}>
                                                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" /></svg>
                                                        </IconButton>
                                                    </Flex>
                                                )}
                                            </Callout.Text>
                                        </Callout.Root>
                                    )}

                                    {(form.tls_mode === 'dns' || form.tls_mode === 'wildcard') && (
                                        <Flex direction="column" gap="1" pl="4" style={{ borderLeft: '2px solid var(--cp-border-subtle)' }}>
                                            <Text size="1" color="gray">{t('host.dns_provider')}</Text>
                                            <Select.Root
                                                value={form.dns_provider_id ? String(form.dns_provider_id) : ''}
                                                onValueChange={(v) => setForm({ ...form, dns_provider_id: v ? Number(v) : null })}
                                                size="2"
                                            >
                                                <Select.Trigger placeholder={t('host.dns_provider')} />
                                                <Select.Content>
                                                    {dnsProviders.map(p => (
                                                        <Select.Item key={p.id} value={String(p.id)}>
                                                            {p.name} ({p.provider})
                                                        </Select.Item>
                                                    ))}
                                                </Select.Content>
                                            </Select.Root>
                                            {dnsProviders.length === 0 && (
                                                <Text size="1" color="red">{t('dns.no_providers_hint')}</Text>
                                            )}
                                        </Flex>
                                    )}

                                    {form.tls_mode === 'custom' && (
                                        <Flex direction="column" gap="2" pl="4" style={{ borderLeft: '2px solid var(--cp-border-subtle)' }}>
                                            <Text size="1" color="gray">{t('cert.title')}</Text>
                                            <Select.Root
                                                value={form.certificate_id ? String(form.certificate_id) : ''}
                                                onValueChange={(v) => setForm({ ...form, certificate_id: v ? Number(v) : null })}
                                                size="2"
                                            >
                                                <Select.Trigger placeholder={t('host.select_cert_hint')} />
                                                <Select.Content>
                                                    {certificates.map(c => (
                                                        <Select.Item key={c.id} value={String(c.id)}>
                                                            {c.name} — {c.domains || t('common.unknown')}
                                                        </Select.Item>
                                                    ))}
                                                </Select.Content>
                                            </Select.Root>
                                            {certificates.length === 0 && (
                                                <Text size="1" color="orange">
                                                    {t('host.no_cert_hint')}
                                                </Text>
                                            )}

                                            <Separator size="4" />

                                            <Text size="1" color="gray">{t('host.upload_cert_hint')}</Text>
                                            <Flex gap="2">
                                                <Button
                                                    variant="soft" color="gray" size="1"
                                                    onClick={() => certFileRef.current?.click()}
                                                >
                                                    {certFile ? certFile.name : t('cert.choose_cert')}
                                                </Button>
                                                <Button
                                                    variant="soft" color="gray" size="1"
                                                    onClick={() => keyFileRef.current?.click()}
                                                >
                                                    {keyFile ? keyFile.name : t('cert.choose_key')}
                                                </Button>
                                                <input ref={certFileRef} type="file" accept=".pem,.crt,.cer" onChange={(e) => setCertFile(e.target.files?.[0])} style={{ display: 'none' }} />
                                                <input ref={keyFileRef} type="file" accept=".pem,.key" onChange={(e) => setKeyFile(e.target.files?.[0])} style={{ display: 'none' }} />
                                            </Flex>
                                            {certFile && keyFile && (
                                                <Button
                                                    size="1" variant="soft"
                                                    onClick={handleUploadCert}
                                                    disabled={uploadingCert}
                                                >
                                                    {uploadingCert ? t('common.loading') : t('host.upload_and_associate')}
                                                </Button>
                                            )}
                                        </Flex>
                                    )}

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.http_redirect')}</Text>
                                            <Text size="1" color="gray">{t('host.http_redirect_hint')}</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.http_redirect}
                                            onCheckedChange={(v) => setForm({ ...form, http_redirect: v })}
                                        />
                                    </Flex>

                                    {isProxy && (
                                        <Flex justify="between" align="center">
                                            <Flex direction="column">
                                                <Text size="2" weight="medium">{t('host.websocket')}</Text>
                                                <Text size="1" color="gray">{t('host.websocket_hint')}</Text>
                                            </Flex>
                                            <Switch
                                                checked={form.websocket}
                                                onCheckedChange={(v) => setForm({ ...form, websocket: v })}
                                            />
                                        </Flex>
                                    )}

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('host.performance')}</Text>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.compression')}</Text>
                                            <Text size="1" color="gray">{t('host.compression_hint')}</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.compression}
                                            onCheckedChange={(v) => setForm({ ...form, compression: v })}
                                        />
                                    </Flex>

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('host.security')}</Text>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.security_headers')}</Text>
                                            <Text size="1" color="gray">{t('host.security_headers_hint')}</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.security_headers}
                                            onCheckedChange={(v) => setForm({ ...form, security_headers: v })}
                                        />
                                    </Flex>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.cors')}</Text>
                                            <Text size="1" color="gray">{t('host.cors_hint')}</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.cors_enabled}
                                            onCheckedChange={(v) => setForm({ ...form, cors_enabled: v })}
                                        />
                                    </Flex>

                                    {form.cors_enabled && (
                                        <Flex direction="column" gap="2" pl="4" style={{ borderLeft: '2px solid var(--cp-border-subtle)' }}>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">{t('host.cors_origins')}</Text>
                                                <TextField.Root
                                                    value={form.cors_origins}
                                                    onChange={(e) => setForm({ ...form, cors_origins: e.target.value })}
                                                    placeholder="* or https://example.com"
                                                />
                                            </Box>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">{t('host.cors_methods')}</Text>
                                                <TextField.Root
                                                    value={form.cors_methods}
                                                    onChange={(e) => setForm({ ...form, cors_methods: e.target.value })}
                                                    placeholder="GET, POST, PUT, DELETE, OPTIONS"
                                                />
                                            </Box>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">{t('host.cors_headers')}</Text>
                                                <TextField.Root
                                                    value={form.cors_headers}
                                                    onChange={(e) => setForm({ ...form, cors_headers: e.target.value })}
                                                    placeholder="Content-Type, Authorization"
                                                />
                                            </Box>
                                        </Flex>
                                    )}

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('host.error_page')}</Text>

                                    <Box>
                                        <Text size="2" weight="medium" mb="1">{t('host.error_page_path')}</Text>
                                        <Text size="1" color="gray" mb="2" as="p">
                                            {t('host.error_page_hint')}
                                        </Text>
                                        <TextField.Root
                                            value={form.error_page_path}
                                            onChange={(e) => setForm({ ...form, error_page_path: e.target.value })}
                                            placeholder="/var/lib/caddypanel/error_pages"
                                        />
                                    </Box>

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>{t('host.advanced')}</Text>

                                    <Box>
                                        <Text size="2" weight="medium" mb="1">{t('host.custom_directives')}</Text>
                                        <Text size="1" color="gray" mb="2" as="p">
                                            {t('host.custom_directives_hint')}
                                        </Text>
                                        <textarea
                                            value={form.custom_directives}
                                            onChange={(e) => setForm({ ...form, custom_directives: e.target.value })}
                                            placeholder={'encode gzip zstd\nrate_limit {remote.ip} 10r/s'}
                                            rows={4}
                                            className="custom-textarea"
                                        />
                                    </Box>
                                </Flex>
                            </Card>
                        </Tabs.Content>

                        {/* Tab 3: Basic Auth */}
                        <Tabs.Content value="auth">
                            <Box pt="3">
                                <Flex direction="column" gap="2">
                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">{t('host.basic_auth')}</Text>
                                            <Text size="1" color="gray">{t('host.basic_auth_hint')}</Text>
                                        </Flex>
                                        <Button variant="ghost" size="1" onClick={addBasicAuth}>
                                            <Plus size={14} /> {t('host.add_auth_user')}
                                        </Button>
                                    </Flex>
                                    {form.basic_auths.length === 0 && (
                                        <Text size="2" color="gray" style={{ fontStyle: 'italic' }}>
                                            {t('host.no_auth_hint')}
                                        </Text>
                                    )}
                                    {form.basic_auths.map((auth, i) => (
                                        <Flex key={i} gap="2" align="center">
                                            <TextField.Root
                                                style={{ flex: 1 }}
                                                placeholder={t('common.username')}
                                                value={auth.username}
                                                onChange={(e) => {
                                                    const auths = [...form.basic_auths]
                                                    auths[i] = { ...auths[i], username: e.target.value }
                                                    setForm({ ...form, basic_auths: auths })
                                                }}
                                                size="2"
                                            />
                                            <TextField.Root
                                                style={{ flex: 1 }}
                                                placeholder={t('common.password')}
                                                type="password"
                                                value={auth.password}
                                                onChange={(e) => {
                                                    const auths = [...form.basic_auths]
                                                    auths[i] = { ...auths[i], password: e.target.value }
                                                    setForm({ ...form, basic_auths: auths })
                                                }}
                                                size="2"
                                            />
                                            <IconButton
                                                variant="ghost"
                                                color="red"
                                                size="1"
                                                onClick={() => removeBasicAuth(i)}
                                            >
                                                <X size={14} />
                                            </IconButton>
                                        </Flex>
                                    ))}
                                    {isEdit && host?.basic_auths?.length > 0 && form.basic_auths.length === 0 && (
                                        <Callout.Root size="1" color="blue">
                                            <Callout.Icon><Shield size={14} /></Callout.Icon>
                                            <Callout.Text>
                                                {t('host.existing_auth_hint', { count: host.basic_auths.length })}
                                            </Callout.Text>
                                        </Callout.Root>
                                    )}
                                </Flex>
                            </Box>
                        </Tabs.Content>
                    </Tabs.Root>
                </Flex>

                <Flex gap="3" mt="5" justify="between">
                    <Box>
                        {isEdit && host?.id && (
                            <SaveAsTemplateButton hostId={host.id} t={t} />
                        )}
                    </Box>
                    <Flex gap="3">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button
                            onClick={handleSave}
                            disabled={saving || !form.domain || (isProxy && !form.upstreams.some(u => u.address)) || (!isProxy && !form.redirect_url)}
                        >
                            {saving ? <Spinner size="1" /> : null}
                            {isEdit ? t('common.save') : t('common.create')}
                        </Button>
                    </Flex>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}

// ============ Save As Template Button ============
function SaveAsTemplateButton({ hostId, t }) {
    const [open, setOpen] = useState(false)
    const [name, setName] = useState('')
    const [description, setDescription] = useState('')
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    const [success, setSuccess] = useState(false)

    const handleOpen = () => {
        setName('')
        setDescription('')
        setError('')
        setSuccess(false)
        setSaving(false)
        setOpen(true)
    }

    const handleSave = async () => {
        if (!name.trim()) return
        setError('')
        setSaving(true)
        try {
            await templateAPI.saveAsTemplate(hostId, { name: name.trim(), description: description.trim() })
            setSuccess(true)
            setTimeout(() => setOpen(false), 1000)
        } catch (err) {
            setError(err.response?.data?.error || t('template.save_template_failed'))
        } finally {
            setSaving(false)
        }
    }

    return (
        <>
            <Button variant="soft" color="green" size="2" onClick={handleOpen}>
                <Layers size={14} /> {t('template.save_as_template')}
            </Button>
            <Dialog.Root open={open} onOpenChange={(o) => !o && setOpen(false)}>
                <Dialog.Content maxWidth="420px" style={{ background: 'var(--cp-card)' }}>
                    <Dialog.Title>{t('template.save_as_template_title')}</Dialog.Title>
                    <Flex direction="column" gap="4" mt="2">
                        <Text size="2" color="gray">{t('template.save_as_template_hint')}</Text>
                        {error && (
                            <Callout.Root color="red" size="1">
                                <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                                <Callout.Text>{error}</Callout.Text>
                            </Callout.Root>
                        )}
                        {success && (
                            <Callout.Root color="green" size="1">
                                <Callout.Icon><CheckCircle size={14} /></Callout.Icon>
                                <Callout.Text>{t('template.save_template_success')}</Callout.Text>
                            </Callout.Root>
                        )}
                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('template.name')}</Text>
                            <TextField.Root
                                placeholder={t('template.name_placeholder')}
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                size="2"
                            />
                        </Flex>
                        <Flex direction="column" gap="1">
                            <Text size="2" weight="medium">{t('template.description')}</Text>
                            <TextField.Root
                                placeholder={t('template.description_placeholder')}
                                value={description}
                                onChange={(e) => setDescription(e.target.value)}
                                size="2"
                            />
                        </Flex>
                        <Flex gap="3" justify="end">
                            <Button variant="soft" color="gray" onClick={() => setOpen(false)}>
                                {t('common.cancel')}
                            </Button>
                            <Button onClick={handleSave} disabled={saving || !name.trim() || success}>
                                {saving ? <Spinner size="1" /> : <Layers size={14} />}
                                {saving ? t('template.saving_template') : t('common.save')}
                            </Button>
                        </Flex>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </>
    )
}

// ============ DNS Status Icon ============
function DnsStatusIcon({ status, dnsResult, t }) {
    if (!status) return null
    let color, icon, tooltipContent
    if (status === 'matched') {
        color = '#22c55e'
        icon = <CheckCircle size={14} color={color} />
        tooltipContent = t('dns_check.tooltip_matched')
    } else if (status === 'mismatched') {
        color = '#eab308'
        icon = <AlertTriangle size={14} color={color} />
        const details = []
        if (dnsResult?.a_records?.length) details.push(`A: ${dnsResult.a_records.join(', ')}`)
        if (dnsResult?.aaaa_records?.length) details.push(`AAAA: ${dnsResult.aaaa_records.join(', ')}`)
        if (dnsResult?.expected_ipv4) details.push(`${t('dns_check.expected_ipv4')}: ${dnsResult.expected_ipv4}`)
        if (dnsResult?.expected_ipv6) details.push(`${t('dns_check.expected_ipv6')}: ${dnsResult.expected_ipv6}`)
        tooltipContent = `${t('dns_check.tooltip_mismatched')}\n${details.join('\n')}`
    } else {
        color = '#9ca3af'
        icon = <Circle size={14} color={color} />
        tooltipContent = t('dns_check.tooltip_no_record')
    }
    return (
        <Tooltip content={<Text size="1" style={{ whiteSpace: 'pre-line' }}>{tooltipContent}</Text>}>
            <span style={{ display: 'inline-flex', cursor: 'help' }}>{icon}</span>
        </Tooltip>
    )
}

// ============ Clone Dialog ============
function CloneDialog({ open, onClose, host, onCloned, t }) {
    const [newDomain, setNewDomain] = useState('')
    const [cloning, setCloning] = useState(false)
    const [error, setError] = useState('')

    useEffect(() => {
        if (open) {
            setNewDomain('')
            setError('')
            setCloning(false)
        }
    }, [open])

    const handleClone = async () => {
        if (!newDomain.trim()) return
        setError('')
        setCloning(true)
        try {
            await hostAPI.clone(host.id, { domain: newDomain.trim() })
            onCloned()
            onClose()
        } catch (err) {
            setError(err.response?.data?.error || t('clone.failed'))
        } finally {
            setCloning(false)
        }
    }

    return (
        <Dialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <Dialog.Content maxWidth="420px" style={{ background: 'var(--cp-card)' }}>
                <Dialog.Title>{t('clone.title')}</Dialog.Title>
                <Flex direction="column" gap="4" mt="2">
                    {error && (
                        <Callout.Root color="red" size="1">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>{error}</Callout.Text>
                        </Callout.Root>
                    )}
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('clone.source_domain')}</Text>
                        <TextField.Root value={host?.domain || ''} disabled size="2" />
                    </Flex>
                    <Flex direction="column" gap="1">
                        <Text size="2" weight="medium">{t('clone.new_domain')}</Text>
                        <TextField.Root
                            placeholder={t('clone.new_domain_placeholder')}
                            value={newDomain}
                            onChange={(e) => setNewDomain(e.target.value)}
                            size="2"
                            onKeyDown={(e) => e.key === 'Enter' && handleClone()}
                        />
                    </Flex>
                    <Flex gap="3" justify="end">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button onClick={handleClone} disabled={cloning || !newDomain.trim()}>
                            {cloning ? <Spinner size="1" /> : <Copy size={14} />}
                            {cloning ? t('clone.cloning') : t('clone.submit')}
                        </Button>
                    </Flex>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}

// ============ Delete Confirmation ============
function DeleteDialog({ open, onClose, host, onConfirm }) {
    const { t } = useTranslation()
    const [deleting, setDeleting] = useState(false)
    const handleDelete = async () => {
        setDeleting(true)
        await onConfirm()
        setDeleting(false)
    }
    return (
        <AlertDialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <AlertDialog.Content maxWidth="400px" style={{ background: 'var(--cp-card)' }}>
                <AlertDialog.Title>{t('host.delete_title')}</AlertDialog.Title>
                <AlertDialog.Description size="2">
                    {t('host.confirm_delete', { domain: host?.domain })}
                </AlertDialog.Description>
                <Flex gap="3" mt="4" justify="end">
                    <AlertDialog.Cancel>
                        <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                    </AlertDialog.Cancel>
                    <AlertDialog.Action>
                        <Button color="red" onClick={handleDelete} disabled={deleting}>
                            {deleting ? <Spinner size="1" /> : <Trash2 size={14} />}
                            {t('common.delete')}
                        </Button>
                    </AlertDialog.Action>
                </Flex>
            </AlertDialog.Content>
        </AlertDialog.Root>
    )
}

// ============ Mobile Host Card ============
function HostCard({ host, t, onEdit, onDelete, onToggle, onClone, toggling, dnsStatus }) {
    return (
        <Box className="mobile-host-card" mb="3">
            <Flex justify="between" align="start" mb="2">
                <Flex align="center" gap="2">
                    {host.host_type === 'redirect' ? (
                        <ArrowRightLeft size={14} color="#f59e0b" />
                    ) : host.host_type === 'static' ? (
                        <Globe size={14} color="#3b82f6" />
                    ) : host.host_type === 'php' ? (
                        <Globe size={14} color="#8b5cf6" />
                    ) : (
                        <Globe size={14} color="#10b981" />
                    )}
                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text)', wordBreak: 'break-all' }}>
                        {host.domain}
                    </Text>
                    {dnsStatus && <DnsStatusIcon status={dnsStatus.status} dnsResult={dnsStatus} t={t} />}
                </Flex>
                <Badge
                    color={host.host_type === 'redirect' ? 'orange' : host.host_type === 'static' ? 'blue' : host.host_type === 'php' ? 'violet' : 'green'}
                    variant="soft"
                    size="1"
                >
                    {t(`host.${host.host_type}`)}
                </Badge>
            </Flex>

            <Flex align="center" gap="2" mb="3">
                <Badge color={host.tls_enabled ? 'green' : 'gray'} variant="soft" size="1">
                    {host.tls_enabled ? 'HTTPS' : 'HTTP'}
                </Badge>
                <Badge color={host.enabled ? 'green' : 'gray'} variant="soft" size="1">
                    {host.enabled ? t('common.enabled') : t('common.disabled')}
                </Badge>
                {host.group && (
                    <Badge color={host.group.color || 'gray'} variant="soft" size="1">
                        <FolderOpen size={10} /> {host.group.name}
                    </Badge>
                )}
                {host.tags?.map(tag => (
                    <Badge key={tag.id} color={tag.color || 'gray'} variant="outline" size="1">
                        {tag.name}
                    </Badge>
                ))}
            </Flex>

            <Flex justify="between" align="center">
                <Tooltip content={host.enabled ? t('host.click_to_disable') : t('host.click_to_enable')}>
                    <Switch
                        checked={host.enabled}
                        onCheckedChange={() => onToggle(host)}
                        disabled={toggling === host.id}
                        size="1"
                    />
                </Tooltip>
                <Flex gap="2">
                    <Tooltip content={t('clone.tooltip')}>
                        <IconButton variant="soft" size="1" onClick={() => onClone(host)}>
                            <Copy size={14} />
                        </IconButton>
                    </Tooltip>
                    <Tooltip content={t('common.edit')}>
                        <IconButton variant="soft" size="1" onClick={() => onEdit(host)}>
                            <Pencil size={14} />
                        </IconButton>
                    </Tooltip>
                    <Tooltip content={t('common.delete')}>
                        <IconButton variant="soft" color="red" size="1" onClick={() => onDelete(host)}>
                            <Trash2 size={14} />
                        </IconButton>
                    </Tooltip>
                </Flex>
            </Flex>
        </Box>
    )
}

// ============ Host List Page ============
export default function HostList() {
    const { t } = useTranslation()
    const [hosts, setHosts] = useState([])
    const [loading, setLoading] = useState(true)
    const [editHost, setEditHost] = useState(null)
    const [showForm, setShowForm] = useState(false)
    const [deleteHost, setDeleteHost] = useState(null)
    const [cloneHost, setCloneHost] = useState(null)
    const [toggling, setToggling] = useState(null)
    const [dnsStatuses, setDnsStatuses] = useState({})
    const [filterGroupId, setFilterGroupId] = useState('')
    const [filterTagId, setFilterTagId] = useState('')
    const [groups, setGroups] = useState([])
    const [allTags, setAllTags] = useState([])
    const [isMobile, setIsMobile] = useState(() =>
        typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches
    )

    useEffect(() => {
        const mql = window.matchMedia('(max-width: 767px)')
        const handler = (e) => setIsMobile(e.matches)
        mql.addEventListener('change', handler)
        return () => mql.removeEventListener('change', handler)
    }, [])

    const fetchHosts = useCallback(async () => {
        try {
            const params = {}
            if (filterGroupId) params.group_id = filterGroupId
            if (filterTagId) params.tag_id = filterTagId
            const res = await hostAPI.list(params)
            setHosts(res.data.hosts || [])
        } catch (err) {
            console.error('Failed to fetch hosts:', err)
        } finally {
            setLoading(false)
        }
    }, [filterGroupId, filterTagId])

    const fetchGroupsAndTags = useCallback(async () => {
        try {
            const [gRes, tRes] = await Promise.all([groupAPI.list(), tagAPI.list()])
            setGroups(gRes.data.groups || [])
            setAllTags(tRes.data.tags || [])
        } catch { /* ignore */ }
    }, [])

    useEffect(() => {
        fetchHosts()
    }, [fetchHosts])

    useEffect(() => {
        fetchGroupsAndTags()
    }, [fetchGroupsAndTags])

    // Fetch DNS status for all hosts
    useEffect(() => {
        if (!hosts.length) return
        hosts.forEach((host) => {
            if (host.domain && !dnsStatuses[host.domain]) {
                dnsCheckAPI.check(host.domain).then((res) => {
                    setDnsStatuses((prev) => ({ ...prev, [host.domain]: res.data }))
                }).catch(() => {})
            }
        })
    }, [hosts])

    const handleToggle = async (host) => {
        setToggling(host.id)
        try {
            await hostAPI.toggle(host.id)
            fetchHosts()
        } catch (err) {
            console.error('Failed to toggle host:', err)
        } finally {
            setToggling(null)
        }
    }

    const handleDelete = async () => {
        try {
            await hostAPI.delete(deleteHost.id)
            setDeleteHost(null)
            fetchHosts()
        } catch (err) {
            console.error('Failed to delete host:', err)
        }
    }

    const openCreate = () => {
        setEditHost(null)
        setShowForm(true)
    }

    const openEdit = (host) => {
        setEditHost(host)
        setShowForm(true)
    }

    const renderTargetCell = (host) => {
        if (host.host_type === 'redirect') {
            return (
                <Flex align="center" gap="1">
                    <ArrowRightLeft size={12} color="#f59e0b" />
                    <Text size="2" color="gray">{host.redirect_url}</Text>
                </Flex>
            )
        }
        if (host.host_type === 'static') {
            return (
                <Flex align="center" gap="1">
                    <Text size="1" color="gray">📂 {host.root_path || '-'}</Text>
                </Flex>
            )
        }
        if (host.host_type === 'php') {
            return (
                <Flex align="center" gap="1">
                    <Text size="1" color="gray">🐘 {host.root_path || '-'} → {host.php_fastcgi || 'localhost:9000'}</Text>
                </Flex>
            )
        }
        return (
            <Flex direction="column" gap="1">
                {(host.upstreams || []).map((u, i) => (
                    <Flex key={i} align="center" gap="1">
                        <ChevronRight size={12} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="2" color="gray">{u.address}</Text>
                    </Flex>
                ))}
            </Flex>
        )
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="5">
                <Box>
                    <Heading size="6" style={{ color: 'var(--cp-text)' }}>{t('host.title')}</Heading>
                    <Text size="2" color="gray">
                        {t('host.subtitle')}
                    </Text>
                </Box>
                <Button size="2" onClick={openCreate}>
                    <Plus size={16} />
                    {t('host.add_host')}
                </Button>
            </Flex>

            {/* Group & Tag Filters */}
            {(groups.length > 0 || allTags.length > 0) && (
                <Flex gap="3" mb="4" align="end" wrap="wrap" direction={isMobile ? 'column' : 'row'}>
                    {groups.length > 0 && (
                        <Flex direction="column" gap="1" style={isMobile ? { width: '100%' } : {}}>
                            <Text size="1" color="gray">{t('group.filter')}</Text>
                            <Select.Root
                                value={filterGroupId}
                                onValueChange={(v) => { setFilterGroupId(v); setLoading(true) }}
                                size="2"
                            >
                                <Select.Trigger placeholder={t('group.all')} style={isMobile ? { width: '100%' } : { minWidth: 140 }} />
                                <Select.Content>
                                    <Select.Item value="">{t('group.all')}</Select.Item>
                                    {groups.map(g => (
                                        <Select.Item key={g.id} value={String(g.id)}>
                                            <Flex align="center" gap="2">
                                                <Box style={{ width: 8, height: 8, borderRadius: '50%', background: g.color || '#9ca3af' }} />
                                                {g.name}
                                            </Flex>
                                        </Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </Flex>
                    )}
                    {allTags.length > 0 && (
                        <Flex direction="column" gap="1" style={isMobile ? { width: '100%' } : {}}>
                            <Text size="1" color="gray">{t('tag.filter')}</Text>
                            <Flex gap="1" wrap="wrap">
                                {allTags.map(tag => {
                                    const active = filterTagId === String(tag.id)
                                    return (
                                        <Badge
                                            key={tag.id}
                                            size="1"
                                            variant={active ? 'solid' : 'outline'}
                                            color={tag.color || 'gray'}
                                            style={{ cursor: 'pointer', userSelect: 'none' }}
                                            onClick={() => {
                                                const newVal = active ? '' : String(tag.id)
                                                setFilterTagId(newVal)
                                                setLoading(true)
                                            }}
                                        >
                                            <Tags size={10} /> {tag.name}
                                        </Badge>
                                    )
                                })}
                            </Flex>
                        </Flex>
                    )}
                </Flex>
            )}

            {loading ? (
                <Flex justify="center" p="9">
                    <Spinner size="3" />
                </Flex>
            ) : hosts.length === 0 ? (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex direction="column" align="center" gap="3" p="6">
                        <Globe size={48} strokeWidth={1} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="3" color="gray">{t('common.no_data')}</Text>
                        <Button onClick={openCreate}>
                            <Plus size={16} /> {t('host.add_first_host')}
                        </Button>
                    </Flex>
                </Card>
            ) : isMobile ? (
                /* Mobile: Card list view */
                <Box>
                    {hosts.map((host) => (
                        <HostCard
                            key={host.id}
                            host={host}
                            t={t}
                            onEdit={openEdit}
                            onDelete={setDeleteHost}
                            onToggle={handleToggle}
                            onClone={setCloneHost}
                            toggling={toggling}
                            dnsStatus={dnsStatuses[host.domain]}
                        />
                    ))}
                </Box>
            ) : (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', padding: 0 }}>
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('host.domain')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('host.target')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('host.tls')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('common.status')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell style={{ width: 140 }}>{t('common.actions')}</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {hosts.map((host) => (
                                <Table.Row
                                    key={host.id}
                                    style={{ opacity: host.enabled ? 1 : 0.5 }}
                                >
                                    <Table.Cell>
                                        <Flex align="center" gap="2">
                                            {host.host_type === 'redirect' ? (
                                                <ArrowRightLeft size={14} color="#f59e0b" />
                                            ) : host.host_type === 'static' ? (
                                                <Globe size={14} color="#3b82f6" />
                                            ) : host.host_type === 'php' ? (
                                                <Globe size={14} color="#8b5cf6" />
                                            ) : (
                                                <Globe size={14} color="#10b981" />
                                            )}
                                            <Text weight="medium">{host.domain}</Text>
                                            {dnsStatuses[host.domain] && (
                                                <DnsStatusIcon
                                                    status={dnsStatuses[host.domain].status}
                                                    dnsResult={dnsStatuses[host.domain]}
                                                    t={t}
                                                />
                                            )}
                                            {host.basic_auths?.length > 0 && (
                                                <Tooltip content={t('host.auth_protected_tooltip')}>
                                                    <Lock size={12} color="#8b5cf6" />
                                                </Tooltip>
                                            )}
                                            {host.group && (
                                                <Badge color={host.group.color || 'gray'} variant="soft" size="1">
                                                    <FolderOpen size={10} /> {host.group.name}
                                                </Badge>
                                            )}
                                            {host.tags?.map(tag => (
                                                <Badge key={tag.id} color={tag.color || 'gray'} variant="outline" size="1">
                                                    {tag.name}
                                                </Badge>
                                            ))}
                                        </Flex>
                                    </Table.Cell>
                                    <Table.Cell>
                                        {renderTargetCell(host)}
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Badge
                                            color={host.tls_enabled ? 'green' : 'gray'}
                                            variant="soft"
                                            size="1"
                                        >
                                            {host.tls_enabled ? 'HTTPS' : 'HTTP'}
                                        </Badge>
                                        {host.custom_cert_path && (
                                            <Badge color="blue" variant="soft" size="1" ml="1">
                                                {t('host.tls_custom')}
                                            </Badge>
                                        )}
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Tooltip content={host.enabled ? t('host.click_to_disable') : t('host.click_to_enable')}>
                                            <Switch
                                                checked={host.enabled}
                                                onCheckedChange={() => handleToggle(host)}
                                                disabled={toggling === host.id}
                                                size="1"
                                            />
                                        </Tooltip>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Flex gap="2">
                                            <Tooltip content={t('clone.tooltip')}>
                                                <IconButton
                                                    variant="ghost"
                                                    size="1"
                                                    onClick={() => setCloneHost(host)}
                                                >
                                                    <Copy size={14} />
                                                </IconButton>
                                            </Tooltip>
                                            <Tooltip content={t('common.edit')}>
                                                <IconButton
                                                    variant="ghost"
                                                    size="1"
                                                    onClick={() => openEdit(host)}
                                                >
                                                    <Pencil size={14} />
                                                </IconButton>
                                            </Tooltip>
                                            <Tooltip content={t('common.delete')}>
                                                <IconButton
                                                    variant="ghost"
                                                    color="red"
                                                    size="1"
                                                    onClick={() => setDeleteHost(host)}
                                                >
                                                    <Trash2 size={14} />
                                                </IconButton>
                                            </Tooltip>
                                        </Flex>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                        </Table.Body>
                    </Table.Root>
                </Card>
            )}

            {/* Form Dialog */}
            <HostFormDialog
                open={showForm}
                onClose={() => setShowForm(false)}
                host={editHost}
                onSaved={fetchHosts}
            />

            {/* Delete Confirmation */}
            <DeleteDialog
                open={!!deleteHost}
                onClose={() => setDeleteHost(null)}
                host={deleteHost}
                onConfirm={handleDelete}
            />

            {/* Clone Dialog */}
            <CloneDialog
                open={!!cloneHost}
                onClose={() => setCloneHost(null)}
                host={cloneHost}
                onCloned={() => { setDnsStatuses({}); fetchHosts() }}
                t={t}
            />
        </Box>
    )
}
