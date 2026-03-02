import { Box, Flex, Text, Card, Button, Code } from '@radix-ui/themes'
import { Container, RefreshCw, AlertTriangle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export default function DockerRequired({ installed, daemonRunning, error, onRetry, extraMessage }) {
    const { t } = useTranslation()

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
                <Text size="2" color="gray" style={{ display: 'block', marginBottom: 8 }}>
                    {t('docker.install_command')}
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
                    curl -fsSL https://get.docker.com | sh
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
