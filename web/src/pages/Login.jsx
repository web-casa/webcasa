import { useState, useEffect, useRef, useCallback } from 'react'
import { useNavigate } from 'react-router'
import { Box, Card, Flex, Heading, Text, TextField, Button, Callout } from '@radix-ui/themes'
import { Zap, AlertCircle, Check, MoveRight } from 'lucide-react'
import { useAuthStore } from '../stores/auth.js'
import { authAPI } from '../api/index.js'

// ============ Slider Captcha Component ============
function SliderCaptcha({ onVerified, onReset }) {
    const [target, setTarget] = useState(50)
    const [token, setToken] = useState('')
    const [value, setValue] = useState(0)
    const [dragging, setDragging] = useState(false)
    const [verified, setVerified] = useState(false)
    const [loading, setLoading] = useState(true)
    const trackRef = useRef(null)
    const thumbWidth = 36

    const fetchChallenge = useCallback(async () => {
        setLoading(true)
        setVerified(false)
        setValue(0)
        onReset?.()
        try {
            const res = await authAPI.challenge()
            setTarget(res.data.target)
            setToken(res.data.token)
        } catch {
            setTarget(50)
        }
        setLoading(false)
    }, [onReset])

    useEffect(() => { fetchChallenge() }, [fetchChallenge])

    // Convert clientX to 0-100 value based on track
    const getPosition = (clientX) => {
        if (!trackRef.current) return 0
        const rect = trackRef.current.getBoundingClientRect()
        const maxTravel = rect.width - thumbWidth
        const x = clientX - rect.left - thumbWidth / 2
        const clamped = Math.max(0, Math.min(x, maxTravel))
        return Math.round((clamped / maxTravel) * 100)
    }

    // Convert 0-100 value to pixel left offset for thumb and target
    const toPixelLeft = (pct) => {
        if (!trackRef.current) return 0
        const maxTravel = trackRef.current.getBoundingClientRect().width - thumbWidth
        return (pct / 100) * maxTravel
    }

    const handleStart = (clientX) => {
        if (verified || loading) return
        setDragging(true)
    }

    const handleMove = (clientX) => {
        if (!dragging || verified) return
        setValue(getPosition(clientX))
    }

    const handleEnd = () => {
        if (!dragging || verified) return
        setDragging(false)
        const diff = Math.abs(value - target)
        if (diff <= 5) {
            setVerified(true)
            onVerified?.(token, value)
        } else {
            // Reset on fail
            setValue(0)
            fetchChallenge()
        }
    }

    return (
        <div
            className={`slider-captcha${verified ? ' success' : ''}`}
            ref={trackRef}
            onMouseMove={(e) => handleMove(e.clientX)}
            onMouseUp={handleEnd}
            onMouseLeave={() => { if (dragging) { setDragging(false); setValue(0) } }}
            onTouchMove={(e) => handleMove(e.touches[0].clientX)}
            onTouchEnd={handleEnd}
        >
            <div className="slider-captcha-track">
                {loading ? '加载中...' : verified ? '✓ 验证通过' : '拖动滑块到标记位置'}
            </div>
            {!loading && !verified && (
                <div className="slider-captcha-target" style={{ left: toPixelLeft(target) + thumbWidth / 2 - 2 }} />
            )}
            <div className="slider-captcha-fill" style={{ width: toPixelLeft(value) + thumbWidth / 2 }} />
            <div
                className="slider-captcha-thumb"
                style={{ left: toPixelLeft(value) }}
                onMouseDown={(e) => { e.preventDefault(); handleStart(e.clientX) }}
                onTouchStart={(e) => handleStart(e.touches[0].clientX)}
            >
                {verified ? <Check size={16} /> : <MoveRight size={16} style={{ color: 'var(--cp-text-muted)' }} />}
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
    const [challengeToken, setChallengeToken] = useState('')
    const [sliderValue, setSliderValue] = useState(0)
    const [sliderVerified, setSliderVerified] = useState(false)

    const handleVerified = (token, value) => {
        setChallengeToken(token)
        setSliderValue(value)
        setSliderVerified(true)
    }

    const handleSliderReset = () => {
        setSliderVerified(false)
        setChallengeToken('')
        setSliderValue(0)
    }

    const handleSubmit = async (e) => {
        e.preventDefault()
        setError('')
        setSubmitting(true)

        try {
            if (needSetup) {
                await setup(username, password)
            } else {
                if (!sliderVerified) {
                    setError('请先完成滑块验证')
                    setSubmitting(false)
                    return
                }
                await login(username, password, challengeToken, sliderValue)
            }
            navigate('/', { replace: true })
        } catch (err) {
            const msg = err.response?.data?.error || 'Connection failed'
            setError(msg)
            // Reset slider on any error
            handleSliderReset()
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

                            {/* Slider captcha (not shown during initial setup) */}
                            {!needSetup && (
                                <Flex direction="column" gap="1">
                                    <Text size="1" color="gray">安全验证</Text>
                                    <SliderCaptcha
                                        onVerified={handleVerified}
                                        onReset={handleSliderReset}
                                    />
                                </Flex>
                            )}

                            <Button
                                type="submit"
                                size="3"
                                disabled={submitting || !username || !password || (!needSetup && !sliderVerified)}
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
