import { useState, useEffect, useRef } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Select, TextField, Badge,
} from '@radix-ui/themes'
import { Download, RefreshCw, Search, FileText } from 'lucide-react'
import { logAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

export default function Logs() {
    const { t } = useTranslation()
    const [logType, setLogType] = useState('caddy')
    const [lines, setLines] = useState('200')
    const [search, setSearch] = useState('')
    const [logLines, setLogLines] = useState([])
    const [logFiles, setLogFiles] = useState([])
    const [loading, setLoading] = useState(false)
    const logEndRef = useRef(null)

    const fetchLogFiles = async () => {
        try {
            const res = await logAPI.files()
            setLogFiles(res.data.files || [])
        } catch (err) {
            console.error('Failed to fetch log files:', err)
        }
    }

    const fetchLogs = async () => {
        setLoading(true)
        try {
            const res = await logAPI.get({ type: logType, lines, search })
            setLogLines(res.data.lines || [])
            // Scroll to bottom
            setTimeout(() => {
                logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
            }, 100)
        } catch (err) {
            console.error('Failed to fetch logs:', err)
            setLogLines([])
        } finally {
            setLoading(false)
        }
    }

    useEffect(() => {
        fetchLogFiles()
        fetchLogs()
    }, [logType, lines])

    const handleSearch = (e) => {
        e.preventDefault()
        fetchLogs()
    }

    const handleDownload = () => {
        const token = localStorage.getItem('token')
        const url = logAPI.downloadUrl(logType)
        // Open download link with auth
        const link = document.createElement('a')
        link.href = `${url}&token=${token}`
        link.download = ''
        link.click()
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="5">
                <Box>
                    <Heading size="6" style={{ color: 'var(--cp-text)' }}>{t('log.title')}</Heading>
                    <Text size="2" color="gray">{t('log.subtitle')}</Text>
                </Box>
                <Flex gap="2">
                    <Button variant="soft" onClick={fetchLogs} disabled={loading}>
                        <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
                        {t('common.refresh')}
                    </Button>
                    <Button variant="soft" color="gray" onClick={handleDownload}>
                        <Download size={14} />
                        {t('common.download')}
                    </Button>
                </Flex>
            </Flex>

            {/* Controls */}
            <Card style={{ background: 'var(--cp-card)', border: '1px solid var(--cp-border)' }} mb="4">
                <Flex gap="3" align="end" wrap="wrap">
                    <Flex direction="column" gap="1" style={{ minWidth: 180 }}>
                        <Text size="1" weight="medium" color="gray">{t('log.select_file')}</Text>
                        <Select.Root value={logType} onValueChange={setLogType}>
                            <Select.Trigger />
                            <Select.Content>
                                <Select.Item value="caddy">{t('log.main_log')}</Select.Item>
                                {logFiles
                                    .filter((f) => f.name !== 'caddy.log')
                                    .map((f) => (
                                        <Select.Item key={f.name} value={f.name}>
                                            {f.name}
                                        </Select.Item>
                                    ))}
                            </Select.Content>
                        </Select.Root>
                    </Flex>

                    <Flex direction="column" gap="1" style={{ minWidth: 100 }}>
                        <Text size="1" weight="medium" color="gray">{t('log.lines')}</Text>
                        <Select.Root value={lines} onValueChange={setLines}>
                            <Select.Trigger />
                            <Select.Content>
                                <Select.Item value="100">100</Select.Item>
                                <Select.Item value="200">200</Select.Item>
                                <Select.Item value="500">500</Select.Item>
                                <Select.Item value="1000">1000</Select.Item>
                                <Select.Item value="5000">5000</Select.Item>
                            </Select.Content>
                        </Select.Root>
                    </Flex>

                    <form onSubmit={handleSearch} style={{ flex: 1, minWidth: 200 }}>
                        <Flex direction="column" gap="1">
                            <Text size="1" weight="medium" color="gray">{t('common.search')}</Text>
                            <Flex gap="2">
                                <TextField.Root
                                    style={{ flex: 1 }}
                                    placeholder={t('log.search_placeholder')}
                                    value={search}
                                    onChange={(e) => setSearch(e.target.value)}
                                    size="2"
                                >
                                    <TextField.Slot>
                                        <Search size={14} style={{ color: 'var(--cp-text-muted)' }} />
                                    </TextField.Slot>
                                </TextField.Root>
                                <Button type="submit" variant="soft" size="2">
                                    {t('log.filter')}
                                </Button>
                            </Flex>
                        </Flex>
                    </form>
                </Flex>
            </Card>

            {/* Log Content */}
            <Card
                style={{
                    background: 'var(--cp-code-bg)',
                    border: '1px solid var(--cp-border)',
                    padding: 0,
                }}
            >
                <Flex
                    justify="between"
                    align="center"
                    px="4"
                    py="2"
                    style={{ borderBottom: '1px solid var(--cp-border)' }}
                >
                    <Flex align="center" gap="2">
                        <FileText size={14} style={{ color: 'var(--cp-text-muted)' }} />
                        <Text size="1" color="gray">{logType}</Text>
                    </Flex>
                    <Badge variant="soft" size="1">
                        {t('log.lines_count', { count: logLines.length })}
                    </Badge>
                </Flex>

                <Box
                    p="4"
                    style={{
                        maxHeight: 600,
                        overflow: 'auto',
                    }}
                >
                    {logLines.length === 0 ? (
                        <Flex justify="center" p="6">
                            <Text size="2" color="gray">
                                {loading ? t('common.loading') : t('log.no_logs')}
                            </Text>
                        </Flex>
                    ) : (
                        <div className="log-viewer">
                            {logLines.map((line, i) => (
                                <div
                                    key={i}
                                    style={{
                                        padding: '1px 0',
                                        borderBottom: '1px solid rgba(255,255,255,0.02)',
                                        display: 'flex',
                                        gap: 12,
                                    }}
                                >
                                    <span style={{ color: 'var(--cp-text-muted)', userSelect: 'none', minWidth: 40, textAlign: 'right' }}>
                                        {i + 1}
                                    </span>
                                    <span style={{ color: 'var(--cp-text)' }}>{line}</span>
                                </div>
                            ))}
                            <div ref={logEndRef} />
                        </div>
                    )}
                </Box>
            </Card>
        </Box>
    )
}
