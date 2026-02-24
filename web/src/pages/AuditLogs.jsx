import { useState, useEffect } from 'react'
import { Box, Flex, Heading, Text, Card, Table, Badge, Button, Spinner } from '@radix-ui/themes'
import { ClipboardList, ChevronLeft, ChevronRight } from 'lucide-react'
import { auditAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

const actionColors = {
    CREATE: 'green',
    UPDATE: 'blue',
    DELETE: 'red',
    ENABLE: 'green',
    DISABLE: 'orange',
    TOGGLE: 'orange',
    START: 'green',
    STOP: 'red',
    RELOAD: 'blue',
}

export default function AuditLogs() {
    const { t } = useTranslation()
    const [logs, setLogs] = useState([])
    const [total, setTotal] = useState(0)
    const [page, setPage] = useState(1)
    const [loading, setLoading] = useState(true)
    const perPage = 20

    const fetchLogs = async (p = 1) => {
        setLoading(true)
        try {
            const res = await auditAPI.list({ page: p, per_page: perPage })
            setLogs(res.data.logs || [])
            setTotal(res.data.total || 0)
            setPage(p)
        } catch (err) {
            console.error('Failed to fetch audit logs:', err)
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => { fetchLogs() }, [])

    const totalPages = Math.ceil(total / perPage)

    return (
        <Box>
            <Flex align="center" gap="2" mb="1">
                <ClipboardList size={22} style={{ color: 'var(--violet-9)' }} />
                <Heading size="6" style={{ color: 'var(--cp-text)' }}>{t('audit.title')}</Heading>
            </Flex>
            <Text size="2" color="gray" mb="5" as="p">
                {t('audit.subtitle_with_count', { count: total })}
            </Text>

            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }}>
                {loading ? (
                    <Flex justify="center" p="6"><Spinner size="3" /></Flex>
                ) : (
                    <Table.Root>
                        <Table.Header>
                            <Table.Row>
                                <Table.ColumnHeaderCell>{t('audit.time')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.user')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.action')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.target')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.detail')}</Table.ColumnHeaderCell>
                                <Table.ColumnHeaderCell>{t('audit.ip')}</Table.ColumnHeaderCell>
                            </Table.Row>
                        </Table.Header>
                        <Table.Body>
                            {logs.map((log) => (
                                <Table.Row key={log.id}>
                                    <Table.Cell>
                                        <Text size="1" color="gray">
                                            {new Date(log.created_at).toLocaleString()}
                                        </Text>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Text size="2" weight="medium">{log.username}</Text>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Badge color={actionColors[log.action] || 'gray'} size="1">
                                            {log.action}
                                        </Badge>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Text size="2">{log.target}</Text>
                                        {log.target_id && (
                                            <Text size="1" color="gray"> #{log.target_id}</Text>
                                        )}
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Text size="1" style={{ maxWidth: 300, display: 'block' }}>
                                            {log.detail}
                                        </Text>
                                    </Table.Cell>
                                    <Table.Cell>
                                        <Text size="1" color="gray">{log.ip}</Text>
                                    </Table.Cell>
                                </Table.Row>
                            ))}
                            {logs.length === 0 && (
                                <Table.Row>
                                    <Table.Cell colSpan={6}>
                                        <Text color="gray" size="2">{t('audit.no_logs')}</Text>
                                    </Table.Cell>
                                </Table.Row>
                            )}
                        </Table.Body>
                    </Table.Root>
                )}

                {totalPages > 1 && (
                    <Flex justify="center" align="center" gap="3" pt="3" pb="1">
                        <Button
                            size="1"
                            variant="soft"
                            disabled={page <= 1}
                            onClick={() => fetchLogs(page - 1)}
                        >
                            <ChevronLeft size={14} /> {t('common.prev_page')}
                        </Button>
                        <Text size="2" color="gray">
                            {page} / {totalPages}
                        </Text>
                        <Button
                            size="1"
                            variant="soft"
                            disabled={page >= totalPages}
                            onClick={() => fetchLogs(page + 1)}
                        >
                            {t('common.next_page')} <ChevronRight size={14} />
                        </Button>
                    </Flex>
                )}
            </Card>
        </Box>
    )
}
