import { useState, useRef } from 'react'
import { Box, Flex, Text, Card, Button, Code, ScrollArea, Select } from '@radix-ui/themes'
import { Container, RefreshCw, AlertTriangle, Download, Terminal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { dockerAPI } from '../api'

export default function DockerRequired({ installed, daemonRunning, error, onRetry, extraMessage }) {
    const { t } = useTranslation()
    const [installing, setInstalling] = useState(false)
    const [installLog, setInstallLog] = useState([])
    const [installDone, setInstallDone] = useState(false)
    const [installError, setInstallError] = useState(null)
    const [mirror, setMirror] = useState('none')
    const logEndRef = useRef(null)

    const scrollToBottom = () => {
        logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }

    const handleInstall = async () => {
        setInstalling(true)
        setInstallLog([])
        setInstallDone(false)
        setInstallError(null)

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
                        setInstallLog(prev => [...prev, data])
                        setTimeout(scrollToBottom, 50)
                    } else if (line.startsWith('event: done')) {
                        setInstallDone(true)
                    } else if (line.startsWith('event: error')) {
                        setInstallError(true)
                    }
                }
            }
        } catch (err) {
            setInstallError(err.message)
        } finally {
            setInstalling(false)
        }
    }

    // Show install progress view
    if (installing || installLog.length > 0) {
        return (
            <Card style={{ padding: 32, maxWidth: 720, margin: '40px auto' }}>
                <Flex align="center" gap="2" mb="3">
                    <Terminal size={20} />
                    <Text size="4" weight="bold">{t('docker.installing')}</Text>
                </Flex>
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
                <Flex gap="2" mt="3" justify="end">
                    {installDone && !installError && (
                        <Button onClick={onRetry}>
                            <RefreshCw size={16} />
                            {t('docker.check_again')}
                        </Button>
                    )}
                    {installError && (
                        <Button variant="soft" color="red" onClick={handleInstall}>
                            {t('docker.retry_install')}
                        </Button>
                    )}
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
