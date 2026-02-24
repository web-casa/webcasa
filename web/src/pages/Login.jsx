import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router'
import { Box, Card, Flex, Heading, Text, TextField, Button, Callout } from '@radix-ui/themes'
import { Zap, AlertCircle, Check, ShieldCheck, Loader2, Languages } from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'
import { useTranslation } from 'react-i18next'
import sha256 from '../utils/sha256.js'

// ============ PoW Captcha Component ============
// Pure JS implementation — works over HTTP (no Web Crypto needed)
function PowCaptcha({ onVerified, onReset }) {
    const { t } = useTranslation()
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
                {state === 'idle' && t('login.pow.idle')}
                {state === 'solving' && t('login.pow.solving', { progress })}
                {state === 'verified' && t('login.pow.verified')}
                {state === 'error' && t('login.pow.error')}
            </div>
            <div className="pow-captcha-icon">
                <ShieldCheck size={20} />
            </div>
        </div>
    )
}

// ============ Language Switcher ============
function LanguageSwitcher() {
    const { i18n } = useTranslation()
    const currentLang = i18n.language?.startsWith('zh') ? 'zh' : 'en'

    const toggle = () => {
        const next = currentLang === 'zh' ? 'en' : 'zh'
        i18n.changeLanguage(next)
    }

    return (
        <button
            onClick={toggle}
            style={{
                background: 'none',
                border: '1px solid var(--cp-border)',
                borderRadius: 6,
                padding: '4px 10px',
                cursor: 'pointer',
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                fontSize: '0.75rem',
                color: 'var(--cp-text-muted)',
            }}
        >
            <Languages size={14} />
            {currentLang === 'zh' ? 'EN' : '中文'}
        </button>
    )
}

// ============ Login Page ============
export default function Login() {
    const navigate = useNavigate()
    const { t } = useTranslation()
    const { needSetup, loading, login, setup } = useAuthStore()
    const [username, setUsername] = useState('')
    const [password, setPassword] = useState('')
    const [error, setError] = useState('')
    const [submitting, setSubmitting] = useState(false)
    const [altchaPayload, setAltchaPayload] = useState('')
    const [captchaKey, setCaptchaKey] = useState(0)

    // 2FA state
    const [requires2FA, setRequires2FA] = useState(false)
    const [tempToken, setTempToken] = useState('')
    const [totpCode, setTotpCode] = useState('')
    const [useRecovery, setUseRecovery] = useState(false)

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
                navigate('/', { replace: true })
            } else if (requires2FA) {
                // 2FA verification step
                const result = await login(username, password, altchaPayload, totpCode, tempToken)
                if (!result.requires_2fa) {
                    navigate('/', { replace: true })
                }
            } else {
                if (!altchaPayload) {
                    setError(t('login.verify_first'))
                    setSubmitting(false)
                    return
                }
                const result = await login(username, password, altchaPayload)
                if (result.requires_2fa) {
                    setRequires2FA(true)
                    setTempToken(result.temp_token)
                } else {
                    navigate('/', { replace: true })
                }
            }
        } catch (err) {
            const msg = err.response?.data?.error || t('error.connection_failed')
            setError(msg)
            if (requires2FA) {
                // Clear TOTP input on failure, allow retry
                setTotpCode('')
            } else {
                // Reset captcha on login error
                setAltchaPayload('')
                setCaptchaKey(k => k + 1)
            }
        } finally {
            setSubmitting(false)
        }
    }

    const handleBack = () => {
        setRequires2FA(false)
        setTempToken('')
        setTotpCode('')
        setUseRecovery(false)
        setError('')
        setAltchaPayload('')
        setCaptchaKey(k => k + 1)
    }

    if (loading) {
        return (
            <Flex align="center" justify="center" style={{ minHeight: '100vh' }}>
                <Text size="3" color="gray">{t('common.loading')}</Text>
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
                        WebCasa
                    </Heading>
                    <Text size="2" color="gray">
                        {t('login.subtitle')}
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
                            {requires2FA ? (
                                <>
                                    {/* 2FA Verification UI */}
                                    <Flex align="center" gap="2" justify="center">
                                        <ShieldCheck size={20} color="var(--accent-9)" />
                                        <Heading size="4">{t('twofa.verify_title')}</Heading>
                                    </Flex>

                                    <Text size="2" color="gray" align="center">
                                        {useRecovery ? t('twofa.recovery_hint') : t('twofa.verify_hint')}
                                    </Text>

                                    {error && (
                                        <Callout.Root color="red" size="1">
                                            <Callout.Icon><AlertCircle size={16} /></Callout.Icon>
                                            <Callout.Text>{error}</Callout.Text>
                                        </Callout.Root>
                                    )}

                                    <Flex direction="column" gap="1">
                                        <Text as="label" size="2" weight="medium" htmlFor="totp-code">
                                            {useRecovery ? t('twofa.recovery_code') : t('twofa.totp_code')}
                                        </Text>
                                        <TextField.Root
                                            id="totp-code"
                                            placeholder={useRecovery ? t('twofa.recovery_placeholder') : t('twofa.totp_placeholder')}
                                            value={totpCode}
                                            onChange={(e) => {
                                                const val = e.target.value
                                                if (useRecovery) {
                                                    // 8-char alphanumeric
                                                    setTotpCode(val.replace(/[^a-zA-Z0-9]/g, '').slice(0, 8))
                                                } else {
                                                    // 6-digit numeric
                                                    setTotpCode(val.replace(/\D/g, '').slice(0, 6))
                                                }
                                            }}
                                            required
                                            autoFocus
                                            size="3"
                                            maxLength={useRecovery ? 8 : 6}
                                            style={{ textAlign: 'center', letterSpacing: '0.2em', fontFamily: 'monospace' }}
                                        />
                                    </Flex>

                                    <Button
                                        type="submit"
                                        size="3"
                                        disabled={submitting || !totpCode || (!useRecovery && totpCode.length < 6) || (useRecovery && totpCode.length < 8)}
                                        style={{ cursor: 'pointer' }}
                                    >
                                        {submitting ? t('twofa.verifying') : t('twofa.verify_button')}
                                    </Button>

                                    <Flex justify="between" align="center">
                                        <Text
                                            size="1"
                                            color="gray"
                                            style={{ cursor: 'pointer', textDecoration: 'underline' }}
                                            onClick={handleBack}
                                        >
                                            {t('common.back')}
                                        </Text>
                                        <Text
                                            size="1"
                                            color="gray"
                                            style={{ cursor: 'pointer', textDecoration: 'underline' }}
                                            onClick={() => {
                                                setUseRecovery(!useRecovery)
                                                setTotpCode('')
                                                setError('')
                                            }}
                                        >
                                            {useRecovery ? t('twofa.use_totp') : t('twofa.use_recovery')}
                                        </Text>
                                    </Flex>
                                </>
                            ) : (
                                <>
                                    {/* Normal Login UI */}
                                    <Heading size="4" align="center">
                                        {needSetup ? t('login.setup_title') : t('login.title')}
                                    </Heading>

                                    {needSetup && (
                                        <Callout.Root color="blue" size="1">
                                            <Callout.Icon>
                                                <AlertCircle size={16} />
                                            </Callout.Icon>
                                            <Callout.Text>
                                                {t('login.setup_hint')}
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
                                            {t('login.username')}
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
                                            {t('login.password')}
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
                                            ? t('login.please_wait')
                                            : needSetup
                                                ? t('login.create_account')
                                                : t('login.sign_in')}
                                    </Button>
                                </>
                            )}
                        </Flex>
                    </form>
                </Card>

                <Flex justify="center" align="center" gap="3" mt="4">
                    <Text size="1" color="gray">
                        {t('login.footer')}
                    </Text>
                    <LanguageSwitcher />
                </Flex>
            </Box>
        </Flex>
    )
}
