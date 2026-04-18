import { useState, useRef, useEffect } from 'react'
import { Box, Flex, Text, Card, Button, Code, ScrollArea, Select, Callout } from '@radix-ui/themes'
import { Container, RefreshCw, AlertTriangle, Download, Terminal, Copy, Check, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { dockerAPI } from '../api'
import { copyToClipboard } from '../utils/clipboard.js'

// DockerRequired renders the install / not-running gate for the Docker
// plugin. The `runtime` prop is required so the recovery commands match the
// actual runtime in use; v0.12 Podman hosts get `systemctl start podman.socket`
// while legacy Docker hosts get `systemctl start docker`. Unknown (neither
// runtime detected) falls back to the Podman install guidance, since v0.12's
// install.sh provisions Podman.
export default function DockerRequired({ installed, daemonRunning, error, onRetry, extraMessage, runtime }) {
    const { t } = useTranslation()
    const isDocker = runtime === 'docker'
    const startCmd = isDocker ? 'sudo systemctl start docker' : 'sudo systemctl start podman.socket'
    const [installing, setInstalling] = useState(false)
    const [installLog, setInstallLog] = useState([])
    const [installDone, setInstallDone] = useState(false)
    const [installError, setInstallError] = useState(null)
    const [needsReboot, setNeedsReboot] = useState(false)
    const [mirror, setMirror] = useState('none')
    const [copied, setCopied] = useState(false)
    const logEndRef = useRef(null)
    // Keep a mutable ref of the log lines so the copy and catch handlers always
    // see the latest data, avoiding stale-closure issues with React state.
    const logLinesRef = useRef([])
    // Track live install resources so unmount can cancel them cleanly: the
    // fetch AbortController aborts the SSE request (and lets the backend kill
    // the subprocess via CommandContext), the reader is released, pending
    // scrollToBottom timers are cleared, and a mounted-flag stops any
    // in-flight setState calls after unmount (React 19 warns about these).
    const abortRef = useRef(null)
    const readerRef = useRef(null)
    const scrollTimersRef = useRef(new Set())
    const mountedRef = useRef(true)

    useEffect(() => {
        return () => {
            mountedRef.current = false
            abortRef.current?.abort()
            readerRef.current?.cancel?.().catch(() => {})
            scrollTimersRef.current.forEach((id) => clearTimeout(id))
            scrollTimersRef.current.clear()
        }
    }, [])

    const scrollToBottom = () => {
        logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }

    const handleCopyLogs = () => {
        const text = logLinesRef.current.join('\n')
        copyToClipboard(text, () => {
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
        })
    }

    const handleInstall = async () => {
        setInstalling(true)
        setInstallLog([])
        logLinesRef.current = []
        setInstallDone(false)
        setInstallError(null)
        setNeedsReboot(false)

        let detectedReboot = false
        const controller = new AbortController()
        abortRef.current = controller
        // Guarded setState helpers so a late chunk arriving after unmount
        // (or after abort) doesn't trigger the React warning.
        const safeSet = (setter) => (...args) => { if (mountedRef.current) setter(...args) }
        const safeSetInstallDone = safeSet(setInstallDone)
        const safeSetInstallError = safeSet(setInstallError)
        const safeSetNeedsReboot = safeSet(setNeedsReboot)
        const safeSetInstallLog = safeSet(setInstallLog)

        try {
            const token = localStorage.getItem('token')
            const response = await fetch('/api/plugins/docker/install', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': `Bearer ${token}`,
                },
                body: JSON.stringify({ mirror: mirror === 'none' ? '' : mirror }),
                signal: controller.signal,
            })

            const reader = response.body.getReader()
            readerRef.current = reader
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
                        const data = line.slice(6)
                        // Detect reboot signal from EasyDocker script
                        if (data.toLowerCase().includes('reboot')) {
                            detectedReboot = true
                        }
                        logLinesRef.current = [...logLinesRef.current, data]
                        safeSetInstallLog(prev => [...prev, data])
                        const tid = setTimeout(() => {
                            scrollTimersRef.current.delete(tid)
                            if (mountedRef.current) scrollToBottom()
                        }, 50)
                        scrollTimersRef.current.add(tid)
                    } else if (line.startsWith('event: done')) {
                        safeSetInstallDone(true)
                    } else if (line.startsWith('event: reboot')) {
                        detectedReboot = true
                        safeSetNeedsReboot(true)
                    } else if (line.startsWith('event: error')) {
                        safeSetInstallError(true)
                    }
                }
            }

            // If stream ended cleanly after reboot detection
            if (detectedReboot) {
                safeSetNeedsReboot(true)
            }
        } catch (err) {
            // Abort during unmount is expected — drop silently.
            if (err?.name === 'AbortError') return
            // Connection dropped — check if it was due to a server reboot
            if (detectedReboot) {
                safeSetNeedsReboot(true)
                safeSetInstallError(null)
            } else {
                // Check log lines for reboot signals
                const hasReboot = logLinesRef.current.some(l => l.toLowerCase().includes('reboot'))
                if (hasReboot) {
                    safeSetNeedsReboot(true)
                } else {
                    safeSetInstallError('network_error')
                }
            }
        } finally {
            abortRef.current = null
            readerRef.current = null
            if (mountedRef.current) setInstalling(false)
        }
    }

    // After successful install, reload the entire page to reset all plugin state
    const handlePostInstallCheck = () => {
        window.location.reload()
    }

    // Show install progress view
    if (installing || installLog.length > 0) {
        return (
            <Card style={{ padding: 32, maxWidth: 720, margin: '40px auto' }}>
                <Flex align="center" gap="2" mb="3">
                    <Terminal size={20} />
                    <Text size="4" weight="bold">
                        {installDone && !installError
                            ? t('docker.install_success')
                            : t('docker.installing')}
                    </Text>
                </Flex>

                {needsReboot && (
                    <Callout.Root color="orange" mb="3">
                        <Callout.Icon>
                            <RotateCcw size={16} />
                        </Callout.Icon>
                        <Callout.Text>
                            {t('docker.reboot_required')}
                        </Callout.Text>
                    </Callout.Root>
                )}

                {installDone && !installError && !needsReboot && (
                    <Callout.Root color="green" mb="3">
                        <Callout.Icon>
                            <Check size={16} />
                        </Callout.Icon>
                        <Callout.Text>
                            {t('docker.install_success_desc')}
                        </Callout.Text>
                    </Callout.Root>
                )}

                <ScrollArea style={{
                    height: 360,
                    background: 'var(--gray-2)',
                    borderRadius: 8,
                    padding: '12px 16px',
                    fontFamily: 'monospace',
                    fontSize: 13,
                    lineHeight: 1.6,
                }}>
                    {installLog.map((line, i) => (
                        <Text key={i} as="div" size="1" style={{ fontFamily: 'monospace', whiteSpace: 'pre-wrap' }}>
                            {line}
                        </Text>
                    ))}
                    <div ref={logEndRef} />
                </ScrollArea>
                <Flex gap="2" mt="3" justify="between">
                    <Button variant="soft" color="gray" onClick={handleCopyLogs} disabled={installLog.length === 0}>
                        {copied ? <Check size={16} /> : <Copy size={16} />}
                        {copied ? t('docker.copied') : t('docker.copy_logs')}
                    </Button>
                    <Flex gap="2">
                        {(installDone || needsReboot) && !installError && (
                            <Button onClick={handlePostInstallCheck}>
                                <RefreshCw size={16} />
                                {t('docker.check_again')}
                            </Button>
                        )}
                        {installError && !needsReboot && (
                            <Button variant="soft" color="red" onClick={handleInstall}>
                                {t('docker.retry_install')}
                            </Button>
                        )}
                    </Flex>
                </Flex>
            </Card>
        )
    }

    if (!installed) {
        return (
            <Card style={{ padding: 48, textAlign: 'center', maxWidth: 600, margin: '40px auto' }}>
                <Container size={64} style={{ margin: '0 auto 16px', opacity: 0.3 }} />
                <Text size="5" weight="bold" style={{ display: 'block', marginBottom: 8 }}>
                    {t('docker.not_installed')}
                </Text>
                <Text size="2" color="gray" style={{ display: 'block', marginBottom: 16 }}>
                    {t('docker.not_installed_desc')}
                </Text>
                {extraMessage && (
                    <Text size="2" color="gray" style={{ display: 'block', marginBottom: 16 }}>
                        {extraMessage}
                    </Text>
                )}

                {/* One-click install */}
                <Flex direction="column" gap="3" align="center" mb="4">
                    <Flex gap="2" align="center">
                        <Text size="2" color="gray">{t('docker.mirror')}:</Text>
                        <Select.Root value={mirror} onValueChange={setMirror}>
                            <Select.Trigger style={{ minWidth: 160 }} />
                            <Select.Content>
                                <Select.Item value="none">{t('docker.mirror_official')}</Select.Item>
                                <Select.Item value="public">{t('docker.mirror_public')}</Select.Item>
                            </Select.Content>
                        </Select.Root>
                    </Flex>
                    <Button size="3" onClick={handleInstall}>
                        <Download size={16} />
                        {t('docker.install_now')}
                    </Button>
                </Flex>

                <Text size="1" color="gray" style={{ display: 'block', marginBottom: 8 }}>
                    {t('docker.install_manual')}
                </Text>
                <Code
                    size="2"
                    style={{
                        display: 'block',
                        padding: '12px 16px',
                        borderRadius: 8,
                        marginBottom: 20,
                        fontFamily: 'monospace',
                        userSelect: 'all',
                    }}
                >
                    bash &lt;(curl -sSL https://web.casa/install.sh)
                </Code>
                <Button variant="soft" onClick={onRetry}>
                    <RefreshCw size={16} />
                    {t('docker.check_again')}
                </Button>
            </Card>
        )
    }

    if (installed && !daemonRunning) {
        return (
            <Card style={{ padding: 48, textAlign: 'center', maxWidth: 600, margin: '40px auto' }}>
                <AlertTriangle size={64} style={{ margin: '0 auto 16px', opacity: 0.3, color: 'var(--orange-11)' }} />
                <Text size="5" weight="bold" style={{ display: 'block', marginBottom: 8 }}>
                    {t('docker.not_running')}
                </Text>
                <Text size="2" color="gray" style={{ display: 'block', marginBottom: 16 }}>
                    {t('docker.not_running_desc')}
                </Text>
                {error && (
                    <Text size="1" color="red" style={{ display: 'block', marginBottom: 16 }}>
                        {error}
                    </Text>
                )}
                <Code
                    size="2"
                    style={{
                        display: 'block',
                        padding: '12px 16px',
                        borderRadius: 8,
                        marginBottom: 20,
                        fontFamily: 'monospace',
                        userSelect: 'all',
                    }}
                >
                    {startCmd}
                </Code>
                <Button variant="soft" onClick={onRetry}>
                    <RefreshCw size={16} />
                    {t('docker.check_again')}
                </Button>
            </Card>
        )
    }

    return null
}
