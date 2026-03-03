import { useState, useEffect, useCallback, useRef } from 'react'
import {
    Box, Flex, Card, Button, IconButton, Text, Heading, Badge,
    Dialog, TextField, Select, Tabs, Table, Callout, Separator,
    Tooltip, Code, TextArea,
} from '@radix-ui/themes'
import {
    ArrowLeft, Play, Square, RotateCw, Trash2, Plus, Copy, Eye,
    EyeOff, Download, Search, Radio, Database, CheckCircle2,
    AlertCircle, Check,
} from 'lucide-react'
import { useParams, useNavigate } from 'react-router'
import { databaseAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

const engineColors = { mysql: 'blue', postgres: 'indigo', mariadb: 'teal', redis: 'red' }
const statusColors = { running: 'green', stopped: 'gray', error: 'red', creating: 'orange' }

const mysqlCharsets = ['utf8mb4', 'utf8', 'latin1']
const pgCharsets = ['UTF8', 'LATIN1']

export default function DatabaseDetail() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const { id } = useParams()

    // Core state
    const [instance, setInstance] = useState(null)
    const [databases, setDatabases] = useState([])
    const [users, setUsers] = useState([])
    const [connectionInfo, setConnectionInfo] = useState(null)
    const [loading, setLoading] = useState(true)

    // Password state
    const [password, setPassword] = useState({ value: '', visible: false, fetched: false })

    // Logs state
    const [logs, setLogs] = useState('')
    const [logFilter, setLogFilter] = useState('')
    const [liveStreaming, setLiveStreaming] = useState(false)
    const logRef = useRef(null)
    const wsRef = useRef(null)

    // Message state
    const [message, setMessage] = useState({ type: '', text: '' })

    // Dialog states
    const [showCreateDb, setShowCreateDb] = useState(false)
    const [showCreateUser, setShowCreateUser] = useState(false)
    const [newDbName, setNewDbName] = useState('')
    const [newDbCharset, setNewDbCharset] = useState('')
    const [newUsername, setNewUsername] = useState('')
    const [newPassword, setNewPassword] = useState('')
    const [grantDatabases, setGrantDatabases] = useState([])
    const [actionLoading, setActionLoading] = useState(false)

    // Active tab
    const [activeTab, setActiveTab] = useState('databases')

    // Query state
    const [queryText, setQueryText] = useState('')
    const [queryDb, setQueryDb] = useState('')
    const [queryResult, setQueryResult] = useState(null)
    const [queryError, setQueryError] = useState('')
    const [queryLoading, setQueryLoading] = useState(false)

    // ---- Data fetching ----

    const fetchInstance = useCallback(async () => {
        try {
            const res = await databaseAPI.getInstance(id)
            setInstance(res.data)
        } catch (e) {
            console.error(e)
        } finally {
            setLoading(false)
        }
    }, [id])

    const fetchDatabases = useCallback(async () => {
        try {
            const res = await databaseAPI.listDatabases(id)
            setDatabases(res.data?.databases || [])
        } catch { /* ignore for redis */ }
    }, [id])

    const fetchUsers = useCallback(async () => {
        try {
            const res = await databaseAPI.listUsers(id)
            setUsers(res.data?.users || [])
        } catch { /* ignore for redis */ }
    }, [id])

    const fetchConnectionInfo = useCallback(async () => {
        try {
            const res = await databaseAPI.connectionInfo(id)
            setConnectionInfo(res.data)
        } catch (e) {
            console.error(e)
        }
    }, [id])

    const fetchPassword = useCallback(async () => {
        if (password.fetched) {
            setPassword(prev => ({ ...prev, visible: !prev.visible }))
            return
        }
        try {
            const res = await databaseAPI.rootPassword(id)
            setPassword({ value: res.data?.password || '', visible: true, fetched: true })
        } catch (e) {
            console.error(e)
        }
    }, [id, password.fetched])

    // Initial load
    useEffect(() => {
        fetchInstance()
        fetchDatabases()
        fetchUsers()
    }, [fetchInstance, fetchDatabases, fetchUsers])

    // Fetch connection info when tab changes
    useEffect(() => {
        if (activeTab === 'connection' && !connectionInfo) {
            fetchConnectionInfo()
        }
    }, [activeTab, connectionInfo, fetchConnectionInfo])

    // Fetch logs when tab changes
    useEffect(() => {
        if (activeTab === 'logs' && !logs && !liveStreaming) {
            fetchStaticLogs()
        }
    }, [activeTab])

    // Auto-scroll logs
    useEffect(() => {
        if (logRef.current) {
            logRef.current.scrollTop = logRef.current.scrollHeight
        }
    }, [logs])

    // Cleanup WebSocket on unmount
    useEffect(() => {
        return () => {
            if (wsRef.current) {
                wsRef.current.close()
                wsRef.current = null
            }
        }
    }, [])

    // ---- Message helper ----

    const showMessage = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage({ type: '', text: '' }), 3000)
    }

    // ---- Instance actions ----

    const handleStart = async () => {
        setActionLoading(true)
        try {
            await databaseAPI.startInstance(id)
            await fetchInstance()
            showMessage('success', t('docker.start'))
        } catch (e) {
            showMessage('error', e.response?.data?.error || t('common.operation_failed'))
        } finally { setActionLoading(false) }
    }

    const handleStop = async () => {
        setActionLoading(true)
        try {
            await databaseAPI.stopInstance(id)
            await fetchInstance()
            showMessage('success', t('docker.stop'))
        } catch (e) {
            showMessage('error', e.response?.data?.error || t('common.operation_failed'))
        } finally { setActionLoading(false) }
    }

    const handleRestart = async () => {
        setActionLoading(true)
        try {
            await databaseAPI.restartInstance(id)
            await fetchInstance()
            showMessage('success', t('docker.restart'))
        } catch (e) {
            showMessage('error', e.response?.data?.error || t('common.operation_failed'))
        } finally { setActionLoading(false) }
    }

    const handleDelete = async () => {
        if (!confirm(t('database.confirm_delete', { name: instance?.name }))) return
        try {
            await databaseAPI.deleteInstance(id)
            navigate('/database')
        } catch (e) {
            showMessage('error', e.response?.data?.error || t('common.operation_failed'))
        }
    }

    // ---- Database actions ----

    const handleCreateDatabase = async () => {
        if (!newDbName.trim()) return
        setActionLoading(true)
        try {
            await databaseAPI.createDatabase(id, {
                name: newDbName.trim(),
                charset: newDbCharset || undefined,
            })
            showMessage('success', t('database.database_created'))
            setShowCreateDb(false)
            setNewDbName('')
            setNewDbCharset('')
            fetchDatabases()
        } catch (e) {
            showMessage('error', e.response?.data?.error || t('common.operation_failed'))
        } finally { setActionLoading(false) }
    }

    const handleDeleteDatabase = async (dbname) => {
        if (!confirm(t('database.confirm_delete_db', { name: dbname }))) return
        try {
            await databaseAPI.deleteDatabase(id, dbname)
            showMessage('success', t('database.database_deleted'))
            fetchDatabases()
        } catch (e) {
            showMessage('error', e.response?.data?.error || t('common.operation_failed'))
        }
    }

    // ---- User actions ----

    const handleCreateUser = async () => {
        if (!newUsername.trim() || !newPassword.trim()) return
        setActionLoading(true)
        try {
            await databaseAPI.createUser(id, {
                username: newUsername.trim(),
                password: newPassword.trim(),
                databases: grantDatabases,
            })
            showMessage('success', t('database.user_created'))
            setShowCreateUser(false)
            setNewUsername('')
            setNewPassword('')
            setGrantDatabases([])
            fetchUsers()
        } catch (e) {
            showMessage('error', e.response?.data?.error || t('common.operation_failed'))
        } finally { setActionLoading(false) }
    }

    const handleDeleteUser = async (username) => {
        if (!confirm(t('database.confirm_delete_user', { name: username }))) return
        try {
            await databaseAPI.deleteUser(id, username)
            showMessage('success', t('database.user_deleted'))
            fetchUsers()
        } catch (e) {
            showMessage('error', e.response?.data?.error || t('common.operation_failed'))
        }
    }

    // ---- Grant databases toggle ----

    const toggleGrantDb = (dbname) => {
        setGrantDatabases(prev =>
            prev.includes(dbname)
                ? prev.filter(d => d !== dbname)
                : [...prev, dbname]
        )
    }

    // ---- Clipboard ----

    const copyToClipboard = (text) => {
        navigator.clipboard.writeText(text)
        showMessage('success', t('common.copied'))
    }

    // ---- Logs ----

    const fetchStaticLogs = async () => {
        try {
            const res = await databaseAPI.instanceLogs(id, 500)
            setLogs(res.data?.logs || res.data?.log || '')
        } catch {
            setLogs('Failed to fetch logs')
        }
    }

    const startLiveStream = () => {
        if (wsRef.current) wsRef.current.close()
        setLiveStreaming(true)
        setLogs('')

        const url = databaseAPI.instanceLogsWsUrl(id)
        const ws = new WebSocket(url)
        wsRef.current = ws
        let buffer = ''

        ws.onmessage = (e) => {
            buffer += e.data + '\n'
            setLogs(buffer)
        }
        ws.onerror = () => {
            setLogs(prev => prev + '\n[WebSocket error]\n')
        }
        ws.onclose = () => {
            setLiveStreaming(false)
        }
    }

    const stopLiveStream = () => {
        if (wsRef.current) {
            wsRef.current.close()
            wsRef.current = null
        }
        setLiveStreaming(false)
    }

    const toggleLiveStream = () => {
        if (liveStreaming) {
            stopLiveStream()
            fetchStaticLogs()
        } else {
            startLiveStream()
        }
    }

    const downloadLogs = () => {
        const blob = new Blob([logs], { type: 'text/plain' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `${instance?.name || 'database'}-logs.txt`
        a.click()
        URL.revokeObjectURL(url)
    }

    const filteredLogs = logFilter
        ? logs.split('\n').filter(line => line.toLowerCase().includes(logFilter.toLowerCase())).join('\n')
        : logs

    // ---- Connection info helpers ----

    // ---- Query execution ----
    const handleExecuteQuery = async () => {
        if (!queryText.trim()) return
        setQueryLoading(true)
        setQueryError('')
        setQueryResult(null)
        try {
            const res = await databaseAPI.executeQuery(id, {
                query: queryText.trim(),
                database: queryDb || undefined,
                limit: 1000,
            })
            setQueryResult({
                columns: res.data.columns || [],
                rows: res.data.rows || [],
                count: res.data.count || 0,
            })
        } catch (e) {
            setQueryError(e.response?.data?.error || e.message)
        } finally {
            setQueryLoading(false)
        }
    }

    // ---- Connection info helpers (continued) ----

    const isRedis = instance?.engine === 'redis'
    const defaultUser = instance?.engine === 'postgres' ? 'postgres' : 'root'
    const defaultPort = connectionInfo?.port || instance?.port || ''
    const host = connectionInfo?.host || 'localhost'

    const buildConnectionUri = () => {
        if (!instance) return ''
        const eng = instance.engine
        const pw = password.visible && password.value ? password.value : '********'
        if (eng === 'redis') return `redis://:${pw}@${host}:${defaultPort}/0`
        if (eng === 'postgres') return `postgresql://${defaultUser}:${pw}@${host}:${defaultPort}/${databases[0]?.name || 'postgres'}`
        return `mysql://${defaultUser}:${pw}@${host}:${defaultPort}/${databases[0]?.name || ''}`
    }

    const buildCliCommand = () => {
        if (!instance) return ''
        const eng = instance.engine
        if (eng === 'redis') return `redis-cli -h ${host} -p ${defaultPort}`
        if (eng === 'postgres') return `psql -h ${host} -p ${defaultPort} -U ${defaultUser}`
        return `mysql -h ${host} -P ${defaultPort} -u ${defaultUser} -p`
    }

    const buildEnvVar = () => {
        if (!instance) return ''
        const eng = instance.engine
        const pw = password.visible && password.value ? password.value : '********'
        if (eng === 'redis') return `REDIS_URL=redis://:${pw}@${host}:${defaultPort}/0`
        if (eng === 'postgres') return `DATABASE_URL=postgresql://${defaultUser}:${pw}@${host}:${defaultPort}/${databases[0]?.name || 'postgres'}`
        return `DATABASE_URL=mysql://${defaultUser}:${pw}@${host}:${defaultPort}/${databases[0]?.name || ''}`
    }

    const buildDockerInternal = () => {
        if (!instance) return ''
        return connectionInfo?.docker_internal || `${instance.name}:${defaultPort}`
    }

    // ---- Charset options based on engine ----

    const getCharsetOptions = () => {
        if (!instance) return []
        if (instance.engine === 'postgres') return pgCharsets
        return mysqlCharsets
    }

    // ---- Render ----

    if (loading) return <Text color="gray">{t('common.loading')}</Text>
    if (!instance) return <Text color="red">{t('common.no_data')}</Text>

    return (
        <Box>
            {/* Message Callout */}
            {message.text && (
                <Callout.Root
                    color={message.type === 'success' ? 'green' : 'red'}
                    mb="3"
                    size="1"
                >
                    <Callout.Icon>
                        {message.type === 'success' ? <CheckCircle2 size={16} /> : <AlertCircle size={16} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            {/* Header */}
            <Flex justify="between" align="start" mb="4" wrap="wrap" gap="3">
                <Flex align="center" gap="2">
                    <Button variant="ghost" size="1" onClick={() => navigate('/database')}>
                        <ArrowLeft size={16} />
                    </Button>
                    <Box>
                        <Flex align="center" gap="2">
                            <Heading size="5">{instance.name}</Heading>
                            <Badge color={engineColors[instance.engine] || 'gray'} variant="soft">
                                {instance.engine}
                            </Badge>
                            {instance.version && (
                                <Badge color="gray" variant="soft">{instance.version}</Badge>
                            )}
                            <Badge color={statusColors[instance.status] || 'gray'} variant="soft">
                                {t(`database.status_${instance.status}`)}
                            </Badge>
                        </Flex>
                    </Box>
                </Flex>
                <Flex gap="2">
                    {instance.status === 'stopped' && (
                        <Button size="2" variant="soft" color="green" onClick={handleStart} disabled={actionLoading}>
                            <Play size={14} /> {t('docker.start')}
                        </Button>
                    )}
                    {instance.status === 'running' && (
                        <Button size="2" variant="soft" color="orange" onClick={handleStop} disabled={actionLoading}>
                            <Square size={14} /> {t('docker.stop')}
                        </Button>
                    )}
                    <Button size="2" variant="soft" onClick={handleRestart} disabled={actionLoading}>
                        <RotateCw size={14} /> {t('docker.restart')}
                    </Button>
                    <Button size="2" variant="soft" color="red" onClick={handleDelete}>
                        <Trash2 size={14} /> {t('common.delete')}
                    </Button>
                </Flex>
            </Flex>

            {/* Tabs */}
            <Tabs.Root value={activeTab} onValueChange={setActiveTab}>
                <Tabs.List>
                    {!isRedis && (
                        <Tabs.Trigger value="databases">{t('database.tab_databases')}</Tabs.Trigger>
                    )}
                    {!isRedis && (
                        <Tabs.Trigger value="users">{t('database.tab_users')}</Tabs.Trigger>
                    )}
                    <Tabs.Trigger value="connection">{t('database.tab_connection')}</Tabs.Trigger>
                    <Tabs.Trigger value="query">{t('database.tab_query')}</Tabs.Trigger>
                    <Tabs.Trigger value="logs">{t('database.tab_logs')}</Tabs.Trigger>
                </Tabs.List>

                {/* Tab 1: Databases */}
                {!isRedis && (
                    <Tabs.Content value="databases">
                        <Card mt="3">
                            <Flex justify="between" align="center" mb="3">
                                <Text size="2" weight="medium">{t('database.tab_databases')}</Text>
                                <Button size="1" onClick={() => {
                                    setNewDbName('')
                                    setNewDbCharset(getCharsetOptions()[0] || '')
                                    setShowCreateDb(true)
                                }}>
                                    <Plus size={14} /> {t('database.create_database')}
                                </Button>
                            </Flex>
                            {databases.length === 0 ? (
                                <Text size="2" color="gray">{t('database.no_databases')}</Text>
                            ) : (
                                <Table.Root>
                                    <Table.Header>
                                        <Table.Row>
                                            <Table.ColumnHeaderCell>{t('common.name')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('database.charset')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('user.created_at')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                                        </Table.Row>
                                    </Table.Header>
                                    <Table.Body>
                                        {databases.map(db => (
                                            <Table.Row key={db.name}>
                                                <Table.Cell>
                                                    <Flex align="center" gap="2">
                                                        <Database size={14} />
                                                        <Text weight="medium">{db.name}</Text>
                                                    </Flex>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2">{db.charset || '-'}</Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2">
                                                        {db.created_at ? new Date(db.created_at).toLocaleString() : '-'}
                                                    </Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Tooltip content={t('common.delete')}>
                                                        <IconButton
                                                            variant="ghost"
                                                            color="red"
                                                            size="1"
                                                            onClick={() => handleDeleteDatabase(db.name)}
                                                        >
                                                            <Trash2 size={14} />
                                                        </IconButton>
                                                    </Tooltip>
                                                </Table.Cell>
                                            </Table.Row>
                                        ))}
                                    </Table.Body>
                                </Table.Root>
                            )}
                        </Card>
                    </Tabs.Content>
                )}

                {/* Tab 2: Users */}
                {!isRedis && (
                    <Tabs.Content value="users">
                        <Card mt="3">
                            <Flex justify="between" align="center" mb="3">
                                <Text size="2" weight="medium">{t('database.tab_users')}</Text>
                                <Button size="1" onClick={() => {
                                    setNewUsername('')
                                    setNewPassword('')
                                    setGrantDatabases([])
                                    setShowCreateUser(true)
                                }}>
                                    <Plus size={14} /> {t('database.create_user')}
                                </Button>
                            </Flex>
                            {users.length === 0 ? (
                                <Text size="2" color="gray">{t('database.no_users')}</Text>
                            ) : (
                                <Table.Root>
                                    <Table.Header>
                                        <Table.Row>
                                            <Table.ColumnHeaderCell>{t('common.username')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('common.host')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('user.created_at')}</Table.ColumnHeaderCell>
                                            <Table.ColumnHeaderCell>{t('common.actions')}</Table.ColumnHeaderCell>
                                        </Table.Row>
                                    </Table.Header>
                                    <Table.Body>
                                        {users.map(u => (
                                            <Table.Row key={u.username}>
                                                <Table.Cell>
                                                    <Text weight="medium">{u.username}</Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2">{u.host || '%'}</Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Text size="2">
                                                        {u.created_at ? new Date(u.created_at).toLocaleString() : '-'}
                                                    </Text>
                                                </Table.Cell>
                                                <Table.Cell>
                                                    <Tooltip content={t('common.delete')}>
                                                        <IconButton
                                                            variant="ghost"
                                                            color="red"
                                                            size="1"
                                                            onClick={() => handleDeleteUser(u.username)}
                                                        >
                                                            <Trash2 size={14} />
                                                        </IconButton>
                                                    </Tooltip>
                                                </Table.Cell>
                                            </Table.Row>
                                        ))}
                                    </Table.Body>
                                </Table.Root>
                            )}
                        </Card>
                    </Tabs.Content>
                )}

                {/* Tab 3: Connection */}
                <Tabs.Content value="connection">
                    <Card mt="3">
                        <Heading size="3" mb="3">{t('database.connection_info')}</Heading>
                        <Flex direction="column" gap="3">
                            {/* Host */}
                            <ConnectionRow
                                label={t('common.host')}
                                value={host}
                                onCopy={() => copyToClipboard(host)}
                                t={t}
                            />

                            <Separator size="4" />

                            {/* Port */}
                            <ConnectionRow
                                label={t('database.port')}
                                value={String(defaultPort)}
                                onCopy={() => copyToClipboard(String(defaultPort))}
                                t={t}
                            />

                            <Separator size="4" />

                            {/* Username */}
                            <ConnectionRow
                                label={t('common.username')}
                                value={defaultUser}
                                onCopy={() => copyToClipboard(defaultUser)}
                                t={t}
                            />

                            <Separator size="4" />

                            {/* Password */}
                            <Flex justify="between" align="center">
                                <Box>
                                    <Text size="1" color="gray" style={{ display: 'block' }}>
                                        {t('common.password')}
                                    </Text>
                                    <Flex align="center" gap="2">
                                        <Text size="2" style={{ fontFamily: 'monospace' }}>
                                            {password.visible && password.value ? password.value : '••••••••'}
                                        </Text>
                                        <Tooltip content={password.visible ? t('common.password') : t('database.click_to_reveal')}>
                                            <IconButton
                                                variant="ghost"
                                                size="1"
                                                onClick={fetchPassword}
                                            >
                                                {password.visible ? <EyeOff size={14} /> : <Eye size={14} />}
                                            </IconButton>
                                        </Tooltip>
                                    </Flex>
                                </Box>
                                {password.visible && password.value && (
                                    <Tooltip content={t('common.copy')}>
                                        <IconButton
                                            variant="ghost"
                                            size="1"
                                            onClick={() => copyToClipboard(password.value)}
                                        >
                                            <Copy size={14} />
                                        </IconButton>
                                    </Tooltip>
                                )}
                            </Flex>

                            <Separator size="4" />

                            {/* Connection URI */}
                            <Flex justify="between" align="center">
                                <Box style={{ flex: 1, minWidth: 0 }}>
                                    <Text size="1" color="gray" style={{ display: 'block' }}>
                                        {t('database.connection_uri')}
                                    </Text>
                                    <Text
                                        size="2"
                                        style={{
                                            fontFamily: 'monospace',
                                            wordBreak: 'break-all',
                                        }}
                                    >
                                        {buildConnectionUri()}
                                    </Text>
                                </Box>
                                <Tooltip content={t('common.copy')}>
                                    <IconButton
                                        variant="ghost"
                                        size="1"
                                        onClick={() => copyToClipboard(buildConnectionUri())}
                                        style={{ flexShrink: 0 }}
                                    >
                                        <Copy size={14} />
                                    </IconButton>
                                </Tooltip>
                            </Flex>

                            <Separator size="4" />

                            {/* CLI Command */}
                            <Flex justify="between" align="center">
                                <Box style={{ flex: 1, minWidth: 0 }}>
                                    <Text size="1" color="gray" style={{ display: 'block' }}>
                                        {t('database.cli_command')}
                                    </Text>
                                    <Code size="2" style={{ wordBreak: 'break-all' }}>
                                        {buildCliCommand()}
                                    </Code>
                                </Box>
                                <Tooltip content={t('common.copy')}>
                                    <IconButton
                                        variant="ghost"
                                        size="1"
                                        onClick={() => copyToClipboard(buildCliCommand())}
                                        style={{ flexShrink: 0 }}
                                    >
                                        <Copy size={14} />
                                    </IconButton>
                                </Tooltip>
                            </Flex>

                            <Separator size="4" />

                            {/* Environment Variable */}
                            <Flex justify="between" align="center">
                                <Box style={{ flex: 1, minWidth: 0 }}>
                                    <Text size="1" color="gray" style={{ display: 'block' }}>
                                        {t('database.env_var')}
                                    </Text>
                                    <Text
                                        size="2"
                                        style={{
                                            fontFamily: 'monospace',
                                            wordBreak: 'break-all',
                                        }}
                                    >
                                        {buildEnvVar()}
                                    </Text>
                                </Box>
                                <Tooltip content={t('common.copy')}>
                                    <IconButton
                                        variant="ghost"
                                        size="1"
                                        onClick={() => copyToClipboard(buildEnvVar())}
                                        style={{ flexShrink: 0 }}
                                    >
                                        <Copy size={14} />
                                    </IconButton>
                                </Tooltip>
                            </Flex>

                            <Separator size="4" />

                            {/* Docker Internal */}
                            <ConnectionRow
                                label={t('database.docker_internal')}
                                value={buildDockerInternal()}
                                onCopy={() => copyToClipboard(buildDockerInternal())}
                                t={t}
                            />
                        </Flex>
                    </Card>
                </Tabs.Content>

                {/* Tab 5: Query */}
                <Tabs.Content value="query">
                    <Card mt="3">
                        {/* Database selector (non-Redis only) */}
                        {!isRedis && databases.length > 0 && (
                            <Box mb="3">
                                <Text size="2" weight="medium" mb="1" style={{ display: 'block' }}>
                                    {t('database.query_select_database')}
                                </Text>
                                <Select.Root value={queryDb} onValueChange={setQueryDb}>
                                    <Select.Trigger style={{ width: '100%' }} placeholder={t('database.query_no_database')} />
                                    <Select.Content>
                                        <Select.Item value="">{t('database.query_no_database')}</Select.Item>
                                        {databases.map(db => (
                                            <Select.Item key={db.name} value={db.name}>{db.name}</Select.Item>
                                        ))}
                                    </Select.Content>
                                </Select.Root>
                            </Box>
                        )}

                        {/* Query TextArea */}
                        <Box mb="3">
                            <TextArea
                                value={queryText}
                                onChange={(e) => setQueryText(e.target.value)}
                                placeholder={t('database.query_placeholder')}
                                rows={4}
                                style={{ fontFamily: "'JetBrains Mono', 'Fira Code', monospace", fontSize: 13 }}
                                onKeyDown={(e) => {
                                    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                                        e.preventDefault()
                                        handleExecuteQuery()
                                    }
                                }}
                            />
                            <Flex mt="2" justify="between" align="center">
                                <Text size="1" color="gray">{t('database.query_hint')}</Text>
                                <Button size="2" onClick={handleExecuteQuery} disabled={queryLoading || !queryText.trim()}>
                                    <Play size={14} />
                                    {queryLoading ? t('database.query_running') : t('database.query_run')}
                                </Button>
                            </Flex>
                        </Box>

                        {/* Query error */}
                        {queryError && (
                            <Callout.Root color="red" size="1" mb="3">
                                <Callout.Icon><AlertCircle size={16} /></Callout.Icon>
                                <Callout.Text>{queryError}</Callout.Text>
                            </Callout.Root>
                        )}

                        {/* Query results */}
                        {queryResult && (
                            <Box>
                                <Badge size="1" variant="soft" mb="2">
                                    {t('database.query_rows_returned', { count: queryResult.count })}
                                </Badge>
                                <Box style={{ overflowX: 'auto', border: '1px solid var(--gray-5)', borderRadius: 6 }}>
                                    <Table.Root variant="surface" size="1">
                                        <Table.Header>
                                            <Table.Row>
                                                {queryResult.columns.map((col) => (
                                                    <Table.ColumnHeaderCell key={col} style={{ whiteSpace: 'nowrap' }}>
                                                        {col}
                                                    </Table.ColumnHeaderCell>
                                                ))}
                                            </Table.Row>
                                        </Table.Header>
                                        <Table.Body>
                                            {queryResult.rows.map((row, ri) => (
                                                <Table.Row key={ri}>
                                                    {queryResult.columns.map((col) => (
                                                        <Table.Cell key={col} style={{ whiteSpace: 'nowrap', maxWidth: 300, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                                                            <Text size="2">
                                                                {row[col] !== null && row[col] !== undefined ? String(row[col]) : <Text color="gray">NULL</Text>}
                                                            </Text>
                                                        </Table.Cell>
                                                    ))}
                                                </Table.Row>
                                            ))}
                                        </Table.Body>
                                    </Table.Root>
                                </Box>
                            </Box>
                        )}
                    </Card>
                </Tabs.Content>

                {/* Tab 4: Logs */}
                <Tabs.Content value="logs">
                    <Card mt="3">
                        <Flex justify="between" align="center" mb="3" wrap="wrap" gap="2">
                            <Flex align="center" gap="2" style={{ flex: 1, minWidth: 200 }}>
                                <TextField.Root
                                    placeholder={t('database.filter_logs')}
                                    value={logFilter}
                                    onChange={e => setLogFilter(e.target.value)}
                                    style={{ flex: 1 }}
                                >
                                    <TextField.Slot>
                                        <Search size={14} />
                                    </TextField.Slot>
                                </TextField.Root>
                            </Flex>
                            <Flex align="center" gap="2">
                                <Button
                                    size="1"
                                    variant={liveStreaming ? 'solid' : 'soft'}
                                    color={liveStreaming ? 'green' : 'gray'}
                                    onClick={toggleLiveStream}
                                >
                                    <Radio size={14} />
                                    {liveStreaming ? t('docker.live') : t('docker.live')}
                                </Button>
                                <Button size="1" variant="soft" onClick={downloadLogs}>
                                    <Download size={14} /> {t('database.download_logs')}
                                </Button>
                                {!liveStreaming && (
                                    <Button size="1" variant="ghost" onClick={fetchStaticLogs}>
                                        <RotateCw size={14} /> {t('common.refresh')}
                                    </Button>
                                )}
                            </Flex>
                        </Flex>
                        <Box
                            ref={logRef}
                            style={{
                                background: 'var(--gray-2)',
                                borderRadius: 8,
                                padding: 12,
                                maxHeight: 500,
                                overflow: 'auto',
                                fontFamily: 'monospace',
                                fontSize: '0.8rem',
                                whiteSpace: 'pre-wrap',
                                lineHeight: 1.5,
                            }}
                        >
                            {filteredLogs || t('docker.no_logs')}
                        </Box>
                    </Card>
                </Tabs.Content>
            </Tabs.Root>

            {/* Create Database Dialog */}
            <Dialog.Root open={showCreateDb} onOpenChange={(v) => { if (!v) setShowCreateDb(false) }}>
                <Dialog.Content maxWidth="450px">
                    <Dialog.Title>{t('database.create_database')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        <Box>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                {t('database.database_name')}
                            </Text>
                            <TextField.Root
                                placeholder={t('database.database_name_placeholder')}
                                value={newDbName}
                                onChange={e => setNewDbName(e.target.value)}
                            />
                        </Box>
                        <Box>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                {t('database.charset')}
                            </Text>
                            <Select.Root value={newDbCharset} onValueChange={setNewDbCharset}>
                                <Select.Trigger style={{ width: '100%' }} />
                                <Select.Content>
                                    {getCharsetOptions().map(cs => (
                                        <Select.Item key={cs} value={cs}>{cs}</Select.Item>
                                    ))}
                                </Select.Content>
                            </Select.Root>
                        </Box>
                    </Flex>
                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button
                            disabled={actionLoading || !newDbName.trim()}
                            onClick={handleCreateDatabase}
                        >
                            {actionLoading ? t('common.saving') : t('common.create')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Create User Dialog */}
            <Dialog.Root open={showCreateUser} onOpenChange={(v) => { if (!v) setShowCreateUser(false) }}>
                <Dialog.Content maxWidth="450px">
                    <Dialog.Title>{t('database.create_user')}</Dialog.Title>
                    <Flex direction="column" gap="3" mt="3">
                        <Box>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                {t('common.username')}
                            </Text>
                            <TextField.Root
                                placeholder={t('common.username')}
                                value={newUsername}
                                onChange={e => setNewUsername(e.target.value)}
                            />
                        </Box>
                        <Box>
                            <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                {t('common.password')}
                            </Text>
                            <TextField.Root
                                type="password"
                                placeholder={t('common.password')}
                                value={newPassword}
                                onChange={e => setNewPassword(e.target.value)}
                            />
                        </Box>
                        {databases.length > 0 && (
                            <Box>
                                <Text size="2" weight="bold" mb="1" style={{ display: 'block' }}>
                                    {t('database.grant_databases')}
                                </Text>
                                <Flex direction="column" gap="1">
                                    {databases.map(db => (
                                        <Flex
                                            key={db.name}
                                            align="center"
                                            gap="2"
                                            style={{ cursor: 'pointer' }}
                                            onClick={() => toggleGrantDb(db.name)}
                                        >
                                            <Box
                                                style={{
                                                    width: 18,
                                                    height: 18,
                                                    borderRadius: 4,
                                                    border: '1px solid var(--gray-7)',
                                                    display: 'flex',
                                                    alignItems: 'center',
                                                    justifyContent: 'center',
                                                    background: grantDatabases.includes(db.name)
                                                        ? 'var(--accent-9)'
                                                        : 'transparent',
                                                }}
                                            >
                                                {grantDatabases.includes(db.name) && (
                                                    <Check size={12} color="white" />
                                                )}
                                            </Box>
                                            <Text size="2">{db.name}</Text>
                                        </Flex>
                                    ))}
                                </Flex>
                            </Box>
                        )}
                    </Flex>
                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close>
                            <Button variant="soft" color="gray">{t('common.cancel')}</Button>
                        </Dialog.Close>
                        <Button
                            disabled={actionLoading || !newUsername.trim() || !newPassword.trim()}
                            onClick={handleCreateUser}
                        >
                            {actionLoading ? t('common.saving') : t('common.create')}
                        </Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}

// ---- Helper component for connection info rows ----

function ConnectionRow({ label, value, onCopy, t }) {
    return (
        <Flex justify="between" align="center">
            <Box>
                <Text size="1" color="gray" style={{ display: 'block' }}>
                    {label}
                </Text>
                <Text size="2" style={{ fontFamily: 'monospace' }}>
                    {value || '-'}
                </Text>
            </Box>
            {value && (
                <Tooltip content={t('common.copy')}>
                    <IconButton variant="ghost" size="1" onClick={onCopy}>
                        <Copy size={14} />
                    </IconButton>
                </Tooltip>
            )}
        </Flex>
    )
}
