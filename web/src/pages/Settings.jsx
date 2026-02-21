import { useState, useEffect, useRef } from 'react'
import {
    Box, Flex, Heading, Text, Button, Card, Badge, Callout, Separator, Code,
    Tabs,
} from '@radix-ui/themes'
import {
    Play, Square, RefreshCw, Download, Upload, Server, FileCode,
    AlertCircle, CheckCircle2,
} from 'lucide-react'
import { caddyAPI, configAPI } from '../api/index.js'

export default function Settings() {
    const [caddyStatus, setCaddyStatus] = useState(null)
    const [caddyfile, setCaddyfile] = useState('')
    const [actionLoading, setActionLoading] = useState(null)
    const [message, setMessage] = useState(null)
    const fileInputRef = useRef(null)

    const fetchStatus = async () => {
        try {
            const res = await caddyAPI.status()
            setCaddyStatus(res.data)
        } catch (err) {
            console.error('Failed to fetch Caddy status:', err)
        }
    }

    const fetchCaddyfile = async () => {
        try {
            const res = await caddyAPI.caddyfile()
            setCaddyfile(res.data.content || '')
        } catch {
            setCaddyfile('# Caddyfile not found')
        }
    }

    useEffect(() => {
        fetchStatus()
        fetchCaddyfile()
    }, [])

    const showMessage = (type, text) => {
        setMessage({ type, text })
        setTimeout(() => setMessage(null), 5000)
    }

    const handleCaddyAction = async (action) => {
        setActionLoading(action)
        try {
            let res
            switch (action) {
                case 'start':
                    res = await caddyAPI.start()
                    break
                case 'stop':
                    res = await caddyAPI.stop()
                    break
                case 'reload':
                    res = await caddyAPI.reload()
                    break
            }
            showMessage('success', res.data.message)
            await fetchStatus()
        } catch (err) {
            showMessage('error', err.response?.data?.error || `Failed to ${action} Caddy`)
        } finally {
            setActionLoading(null)
        }
    }

    const handleExport = async () => {
        setActionLoading('export')
        try {
            const res = await configAPI.export()
            const blob = new Blob([JSON.stringify(res.data, null, 2)], {
                type: 'application/json',
            })
            const url = URL.createObjectURL(blob)
            const link = document.createElement('a')
            link.href = url
            link.download = `caddypanel-export-${new Date().toISOString().slice(0, 10)}.json`
            link.click()
            URL.revokeObjectURL(url)
            showMessage('success', 'Configuration exported successfully')
        } catch (err) {
            showMessage('error', 'Failed to export configuration')
        } finally {
            setActionLoading(null)
        }
    }

    const handleImport = async (e) => {
        const file = e.target.files?.[0]
        if (!file) return

        setActionLoading('import')
        try {
            const text = await file.text()
            const data = JSON.parse(text)
            const res = await configAPI.import(data)
            showMessage('success', res.data.message)
            await fetchStatus()
            await fetchCaddyfile()
        } catch (err) {
            showMessage('error', err.response?.data?.error || 'Invalid import file')
        } finally {
            setActionLoading(null)
            e.target.value = '' // reset file input
        }
    }

    const running = caddyStatus?.running

    return (
        <Box>
            <Heading size="6" mb="1" style={{ color: '#fafafa' }}>Settings</Heading>
            <Text size="2" color="gray" mb="5" as="p">
                Manage Caddy server and panel configuration
            </Text>

            {/* Status message */}
            {message && (
                <Callout.Root
                    color={message.type === 'success' ? 'green' : 'red'}
                    size="1"
                    mb="4"
                >
                    <Callout.Icon>
                        {message.type === 'success' ? <CheckCircle2 size={14} /> : <AlertCircle size={14} />}
                    </Callout.Icon>
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Tabs.Root defaultValue="caddy">
                <Tabs.List>
                    <Tabs.Trigger value="caddy">
                        <Server size={14} style={{ marginRight: 6 }} /> Caddy Server
                    </Tabs.Trigger>
                    <Tabs.Trigger value="caddyfile">
                        <FileCode size={14} style={{ marginRight: 6 }} /> Caddyfile
                    </Tabs.Trigger>
                    <Tabs.Trigger value="backup">
                        <Download size={14} style={{ marginRight: 6 }} /> Backup
                    </Tabs.Trigger>
                </Tabs.List>

                {/* ---- Caddy Server Tab ---- */}
                <Tabs.Content value="caddy">
                    <Card mt="4" style={{ background: '#111113', border: '1px solid #1e1e22' }}>
                        <Heading size="3" mb="4">Process Control</Heading>

                        <Flex align="center" gap="3" mb="4">
                            <Text size="2" color="gray">Status:</Text>
                            <Badge
                                color={running ? 'green' : 'red'}
                                variant="solid"
                                size="2"
                            >
                                {running ? '● Running' : '○ Stopped'}
                            </Badge>
                            {caddyStatus?.version && (
                                <Badge variant="soft" size="1">{caddyStatus.version}</Badge>
                            )}
                        </Flex>

                        <Flex gap="2" wrap="wrap">
                            <Button
                                color="green"
                                disabled={running || actionLoading === 'start'}
                                onClick={() => handleCaddyAction('start')}
                            >
                                <Play size={14} />
                                {actionLoading === 'start' ? 'Starting...' : 'Start'}
                            </Button>
                            <Button
                                color="red"
                                variant="soft"
                                disabled={!running || actionLoading === 'stop'}
                                onClick={() => handleCaddyAction('stop')}
                            >
                                <Square size={14} />
                                {actionLoading === 'stop' ? 'Stopping...' : 'Stop'}
                            </Button>
                            <Button
                                variant="soft"
                                disabled={!running || actionLoading === 'reload'}
                                onClick={() => handleCaddyAction('reload')}
                            >
                                <RefreshCw size={14} />
                                {actionLoading === 'reload' ? 'Reloading...' : 'Reload'}
                            </Button>
                        </Flex>
                    </Card>
                </Tabs.Content>

                {/* ---- Caddyfile Tab ---- */}
                <Tabs.Content value="caddyfile">
                    <Card mt="4" style={{ background: '#111113', border: '1px solid #1e1e22' }}>
                        <Flex justify="between" align="center" mb="3">
                            <Heading size="3">Generated Caddyfile</Heading>
                            <Button variant="ghost" size="1" onClick={fetchCaddyfile}>
                                <RefreshCw size={12} /> Refresh
                            </Button>
                        </Flex>

                        <Box
                            style={{
                                background: '#0a0a0b',
                                border: '1px solid #1e1e22',
                                borderRadius: 8,
                                padding: 16,
                                maxHeight: 500,
                                overflow: 'auto',
                            }}
                        >
                            <pre className="log-viewer" style={{ margin: 0, color: '#d4d4d8' }}>
                                {caddyfile || '# No Caddyfile generated yet. Add a proxy host first.'}
                            </pre>
                        </Box>
                    </Card>
                </Tabs.Content>

                {/* ---- Backup Tab ---- */}
                <Tabs.Content value="backup">
                    <Card mt="4" style={{ background: '#111113', border: '1px solid #1e1e22' }}>
                        <Heading size="3" mb="4">Import / Export</Heading>

                        <Text size="2" color="gray" mb="4" as="p">
                            Export your entire configuration as JSON, or import from a previous backup.
                        </Text>

                        <Flex gap="3" wrap="wrap">
                            <Button
                                onClick={handleExport}
                                disabled={actionLoading === 'export'}
                            >
                                <Download size={14} />
                                {actionLoading === 'export' ? 'Exporting...' : 'Export Configuration'}
                            </Button>

                            <Button
                                variant="soft"
                                color="gray"
                                onClick={() => fileInputRef.current?.click()}
                                disabled={actionLoading === 'import'}
                            >
                                <Upload size={14} />
                                {actionLoading === 'import' ? 'Importing...' : 'Import Configuration'}
                            </Button>

                            <input
                                ref={fileInputRef}
                                type="file"
                                accept=".json"
                                onChange={handleImport}
                                style={{ display: 'none' }}
                            />
                        </Flex>

                        <Callout.Root color="orange" size="1" mt="4">
                            <Callout.Icon><AlertCircle size={14} /></Callout.Icon>
                            <Callout.Text>
                                Importing will <strong>replace</strong> all existing host configurations.
                                Make sure to export a backup first.
                            </Callout.Text>
                        </Callout.Root>
                    </Card>
                </Tabs.Content>
            </Tabs.Root>
        </Box>
    )
}
