import { useState, useRef } from 'react'
import { Box, Flex, Text, Card, Button, Code, ScrollArea, Select, Callout } from '@radix-ui/themes'
import { Container, RefreshCw, AlertTriangle, Download, Terminal, Copy, Check, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { dockerAPI } from '../api'

export default function DockerRequired({ installed, daemonRunning, error, onRetry, extraMessage }) {
    const { t } = useTranslation()
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

    const scrollToBottom = () => {
        logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }

    const handleCopyLogs = () => {
        const text = logLinesRef.current.join('\n')
        if (!text) return

        // Use the modern clipboard API when available (HTTPS / localhost)
        if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
            navigator.clipboard.writeText(text).then(() => {
                setCopied(true)
                setTimeout(() => setCopied(false), 2000)
            }).catch(() => {
                fallbackCopy(text)
            })
        } else {
            fallbackCopy(text)
        }
    }

    const fallbackCopy = (text) => {
        try {
            const textarea = document.createElement('textarea')
            textarea.value = text
            // Must be visible for execCommand to work in some browsers
            textarea.style.position = 'fixed'
            textarea.style.left = '0'
            textarea.style.top = '0'
            textarea.style.width = '2em'
            textarea.style.height = '2em'
            textarea.style.opacity = '0.01'
            document.body.appendChild(textarea)
            textarea.focus()
            textarea.select()
            document.execCommand('copy')
            document.body.removeChild(textarea)
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
        } catch {
            // Last resort: open a window with the text so the user can copy manually
            const win = window.open('', '_blank', 'width=600,height=400')
            if (win) {
                win.document.write(`<pre style="white-space:pre-wrap">${text.replace(/</g, '&lt;')}</pre>`)
            }
        }
    }

    const handleInstall = async () => {
        setInstalling(true)
        setInstallLog([])
        logLinesRef.current = []
        setInstallDone(false)
        setInstallError(null)
        setNeedsReboot(false)

        let detectedReboot = false

        try {
            const token = localStorage.getItem('token')
            const response = await fetch('/api/plugins/docker/install', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': `Bearer ${token}`,
                },
                body: JSON.stringify({ mirror: mirror === 'none' ? '' : mirror }),
            })

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
                        const data = line.slice(6)
                        // Detect reboot signal from EasyDocker script
                        if (data.toLowerCase().includes('reboot')) {
                            detectedReboot = true
                        }
                        logLinesRef.current = [...logLinesRef.current, data]
                        setInstallLog(prev => [...prev, data])
                        setTimeout(scrollToBottom, 50)
                    } else if (line.startsWith('event: done')) {
                        setInstallDone(true)
                    } else if (line.startsWith('event: reboot')) {
                        detectedReboot = true
                        setNeedsReboot(true)
                    } else if (line.startsWith('event: error')) {
                        setInstallError(true)
                    }
                }
            }

            // If stream ended cleanly after reboot detection
            if (detectedReboot) {
                setNeedsReboot(true)
            }
        } catch {
            // Connection dropped — check if it was due to a server reboot
            if (detectedReboot) {
                setNeedsReboot(true)
                setInstallError(null)
            } else {
                // Check log lines for reboot signals
                const hasReboot = logLinesRef.current.some(l => l.toLowerCase().includes('reboot'))
                if (hasReboot) {
                    setNeedsReboot(true)
                } else {
                    setInstallError('network_error')
                }
            }
        } finally {
            setInstalling(false)
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
                    bash &lt;(curl -sSL https://raw.githubusercontent.com/web-casa/easydocker/main/docker.sh)
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
                    sudo systemctl start docker
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
