import { useState } from 'react'
import { Box, Flex, Card, Button, Text, Heading, TextField, TextArea, Table, Badge, Callout, Separator, Code } from '@radix-ui/themes'
import { Database, FolderOpen, Play, Table2, AlertCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { databaseAPI } from '../api/index.js'

export default function SQLiteBrowser() {
    const { t } = useTranslation()

    const [filePath, setFilePath] = useState('')
    const [tables, setTables] = useState([])
    const [selectedTable, setSelectedTable] = useState('')
    const [schema, setSchema] = useState([])
    const [query, setQuery] = useState('')
    const [result, setResult] = useState(null)
    const [error, setError] = useState('')
    const [loading, setLoading] = useState(false)

    const handleOpen = async () => {
        if (!filePath.trim()) return
        setLoading(true)
        setError('')
        setTables([])
        setSelectedTable('')
        setSchema([])
        setQuery('')
        setResult(null)
        try {
            const res = await databaseAPI.sqliteTables(filePath.trim())
            setTables(res.data.tables || [])
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        } finally {
            setLoading(false)
        }
    }

    const handleSelectTable = async (table) => {
        setSelectedTable(table)
        setError('')
        try {
            const res = await databaseAPI.sqliteSchema(filePath.trim(), table)
            setSchema(res.data.schema || [])
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        }
        const q = `SELECT * FROM ${table} LIMIT 100`
        setQuery(q)
        runQuery(q)
    }

    const runQuery = async (q) => {
        const queryStr = q || query
        if (!queryStr.trim()) return
        setLoading(true)
        setError('')
        setResult(null)
        try {
            const res = await databaseAPI.sqliteQuery(filePath.trim(), queryStr.trim(), 1000)
            setResult({
                columns: res.data.columns || [],
                rows: res.data.rows || [],
                count: res.data.count || 0,
            })
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        } finally {
            setLoading(false)
        }
    }

    const handleRunQuery = () => {
        runQuery(query)
    }

    const fileOpened = tables.length > 0

    return (
        <Box>
            {/* Header */}
            <Flex align="center" gap="3" mb="4">
                <Database size={20} style={{ color: 'var(--accent-9)' }} />
                <Box>
                    <Heading size="5" style={{ color: 'var(--cp-text)' }}>
                        {t('database.sqlite_title')}
                    </Heading>
                    <Text size="2" color="gray" as="p">
                        {t('database.sqlite_subtitle')}
                    </Text>
                </Box>
            </Flex>

            {/* Read-only warning */}
            <Callout.Root color="blue" size="1" mb="4">
                <Callout.Icon>
                    <AlertCircle size={16} />
                </Callout.Icon>
                <Callout.Text>{t('database.sqlite_readonly')}</Callout.Text>
            </Callout.Root>

            {/* File selector */}
            <Flex gap="2" mb="4" align="end">
                <Box style={{ flex: 1 }}>
                    <TextField.Root
                        value={filePath}
                        onChange={(e) => setFilePath(e.target.value)}
                        placeholder={t('database.sqlite_path_placeholder')}
                        onKeyDown={(e) => e.key === 'Enter' && handleOpen()}
                    >
                        <TextField.Slot>
                            <FolderOpen size={14} />
                        </TextField.Slot>
                    </TextField.Root>
                </Box>
                <Button size="2" onClick={handleOpen} disabled={loading || !filePath.trim()}>
                    <FolderOpen size={14} />
                    {loading ? t('common.loading') : t('database.sqlite_open')}
                </Button>
            </Flex>

            {/* Main content */}
            {fileOpened && (
                <>
                    <Separator size="4" mb="4" />
                    <Flex gap="4">
                        {/* Left column - Tables list */}
                        <Box style={{ width: 200, minWidth: 200 }}>
                            <Flex align="center" gap="2" mb="3">
                                <Table2 size={16} style={{ color: 'var(--accent-9)' }} />
                                <Heading size="3">{t('database.sqlite_tables')}</Heading>
                            </Flex>
                            {tables.length === 0 ? (
                                <Text size="2" color="gray">{t('database.sqlite_no_tables')}</Text>
                            ) : (
                                <Flex direction="column" gap="1">
                                    {tables.map((table) => (
                                        <Card
                                            key={table}
                                            size="1"
                                            style={{
                                                cursor: 'pointer',
                                                background: selectedTable === table ? 'var(--accent-3)' : undefined,
                                                borderColor: selectedTable === table ? 'var(--accent-7)' : undefined,
                                            }}
                                            onClick={() => handleSelectTable(table)}
                                        >
                                            <Flex align="center" gap="2">
                                                <Table2 size={14} style={{ color: selectedTable === table ? 'var(--accent-11)' : 'var(--gray-9)', flexShrink: 0 }} />
                                                <Text
                                                    size="2"
                                                    weight={selectedTable === table ? 'medium' : 'regular'}
                                                    style={{
                                                        color: selectedTable === table ? 'var(--accent-11)' : undefined,
                                                        overflow: 'hidden',
                                                        textOverflow: 'ellipsis',
                                                        whiteSpace: 'nowrap',
                                                    }}
                                                >
                                                    {table}
                                                </Text>
                                            </Flex>
                                        </Card>
                                    ))}
                                </Flex>
                            )}
                        </Box>

                        {/* Right column */}
                        <Box style={{ flex: 1, minWidth: 0 }}>
                            {/* Schema section */}
                            {selectedTable && schema.length > 0 && (
                                <Box mb="4">
                                    <Heading size="3" mb="2">{t('database.sqlite_schema')}</Heading>
                                    <Table.Root variant="surface" size="1">
                                        <Table.Header>
                                            <Table.Row>
                                                <Table.ColumnHeaderCell>{t('database.col_name')}</Table.ColumnHeaderCell>
                                                <Table.ColumnHeaderCell>{t('database.col_type')}</Table.ColumnHeaderCell>
                                                <Table.ColumnHeaderCell>{t('database.col_notnull')}</Table.ColumnHeaderCell>
                                                <Table.ColumnHeaderCell>{t('database.col_pk')}</Table.ColumnHeaderCell>
                                                <Table.ColumnHeaderCell>{t('database.col_default')}</Table.ColumnHeaderCell>
                                            </Table.Row>
                                        </Table.Header>
                                        <Table.Body>
                                            {schema.map((col, i) => (
                                                <Table.Row key={i}>
                                                    <Table.Cell>
                                                        <Code size="2">{col.name}</Code>
                                                    </Table.Cell>
                                                    <Table.Cell>
                                                        <Badge size="1" variant="soft" color="gray">{col.type || 'TEXT'}</Badge>
                                                    </Table.Cell>
                                                    <Table.Cell>
                                                        {col.notnull ? <Badge size="1" color="orange">YES</Badge> : <Text size="2" color="gray">NO</Text>}
                                                    </Table.Cell>
                                                    <Table.Cell>
                                                        {col.pk ? <Badge size="1" color="blue">YES</Badge> : <Text size="2" color="gray">NO</Text>}
                                                    </Table.Cell>
                                                    <Table.Cell>
                                                        <Text size="2" color="gray">{col.dflt_value !== null && col.dflt_value !== undefined ? String(col.dflt_value) : '—'}</Text>
                                                    </Table.Cell>
                                                </Table.Row>
                                            ))}
                                        </Table.Body>
                                    </Table.Root>
                                </Box>
                            )}

                            {/* Query section */}
                            <Box mb="4">
                                <TextArea
                                    value={query}
                                    onChange={(e) => setQuery(e.target.value)}
                                    placeholder="SELECT * FROM ..."
                                    rows={4}
                                    style={{ fontFamily: "'JetBrains Mono', 'Fira Code', monospace", fontSize: 13 }}
                                    onKeyDown={(e) => {
                                        if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                                            e.preventDefault()
                                            handleRunQuery()
                                        }
                                    }}
                                />
                                <Flex mt="2" justify="end">
                                    <Button size="2" onClick={handleRunQuery} disabled={loading || !query.trim()}>
                                        <Play size={14} />
                                        {loading ? t('common.loading') : t('database.sqlite_run')}
                                    </Button>
                                </Flex>
                            </Box>

                            {/* Error display */}
                            {error && (
                                <Callout.Root color="red" size="1" mb="4">
                                    <Callout.Icon>
                                        <AlertCircle size={16} />
                                    </Callout.Icon>
                                    <Callout.Text>{error}</Callout.Text>
                                </Callout.Root>
                            )}

                            {/* Results section */}
                            {result && (
                                <Box>
                                    <Flex align="center" gap="2" mb="2">
                                        <Badge size="1" variant="soft">
                                            {t('database.sqlite_rows', { count: result.count })}
                                        </Badge>
                                    </Flex>
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
                        </Box>
                    </Flex>
                </>
            )}

            {/* Error display when file is not opened yet */}
            {!fileOpened && error && (
                <Callout.Root color="red" size="1" mt="4">
                    <Callout.Icon>
                        <AlertCircle size={16} />
                    </Callout.Icon>
                    <Callout.Text>{error}</Callout.Text>
                </Callout.Root>
            )}
        </Box>
    )
}
