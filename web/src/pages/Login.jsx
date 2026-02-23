import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router'
import { Box, Card, Flex, Heading, Text, TextField, Button, Callout } from '@radix-ui/themes'
import { Zap, AlertCircle, Check, ShieldCheck, Loader2 } from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'
import sha256 from '../utils/sha256.js'

// ============ PoW Captcha Component ============
// Pure JS implementation — works over HTTP (no Web Crypto needed)
function PowCaptcha({ onVerified, onReset }) {
    const [state, setState] = useState('idle') // idle | solving | verified | error
    const [progress, setProgress] = useState(0)

    const solve = useCallback(async () => {
        if (state === 'solving' || state === 'verified') return
        setState('solving')
        setProgress(0)
        onReset?.()

        try {
            // 1. Fetch challenge from server
            const res = await fetch('/api/auth/altcha-challenge')
            if (!res.ok) throw new Error('Failed to fetch challenge')
            const challenge = await res.json()

            const { salt, challenge: target, maxnumber, algorithm, signature } = challenge
            const max = maxnumber || 50000

            // 2. Solve PoW: find number n where SHA-256(salt + n) === target
            let found = -1
            const batchSize = 1000
            for (let i = 0; i <= max; i++) {
                const hash = sha256(salt + String(i))
                if (hash === target) {
                    found = i
                    break
                }
                if (i % batchSize === 0) {
                    setProgress(Math.min(95, Math.round((i / max) * 100)))
                    // Yield to UI thread
                    await new Promise(r => setTimeout(r, 0))
                }
            }

            if (found < 0) {
                setState('error')
                return
            }

            // 3. Build payload (same format as Altcha library expects)
            const payload = {
                algorithm: algorithm || 'SHA-256',
                challenge: target,
                number: found,
                salt: salt,
                signature: signature,
            }

            const base64Payload = btoa(JSON.stringify(payload))
            setState('verified')
            setProgress(100)
            onVerified?.(base64Payload)
        } catch {
            setState('error')
        }
    }, [state, onVerified, onReset])

    const handleClick = () => {
        if (state === 'error') {
            setState('idle')
        }
        if (state === 'idle' || state === 'error') {
            solve()
        }
    }

    return (
        <div
            className={`pow-captcha ${state}`}
            onClick={handleClick}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') handleClick() }}
        >
            <div className="pow-captcha-checkbox">
                {state === 'idle' && <div className="pow-captcha-box" />}
                {state === 'solving' && <Loader2 size={18} className="pow-captcha-spinner" />}
                {state === 'verified' && <Check size={18} />}
                {state === 'error' && <AlertCircle size={16} />}
            </div>
            <div className="pow-captcha-label">
                {state === 'idle' && '点击进行安全验证'}
                {state === 'solving' && `验证中... ${progress}%`}
                {state === 'verified' && '验证通过'}
                {state === 'error' && '验证失败，点击重试'}
            </div>
            <div className="pow-captcha-icon">
                <ShieldCheck size={20} />
            </div>
        </div>
    )
}

// ============ Login Page ============
export default function Login() {
    const navigate = useNavigate()
    const { needSetup, loading, login, setup } = useAuthStore()
    const [username, setUsername] = useState('')
    const [password, setPassword] = useState('')
    const [error, setError] = useState('')
    const [submitting, setSubmitting] = useState(false)
    const [altchaPayload, setAltchaPayload] = useState('')
    const [captchaKey, setCaptchaKey] = useState(0)

    const handleVerified = (payload) => {
        setAltchaPayload(payload)
    }

    const handleCaptchaReset = () => {
        setAltchaPayload('')
    }

    const handleSubmit = async (e) => {
        e.preventDefault()
        setError('')
        setSubmitting(true)

        try {
            if (needSetup) {
                await setup(username, password)
            } else {
                if (!altchaPayload) {
                    setError('请先完成安全验证')
                    setSubmitting(false)
                    return
                }
                await login(username, password, altchaPayload)
            }
            navigate('/', { replace: true })
        } catch (err) {
            const msg = err.response?.data?.error || 'Connection failed'
            setError(msg)
            // Reset captcha on error
            setAltchaPayload('')
            setCaptchaKey(k => k + 1)
        } finally {
            setSubmitting(false)
        }
    }

    if (loading) {
        return (
            <Flex align="center" justify="center" style={{ minHeight: '100vh' }}>
                <Text size="3" color="gray">Loading...</Text>
            </Flex>
        )
    }

    return (
        <Flex
            align="center"
            justify="center"
            style={{
                minHeight: '100vh',
                background: 'var(--cp-bg)',
            }}
        >
            <Box style={{ width: '100%', maxWidth: 400, padding: '0 16px' }}>
                {/* Logo */}
                <Flex direction="column" align="center" gap="2" mb="6">
                    <Flex
                        align="center"
                        justify="center"
                        style={{
                            width: 56,
                            height: 56,
                            borderRadius: 16,
                            background: 'linear-gradient(135deg, #10b981 0%, #059669 100%)',
                            boxShadow: '0 8px 32px rgba(16, 185, 129, 0.25)',
                        }}
                    >
                        <Zap size={28} color="white" />
                    </Flex>
                    <Heading size="6" weight="bold" style={{ color: 'var(--cp-text)' }}>
                        CaddyPanel
                    </Heading>
                    <Text size="2" color="gray">
                        Reverse Proxy Management
                    </Text>
                </Flex>

                {/* Login Card */}
                <Card
                    size="3"
                    style={{
                        background: 'var(--cp-card)',
                        border: '1px solid var(--cp-border)',
                        boxShadow: 'var(--cp-shadow-lg)',
                    }}
                >
                    <form onSubmit={handleSubmit}>
                        <Flex direction="column" gap="4">
                            <Heading size="4" align="center">
                                {needSetup ? 'Create Admin Account' : 'Sign In'}
                            </Heading>

                            {needSetup && (
                                <Callout.Root color="blue" size="1">
                                    <Callout.Icon>
                                        <AlertCircle size={16} />
                                    </Callout.Icon>
                                    <Callout.Text>
                                        First time setup — create your admin account.
                                    </Callout.Text>
                                </Callout.Root>
                            )}

                            {error && (
                                <Callout.Root color="red" size="1">
                                    <Callout.Icon>
                                        <AlertCircle size={16} />
                                    </Callout.Icon>
                                    <Callout.Text>{error}</Callout.Text>
                                </Callout.Root>
                            )}

                            <Flex direction="column" gap="1">
                                <Text as="label" size="2" weight="medium" htmlFor="username">
                                    Username
                                </Text>
                                <TextField.Root
                                    id="username"
                                    placeholder="admin"
                                    value={username}
                                    onChange={(e) => setUsername(e.target.value)}
                                    required
                                    autoFocus
                                    size="3"
                                />
                            </Flex>

                            <Flex direction="column" gap="1">
                                <Text as="label" size="2" weight="medium" htmlFor="password">
                                    Password
                                </Text>
                                <TextField.Root
                                    id="password"
                                    type="password"
                                    placeholder="••••••••"
                                    value={password}
                                    onChange={(e) => setPassword(e.target.value)}
                                    required
                                    size="3"
                                />
                            </Flex>

                            {/* PoW verification (not shown during initial setup) */}
                            {!needSetup && (
                                <PowCaptcha
                                    key={captchaKey}
                                    onVerified={handleVerified}
                                    onReset={handleCaptchaReset}
                                />
                            )}

                            <Button
                                type="submit"
                                size="3"
                                disabled={submitting || !username || !password || (!needSetup && !altchaPayload)}
                                style={{ cursor: 'pointer' }}
                            >
                                {submitting
                                    ? 'Please wait...'
                                    : needSetup
                                        ? 'Create Account'
                                        : 'Sign In'}
                            </Button>
                        </Flex>
                    </form>
                </Card>

                <Text size="1" color="gray" align="center" mt="4" as="p">
                    CaddyPanel — Powered by Caddy Server
                </Text>
            </Box>
        </Flex>
    )
}
