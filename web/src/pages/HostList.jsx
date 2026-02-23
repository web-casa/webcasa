import { useState, useEffect, useCallback } from 'react'
import {
    Box, Flex, Heading, Text, Button, Badge, Switch, Table, Dialog,
    TextField, Callout, IconButton, Card, Tooltip, Spinner, AlertDialog,
    Select, Tabs, Separator,
} from '@radix-ui/themes'
import {
    Plus, Pencil, Trash2, Globe, AlertCircle, X, ChevronRight,
    ArrowRightLeft, Shield, Lock,
} from 'lucide-react'
import { hostAPI, dnsProviderAPI, settingAPI, certificateAPI } from '../api/index.js'
import { useRef as useReactRef } from 'react'

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
}

// ============ Host Form Dialog ============
function HostFormDialog({ open, onClose, onSaved, host }) {
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
    const isEdit = !!host

    useEffect(() => {
        dnsProviderAPI.list().then(res => setDnsProviders(res.data.providers || [])).catch(() => { })
        settingAPI.getAll().then(res => {
            const s = res.data.settings || {}
            setServerIPs({ ipv4: s.server_ipv4 || '', ipv6: s.server_ipv6 || '' })
        }).catch(() => { })
        certificateAPI.list().then(res => setCertificates(res.data.certificates || [])).catch(() => { })
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
            })
        } else {
            setForm({ ...DEFAULT_FORM })
        }
        setError('')
    }, [host, open])

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
            setError(err.response?.data?.error || '证书上传失败')
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
            }
            if (isEdit) {
                await hostAPI.update(host.id, payload)
            } else {
                await hostAPI.create(payload)
            }
            onSaved()
            onClose()
        } catch (err) {
            setError(err.response?.data?.error || 'Failed to save host')
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
                    {isEdit ? 'Edit Host' : 'New Host'}
                </Dialog.Title>
                <Dialog.Description size="2" color="gray" mb="4">
                    {isProxy ? '配置反向代理' : isRedirect ? '配置域名跳转' : isStatic ? '配置静态网站托管' : '配置 PHP 站点'}
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
                            <Text size="2" weight="medium">Type</Text>
                            <Select.Root
                                value={form.host_type}
                                onValueChange={(v) => setForm({ ...form, host_type: v })}
                                size="2"
                            >
                                <Select.Trigger />
                                <Select.Content>
                                    <Select.Item value="proxy">反向代理</Select.Item>
                                    <Select.Item value="redirect">域名跳转</Select.Item>
                                    <Select.Item value="static">静态网站</Select.Item>
                                    <Select.Item value="php">PHP 站点</Select.Item>
                                </Select.Content>
                            </Select.Root>
                        </Flex>
                        <Flex direction="column" gap="1" style={{ flex: 1 }}>
                            <Text size="2" weight="medium">Domain</Text>
                            <TextField.Root
                                placeholder="example.com"
                                value={form.domain}
                                onChange={(e) => setForm({ ...form, domain: e.target.value })}
                                size="2"
                            />
                        </Flex>
                    </Flex>

                    <Tabs.Root defaultValue="main">
                        <Tabs.List>
                            <Tabs.Trigger value="main">
                                {isProxy ? 'Upstream' : isRedirect ? 'Redirect' : '配置'}
                            </Tabs.Trigger>
                            <Tabs.Trigger value="options">Options</Tabs.Trigger>
                            <Tabs.Trigger value="auth">
                                <Lock size={12} style={{ marginRight: 4 }} />
                                Auth
                            </Tabs.Trigger>
                        </Tabs.List>

                        {/* Tab 1: Main config */}
                        <Tabs.Content value="main">
                            <Box pt="3">
                                {isProxy ? (
                                    /* Proxy: Upstreams */
                                    <Flex direction="column" gap="2">
                                        <Flex justify="between" align="center">
                                            <Text size="2" weight="medium">Upstream Servers</Text>
                                            <Button variant="ghost" size="1" onClick={addUpstream}>
                                                <Plus size={14} /> Add
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
                                            <Text size="2" weight="medium">Redirect Target</Text>
                                            <TextField.Root
                                                placeholder="https://new-site.com"
                                                value={form.redirect_url}
                                                onChange={(e) => setForm({ ...form, redirect_url: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">
                                                The original URI path will be appended automatically
                                            </Text>
                                        </Flex>
                                        <Flex direction="column" gap="1" style={{ width: 200 }}>
                                            <Text size="2" weight="medium">Redirect Code</Text>
                                            <Select.Root
                                                value={String(form.redirect_code)}
                                                onValueChange={(v) => setForm({ ...form, redirect_code: Number(v) })}
                                                size="2"
                                            >
                                                <Select.Trigger />
                                                <Select.Content>
                                                    <Select.Item value="301">301 — Permanent</Select.Item>
                                                    <Select.Item value="302">302 — Temporary</Select.Item>
                                                </Select.Content>
                                            </Select.Root>
                                        </Flex>
                                    </Flex>
                                ) : (
                                    /* Static / PHP: Root path + options */
                                    <Flex direction="column" gap="3">
                                        <Flex direction="column" gap="1">
                                            <Text size="2" weight="medium">根目录</Text>
                                            <TextField.Root
                                                placeholder="/var/www/my-site"
                                                value={form.root_path || ''}
                                                onChange={(e) => setForm({ ...form, root_path: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">
                                                {isStatic ? '静态文件所在的服务器目录' : 'PHP 站点根目录（如 WordPress）'}
                                            </Text>
                                        </Flex>
                                        {isPHP && (
                                            <Flex direction="column" gap="1">
                                                <Text size="2" weight="medium">PHP-FPM 地址</Text>
                                                <TextField.Root
                                                    placeholder="localhost:9000"
                                                    value={form.php_fastcgi || ''}
                                                    onChange={(e) => setForm({ ...form, php_fastcgi: e.target.value })}
                                                    size="2"
                                                />
                                                <Text size="1" color="gray">
                                                    PHP-FPM 监听地址，默认 localhost:9000
                                                </Text>
                                            </Flex>
                                        )}
                                        {isStatic && (
                                            <Flex justify="between" align="center">
                                                <Flex direction="column">
                                                    <Text size="2" weight="medium">目录浏览</Text>
                                                    <Text size="1" color="gray">允许查看目录列表</Text>
                                                </Flex>
                                                <Switch
                                                    checked={form.directory_browse || false}
                                                    onCheckedChange={(v) => setForm({ ...form, directory_browse: v })}
                                                />
                                            </Flex>
                                        )}
                                        <Flex direction="column" gap="1">
                                            <Text size="2" weight="medium">首页文件</Text>
                                            <TextField.Root
                                                placeholder="index.html index.htm"
                                                value={form.index_files || ''}
                                                onChange={(e) => setForm({ ...form, index_files: e.target.value })}
                                                size="2"
                                            />
                                            <Text size="1" color="gray">空格分隔，留空使用默认</Text>
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
                                            <Text size="2" weight="medium">TLS 模式</Text>
                                            <Text size="1" color="gray">选择证书获取方式</Text>
                                        </Flex>
                                        <Select.Root
                                            value={form.tls_mode || 'auto'}
                                            onValueChange={(v) => setForm({ ...form, tls_mode: v, tls_enabled: v !== 'off' })}
                                            size="2"
                                        >
                                            <Select.Trigger style={{ width: 160 }} />
                                            <Select.Content>
                                                <Select.Item value="auto">自动 (Let's Encrypt)</Select.Item>
                                                <Select.Item value="dns">DNS Challenge</Select.Item>
                                                <Select.Item value="wildcard">通配符证书</Select.Item>
                                                <Select.Item value="custom">自定义证书</Select.Item>
                                                <Select.Item value="off">关闭 TLS</Select.Item>
                                            </Select.Content>
                                        </Select.Root>
                                    </Flex>

                                    {form.tls_mode === 'auto' && (serverIPs.ipv4 || serverIPs.ipv6) && form.domain && (
                                        <Callout.Root color="blue" size="1">
                                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                                            <Callout.Text>
                                                <Text size="1">请先添加 DNS 解析再保存：</Text>
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
                                            <Text size="1" color="gray">DNS Provider</Text>
                                            <Select.Root
                                                value={form.dns_provider_id ? String(form.dns_provider_id) : ''}
                                                onValueChange={(v) => setForm({ ...form, dns_provider_id: v ? Number(v) : null })}
                                                size="2"
                                            >
                                                <Select.Trigger placeholder="选择 DNS Provider" />
                                                <Select.Content>
                                                    {dnsProviders.map(p => (
                                                        <Select.Item key={p.id} value={String(p.id)}>
                                                            {p.name} ({p.provider})
                                                        </Select.Item>
                                                    ))}
                                                </Select.Content>
                                            </Select.Root>
                                            {dnsProviders.length === 0 && (
                                                <Text size="1" color="red">请先在 DNS Providers 页面添加 Provider</Text>
                                            )}
                                        </Flex>
                                    )}

                                    {form.tls_mode === 'custom' && (
                                        <Flex direction="column" gap="2" pl="4" style={{ borderLeft: '2px solid var(--cp-border-subtle)' }}>
                                            <Text size="1" color="gray">选择证书</Text>
                                            <Select.Root
                                                value={form.certificate_id ? String(form.certificate_id) : ''}
                                                onValueChange={(v) => setForm({ ...form, certificate_id: v ? Number(v) : null })}
                                                size="2"
                                            >
                                                <Select.Trigger placeholder="从已上传的证书中选择" />
                                                <Select.Content>
                                                    {certificates.map(c => (
                                                        <Select.Item key={c.id} value={String(c.id)}>
                                                            {c.name} — {c.domains || '未知域名'}
                                                        </Select.Item>
                                                    ))}
                                                </Select.Content>
                                            </Select.Root>
                                            {certificates.length === 0 && (
                                                <Text size="1" color="orange">
                                                    暂无证书，请先到 <strong>证书管理</strong> 页面上传
                                                </Text>
                                            )}

                                            <Separator size="4" />

                                            <Text size="1" color="gray">或直接上传证书</Text>
                                            <Flex gap="2">
                                                <Button
                                                    variant="soft" color="gray" size="1"
                                                    onClick={() => certFileRef.current?.click()}
                                                >
                                                    {certFile ? certFile.name : '选择 .pem/.crt'}
                                                </Button>
                                                <Button
                                                    variant="soft" color="gray" size="1"
                                                    onClick={() => keyFileRef.current?.click()}
                                                >
                                                    {keyFile ? keyFile.name : '选择 .key'}
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
                                                    {uploadingCert ? '上传中...' : '上传并关联'}
                                                </Button>
                                            )}
                                        </Flex>
                                    )}

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">HTTP → HTTPS Redirect</Text>
                                            <Text size="1" color="gray">Force redirect HTTP to HTTPS</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.http_redirect}
                                            onCheckedChange={(v) => setForm({ ...form, http_redirect: v })}
                                        />
                                    </Flex>

                                    {isProxy && (
                                        <Flex justify="between" align="center">
                                            <Flex direction="column">
                                                <Text size="2" weight="medium">WebSocket Support</Text>
                                                <Text size="1" color="gray">Enable WebSocket proxy</Text>
                                            </Flex>
                                            <Switch
                                                checked={form.websocket}
                                                onCheckedChange={(v) => setForm({ ...form, websocket: v })}
                                            />
                                        </Flex>
                                    )}

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>性能</Text>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">响应压缩</Text>
                                            <Text size="1" color="gray">启用 gzip + zstd 压缩</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.compression}
                                            onCheckedChange={(v) => setForm({ ...form, compression: v })}
                                        />
                                    </Flex>

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>安全</Text>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">安全响应头</Text>
                                            <Text size="1" color="gray">HSTS / X-Frame-Options / CSP 等</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.security_headers}
                                            onCheckedChange={(v) => setForm({ ...form, security_headers: v })}
                                        />
                                    </Flex>

                                    <Flex justify="between" align="center">
                                        <Flex direction="column">
                                            <Text size="2" weight="medium">CORS 跨域</Text>
                                            <Text size="1" color="gray">允许跨域请求</Text>
                                        </Flex>
                                        <Switch
                                            checked={form.cors_enabled}
                                            onCheckedChange={(v) => setForm({ ...form, cors_enabled: v })}
                                        />
                                    </Flex>

                                    {form.cors_enabled && (
                                        <Flex direction="column" gap="2" pl="4" style={{ borderLeft: '2px solid var(--cp-border-subtle)' }}>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">允许的源（逗号分隔）</Text>
                                                <TextField.Root
                                                    value={form.cors_origins}
                                                    onChange={(e) => setForm({ ...form, cors_origins: e.target.value })}
                                                    placeholder="* 或 https://example.com"
                                                />
                                            </Box>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">允许的方法</Text>
                                                <TextField.Root
                                                    value={form.cors_methods}
                                                    onChange={(e) => setForm({ ...form, cors_methods: e.target.value })}
                                                    placeholder="GET, POST, PUT, DELETE, OPTIONS"
                                                />
                                            </Box>
                                            <Box>
                                                <Text size="1" color="gray" mb="1">允许的请求头</Text>
                                                <TextField.Root
                                                    value={form.cors_headers}
                                                    onChange={(e) => setForm({ ...form, cors_headers: e.target.value })}
                                                    placeholder="Content-Type, Authorization"
                                                />
                                            </Box>
                                        </Flex>
                                    )}

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>错误页</Text>

                                    <Box>
                                        <Text size="2" weight="medium" mb="1">自定义错误页目录</Text>
                                        <Text size="1" color="gray" mb="2" as="p">
                                            放置 404.html / 502.html / 503.html 的目录路径
                                        </Text>
                                        <TextField.Root
                                            value={form.error_page_path}
                                            onChange={(e) => setForm({ ...form, error_page_path: e.target.value })}
                                            placeholder="/var/lib/caddypanel/error_pages"
                                        />
                                    </Box>

                                    <Separator size="4" style={{ opacity: 0.15 }} />
                                    <Text size="2" weight="bold" style={{ color: 'var(--cp-text-secondary)' }}>高级</Text>

                                    <Box>
                                        <Text size="2" weight="medium" mb="1">自定义 Caddy 指令</Text>
                                        <Text size="1" color="gray" mb="2" as="p">
                                            直接写入 Caddy 配置，如 rate_limit、encode 等
                                        </Text>
                                        <textarea
                                            value={form.custom_directives}
                                            onChange={(e) => setForm({ ...form, custom_directives: e.target.value })}
                                            placeholder={'encode gzip zstd\nrate_limit {remote.ip} 10r/s'}
                                            rows={4}
                                            style={{
                                                width: '100%',
                                                background: '#09090b',
                                                border: '1px solid var(--cp-border-subtle)',
                                                borderRadius: 6,
                                                padding: '8px 12px',
                                                color: '#e4e4e7',
                                                fontFamily: 'monospace',
                                                fontSize: '0.8rem',
                                                resize: 'vertical',
                                            }}
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
                                            <Text size="2" weight="medium">HTTP Basic Auth</Text>
                                            <Text size="1" color="gray">Protect this host with username/password</Text>
                                        </Flex>
                                        <Button variant="ghost" size="1" onClick={addBasicAuth}>
                                            <Plus size={14} /> Add User
                                        </Button>
                                    </Flex>
                                    {form.basic_auths.length === 0 && (
                                        <Text size="2" color="gray" style={{ fontStyle: 'italic' }}>
                                            No credentials set — host is publicly accessible
                                        </Text>
                                    )}
                                    {form.basic_auths.map((auth, i) => (
                                        <Flex key={i} gap="2" align="center">
                                            <TextField.Root
                                                style={{ flex: 1 }}
                                                placeholder="Username"
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
                                                placeholder="Password"
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
                                                {host.basic_auths.length} existing credential(s). Add new ones to replace, or leave empty to keep current.
                                            </Callout.Text>
                                        </Callout.Root>
                                    )}
                                </Flex>
                            </Box>
                        </Tabs.Content>
                    </Tabs.Root>
                </Flex>

                <Flex gap="3" mt="5" justify="end">
                    <Dialog.Close>
                        <Button variant="soft" color="gray">Cancel</Button>
                    </Dialog.Close>
                    <Button
                        onClick={handleSave}
                        disabled={saving || !form.domain || (isProxy && !form.upstreams.some(u => u.address)) || (!isProxy && !form.redirect_url)}
                    >
                        {saving ? <Spinner size="1" /> : null}
                        {isEdit ? 'Save Changes' : 'Create Host'}
                    </Button>
                </Flex>
            </Dialog.Content>
        </Dialog.Root>
    )
}

// ============ Delete Confirmation ============
function DeleteDialog({ open, onClose, host, onConfirm }) {
    const [deleting, setDeleting] = useState(false)
    const handleDelete = async () => {
        setDeleting(true)
        await onConfirm()
        setDeleting(false)
    }
    return (
        <AlertDialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
            <AlertDialog.Content maxWidth="400px" style={{ background: 'var(--cp-card)' }}>
                <AlertDialog.Title>Delete Host</AlertDialog.Title>
                <AlertDialog.Description size="2">
                    Are you sure you want to delete <strong>{host?.domain}</strong>? This action cannot be
                    undone and the Caddyfile will be updated immediately.
                </AlertDialog.Description>
                <Flex gap="3" mt="4" justify="end">
                    <AlertDialog.Cancel>
                        <Button variant="soft" color="gray">Cancel</Button>
                    </AlertDialog.Cancel>
                    <AlertDialog.Action>
                        <Button color="red" onClick={handleDelete} disabled={deleting}>
                            {deleting ? <Spinner size="1" /> : <Trash2 size={14} />}
                            Delete
                        </Button>
                    </AlertDialog.Action>
                </Flex>
            </AlertDialog.Content>
        </AlertDialog.Root>
    )
}

// ============ Host List Page ============
export default function HostList() {
    const [hosts, setHosts] = useState([])
    const [loading, setLoading] = useState(true)
    const [editHost, setEditHost] = useState(null)
    const [showForm, setShowForm] = useState(false)
    const [deleteHost, setDeleteHost] = useState(null)
    const [toggling, setToggling] = useState(null)

    const fetchHosts = useCallback(async () => {
        try {
            const res = await hostAPI.list()
            setHosts(res.data.hosts || [])
        } catch (err) {
            console.error('Failed to fetch hosts:', err)
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => {
        fetchHosts()
    }, [fetchHosts])

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
                        <ChevronRight size={12} color="#52525b" />
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
                    <Heading size="6" style={{ color: 'var(--cp-text)' }}>Proxy Hosts</Heading>
                    <Text size="2" color="gray">
                        Manage your reverse proxy and redirect configurations
                    </Text>
                </Box>
                <Button size="2" onClick={openCreate}>
                    <Plus size={16} />
                    Add Host
                </Button>
            </Flex>

            {loading ? (
                <Flex justify="center" p="9">
                    <Spinner size="3" />
                </Flex>
            ) : hosts.length === 0 ? (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                    <Flex direction="column" align="center" gap="3" p="6">
                        <Globe size={48} strokeWidth={1} color="#3f3f46" />
                        <Text size="3" color="gray">No proxy hosts configured</Text>
                        <Button onClick={openCreate}>
                            <Plus size={16} /> Add Your First Host
                        </Button>
                    </Flex>
                </Card>
            ) : (
                <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)', padding: 0 }}>
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>Domain</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>Target</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>TLS</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>Status</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell style={{ width: 120 }}>Actions</Table.ColumnHeaderCell>
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
                                            {host.basic_auths?.length > 0 && (
                                                <Tooltip content="Protected by Basic Auth">
                                                    <Lock size={12} color="#8b5cf6" />
                                                </Tooltip>
                                            )}
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
                                                Custom
                                            </Badge>
                                        )}
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Tooltip content={host.enabled ? 'Click to disable' : 'Click to enable'}>
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
                                            <Tooltip content="Edit">
                                                <IconButton
                                                    variant="ghost"
                                                    size="1"
                                                    onClick={() => openEdit(host)}
                                                >
                                                    <Pencil size={14} />
                                                </IconButton>
                                            </Tooltip>
                                            <Tooltip content="Delete">
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
        </Box>
    )
}
