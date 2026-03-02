import { useState, useEffect } from 'react'
import {
    Box, Flex, Card, Button, Text, Heading, TextArea, Table,
    Select, Badge, Callout, Separator,
} from '@radix-ui/themes'
import { Database, Play, AlertCircle, Terminal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { databaseAPI } from '../api/index.js'

const engineColors = { mysql: 'blue', postgres: 'indigo', mariadb: 'teal', redis: 'red' }

export default function DatabaseQuery() {
    const { t } = useTranslation()

    // Instance/database selection
    const [instances, setInstances] = useState([])
    const [selectedInstance, setSelectedInstance] = useState('')
    const [databases, setDatabases] = useState([])
    const [selectedDb, setSelectedDb] = useState('')
    const [loading, setLoading] = useState(true)

    // Query state
    const [query, setQuery] = useState('')
    const [result, setResult] = useState(null)
    const [error, setError] = useState('')
    const [queryLoading, setQueryLoading] = useState(false)

    // Load instances
    useEffect(() => {
        databaseAPI.listInstances()
            .then(res => setInstances(res.data?.instances || []))
            .catch(() => {})
            .finally(() => setLoading(false))
    }, [])

    // Load databases when instance changes
    useEffect(() => {
        if (!selectedInstance) {
            setDatabases([])
            setSelectedDb('')
            return
        }
        const inst = instances.find(i => String(i.id) === selectedInstance)
        if (inst?.engine === 'redis') {
            setDatabases([])
            setSelectedDb('')
            return
        }
        databaseAPI.listDatabases(selectedInstance)
            .then(res => setDatabases(res.data?.databases || []))
            .catch(() => setDatabases([]))
    }, [selectedInstance, instances])

    const selectedInst = instances.find(i => String(i.id) === selectedInstance)
    const isRedis = selectedInst?.engine === 'redis'

    const handleRun = async () => {
        if (!selectedInstance || !query.trim()) return
        setQueryLoading(true)
        setError('')
        setResult(null)
        try {
            const res = await databaseAPI.executeQuery(selectedInstance, {
                query: query.trim(),
                database: selectedDb || undefined,
                limit: 1000,
            })
            setResult({
                columns: res.data.columns || [],
                rows: res.data.rows || [],
                count: res.data.count || 0,
            })
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        } finally {
            setQueryLoading(false)
        }
    }

    const runningInstances = instances.filter(i => i.status === 'running')

    return (
        <Box>
            {/* Header */}
            <Flex align="center" gap="3" mb="4">
                <Terminal size={20} style={{ color: 'var(--accent-9)' }} />
                <Box>
                    <Heading size="5">{t('database.query_console')}</Heading>
                    <Text size="2" color="gray" as="p">
                        {t('database.query_console_subtitle')}
                    </Text>
                </Box>
            </Flex>

            {/* Instance + Database selectors */}
            <Card mb="4">
                <Flex gap="3" wrap="wrap">
                    <Box style={{ flex: 1, minWidth: 200 }}>
                        <Text size="2" weight="medium" mb="1" style={{ display: 'block' }}>
                            {t('database.query_select_instance')}
                        </Text>
                        <Select.Root value={selectedInstance} onValueChange={setSelectedInstance}>
                            <Select.Trigger style={{ width: '100%' }} placeholder={t('database.query_select_instance')} />
                            <Select.Content>
                                {runningInstances.map(inst => (
                                    <Select.Item key={inst.id} value={String(inst.id)}>
                                        <Flex align="center" gap="2">
                                            {inst.name}
                                            <Badge color={engineColors[inst.engine] || 'gray'} variant="soft" size="1">
                                                {inst.engine}
                                            </Badge>
                                            <Text size="1" color="gray">v{inst.version}</Text>
                                        </Flex>
                                    </Select.Item>
                                ))}
                            </Select.Content>
                        </Select.Root>
                    </Box>
                    {selectedInstance && !isRedis && (
                        <Box style={{ flex: 1, minWidth: 200 }}>
                            <Text size="2" weight="medium" mb="1" style={{ display: 'block' }}>
                                {t('database.query_select_database')}
                            </Text>
                            <Select.Root value={selectedDb} onValueChange={setSelectedDb}>
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
                </Flex>
            </Card>

            {/* Query editor */}
            {selectedInstance ? (
                <Card mb="4">
                    <Box mb="3">
                        <TextArea
                            value={query}
                            onChange={(e) => setQuery(e.target.value)}
                            placeholder={t('database.query_placeholder')}
                            rows={6}
                            style={{ fontFamily: "'JetBrains Mono', 'Fira Code', monospace", fontSize: 13 }}
                            onKeyDown={(e) => {
                                if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                                    e.preventDefault()
                                    handleRun()
                                }
                            }}
                        />
                        <Flex mt="2" justify="between" align="center">
                            <Text size="1" color="gray">{t('database.query_hint')}</Text>
                            <Button
                                size="2"
                                onClick={handleRun}
                                disabled={queryLoading || !query.trim() || !selectedInstance}
                            >
                                <Play size={14} />
                                {queryLoading ? t('database.query_running') : t('database.query_run')}
                            </Button>
                        </Flex>
                    </Box>

                    {/* Error */}
                    {error && (
                        <Callout.Root color="red" size="1" mb="3">
                            <Callout.Icon><AlertCircle size={16} /></Callout.Icon>
                            <Callout.Text>{error}</Callout.Text>
                        </Callout.Root>
                    )}

                    {/* Results table */}
                    {result && (
                        <Box>
                            <Badge size="1" variant="soft" mb="2">
                                {t('database.query_rows_returned', { count: result.count })}
                            </Badge>
                            <Box style={{ overflowX: 'auto', border: '1px solid var(--gray-5)', borderRadius: 6 }}>
                                <Table.Root variant="surface" size="1">
                                    <Table.Header>
                                        <Table.Row>
                                            {result.columns.map((col) => (
                                                <Table.ColumnHeaderCell key={col} style={{ whiteSpace: 'nowrap' }}>
                                                    {col}
                                                </Table.ColumnHeaderCell>
                                            ))}
                                        </Table.Row>
                                    </Table.Header>
                                    <Table.Body>
                                        {result.rows.map((row, ri) => (
                                            <Table.Row key={ri}>
                                                {result.columns.map((col) => (
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
            ) : (
                /* Empty state */
                !loading && (
                    <Card style={{ padding: 40, textAlign: 'center' }}>
                        <Database size={48} style={{ margin: '0 auto 12px', opacity: 0.3 }} />
                        <Text size="3" color="gray">{t('database.query_instance_required')}</Text>
                    </Card>
                )
            )}
        </Box>
    )
}
