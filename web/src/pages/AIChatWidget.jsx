import { useState, useEffect, useRef, useCallback } from 'react'
import { Box, Flex, Text, Button, TextField, Dialog, Badge, Separator, ScrollArea } from '@radix-ui/themes'
import { Bot, X, Send, Plus, Trash2, MessageSquare, Sparkles, Loader2 } from 'lucide-react'
import { aiAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

export default function AIChatWidget() {
    const { t } = useTranslation()
    const [open, setOpen] = useState(false)
    const [conversations, setConversations] = useState([])
    const [currentConv, setCurrentConv] = useState(null)
    const [messages, setMessages] = useState([])
    const [input, setInput] = useState('')
    const [streaming, setStreaming] = useState(false)
    const [configured, setConfigured] = useState(null)
    const messagesEndRef = useRef(null)
    const abortRef = useRef(null)

    const scrollToBottom = useCallback(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    }, [])

    useEffect(() => { scrollToBottom() }, [messages, scrollToBottom])

    // Check if AI is configured when widget opens
    useEffect(() => {
        if (open && configured === null) {
            aiAPI.getConfig().then(res => {
                const cfg = res.data
                setConfigured(!!(cfg.base_url && cfg.model && cfg.api_key !== '****' && cfg.api_key !== ''))
            }).catch(() => setConfigured(false))
        }
    }, [open, configured])

    // Load conversations when widget opens
    useEffect(() => {
        if (open && configured) {
            aiAPI.listConversations().then(res => {
                setConversations(res.data?.conversations || [])
            }).catch(() => {})
        }
    }, [open, configured])

    const loadConversation = async (id) => {
        try {
            const res = await aiAPI.getConversation(id)
            setCurrentConv(res.data)
            setMessages(res.data?.messages || [])
        } catch { /* ignore */ }
    }

    const newConversation = () => {
        setCurrentConv(null)
        setMessages([])
        setInput('')
    }

    const deleteConversation = async (id, e) => {
        e.stopPropagation()
        try {
            await aiAPI.deleteConversation(id)
            setConversations(prev => prev.filter(c => c.id !== id))
            if (currentConv?.id === id) newConversation()
        } catch { /* ignore */ }
    }

    const sendMessage = async () => {
        const msg = input.trim()
        if (!msg || streaming) return
        setInput('')

        // Add user message to UI immediately
        const userMsg = { role: 'user', content: msg, id: Date.now() }
        setMessages(prev => [...prev, userMsg])

        // Add placeholder for assistant
        const assistantMsg = { role: 'assistant', content: '', id: Date.now() + 1 }
        setMessages(prev => [...prev, assistantMsg])

        setStreaming(true)

        try {
            const controller = new AbortController()
            abortRef.current = controller

            const response = await fetch('/api/plugins/ai/chat', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': `Bearer ${localStorage.getItem('token')}`,
                },
                body: JSON.stringify({
                    conversation_id: currentConv?.id || 0,
                    message: msg,
                }),
                signal: controller.signal,
            })

            const reader = response.body.getReader()
            const decoder = new TextDecoder()
            let fullContent = ''
            let buffer = ''        // Cross-chunk line buffer
            let currentEvent = ''  // Track current SSE event name

            while (true) {
                const { done, value } = await reader.read()
                if (done) break

                buffer += decoder.decode(value, { stream: true })
                const lines = buffer.split('\n')
                // Keep the last incomplete line in buffer
                buffer = lines.pop() || ''

                for (const line of lines) {
                    if (line.startsWith('event: ')) {
                        currentEvent = line.slice(7).trim()
                        continue
                    }
                    if (line.startsWith('data: ')) {
                        const data = line.slice(6)
                        if (currentEvent === 'done') {
                            // This is the conversation ID
                            const convId = parseInt(data)
                            if (convId > 0 && !currentConv) {
                                setCurrentConv({ id: convId })
                                aiAPI.listConversations().then(res => {
                                    setConversations(res.data?.conversations || [])
                                }).catch(() => {})
                            }
                            currentEvent = ''
                            continue
                        }
                        if (currentEvent === 'error') {
                            fullContent += data
                            currentEvent = ''
                            continue
                        }
                        // Default: content delta
                        fullContent += data
                        setMessages(prev => {
                            const updated = [...prev]
                            updated[updated.length - 1] = { ...updated[updated.length - 1], content: fullContent }
                            return updated
                        })
                        currentEvent = ''
                    }
                    // Empty line resets event state per SSE spec
                    if (line === '') {
                        currentEvent = ''
                    }
                }
            }
        } catch (e) {
            if (e.name !== 'AbortError') {
                setMessages(prev => {
                    const updated = [...prev]
                    updated[updated.length - 1] = { ...updated[updated.length - 1], content: t('ai.error_response') }
                    return updated
                })
            }
        } finally {
            setStreaming(false)
            abortRef.current = null
        }
    }

    const handleKeyDown = (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault()
            sendMessage()
        }
    }

    // Floating button
    if (!open) {
        return (
            <button
                onClick={() => setOpen(true)}
                style={{
                    position: 'fixed',
                    bottom: 24,
                    right: 24,
                    width: 52,
                    height: 52,
                    borderRadius: '50%',
                    background: 'var(--accent-9)',
                    color: 'white',
                    border: 'none',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    boxShadow: '0 4px 16px rgba(0,0,0,0.2)',
                    zIndex: 1000,
                    transition: 'transform 0.2s',
                }}
                onMouseEnter={e => e.currentTarget.style.transform = 'scale(1.1)'}
                onMouseLeave={e => e.currentTarget.style.transform = 'scale(1)'}
                title={t('ai.title')}
            >
                <Bot size={24} />
            </button>
        )
    }

    // Chat panel
    return (
        <Box
            style={{
                position: 'fixed',
                bottom: 24,
                right: 24,
                width: 400,
                maxWidth: 'calc(100vw - 48px)',
                height: 560,
                maxHeight: 'calc(100vh - 100px)',
                borderRadius: 16,
                background: 'var(--color-background)',
                border: '1px solid var(--gray-5)',
                boxShadow: '0 8px 32px rgba(0,0,0,0.15)',
                zIndex: 1000,
                display: 'flex',
                flexDirection: 'column',
                overflow: 'hidden',
            }}
        >
            {/* Header */}
            <Flex align="center" justify="between" p="3" style={{ borderBottom: '1px solid var(--gray-4)', flexShrink: 0 }}>
                <Flex align="center" gap="2">
                    <Bot size={20} />
                    <Text weight="bold" size="3">{t('ai.title')}</Text>
                    {streaming && <Badge size="1" color="green"><Loader2 size={10} className="spin" /> {t('ai.thinking')}</Badge>}
                </Flex>
                <Flex gap="1">
                    <Button variant="ghost" size="1" onClick={newConversation} title={t('ai.new_chat')}>
                        <Plus size={16} />
                    </Button>
                    <Button variant="ghost" size="1" onClick={() => setOpen(false)}>
                        <X size={16} />
                    </Button>
                </Flex>
            </Flex>

            {!configured ? (
                <Flex direction="column" align="center" justify="center" style={{ flex: 1 }} gap="3" p="4">
                    <Sparkles size={48} style={{ opacity: 0.3 }} />
                    <Text size="2" color="gray" align="center">{t('ai.not_configured')}</Text>
                    <Button variant="soft" size="2" onClick={() => { setOpen(false); window.location.href = '/ai/config' }}>
                        {t('ai.go_settings')}
                    </Button>
                </Flex>
            ) : (
                <Flex style={{ flex: 1, overflow: 'hidden' }}>
                    {/* Conversation list (sidebar) */}
                    {messages.length === 0 && !currentConv && (
                        <Box style={{ flex: 1, overflow: 'auto' }} p="3">
                            <Text size="2" weight="medium" mb="2" style={{ display: 'block' }}>{t('ai.recent_conversations')}</Text>
                            {conversations.length === 0 ? (
                                <Flex direction="column" align="center" justify="center" style={{ minHeight: 200 }} gap="2">
                                    <MessageSquare size={32} style={{ opacity: 0.2 }} />
                                    <Text size="2" color="gray">{t('ai.no_conversations')}</Text>
                                </Flex>
                            ) : (
                                <Flex direction="column" gap="1">
                                    {conversations.map(c => (
                                        <Flex
                                            key={c.id}
                                            align="center"
                                            justify="between"
                                            p="2"
                                            style={{
                                                borderRadius: 8,
                                                cursor: 'pointer',
                                                background: 'var(--gray-2)',
                                            }}
                                            onClick={() => loadConversation(c.id)}
                                        >
                                            <Box style={{ flex: 1, minWidth: 0 }}>
                                                <Text size="2" weight="medium" style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                                    {c.title}
                                                </Text>
                                                <Text size="1" color="gray">{new Date(c.updated_at).toLocaleDateString()}</Text>
                                            </Box>
                                            <Button variant="ghost" size="1" color="red" onClick={(e) => deleteConversation(c.id, e)}>
                                                <Trash2 size={12} />
                                            </Button>
                                        </Flex>
                                    ))}
                                </Flex>
                            )}

                            <Separator my="3" size="4" />
                            <Text size="2" color="gray" align="center" style={{ display: 'block' }}>
                                {t('ai.start_hint')}
                            </Text>
                        </Box>
                    )}

                    {/* Messages area */}
                    {(messages.length > 0 || currentConv) && (
                        <Flex direction="column" style={{ flex: 1, overflow: 'hidden' }}>
                            <Box style={{ flex: 1, overflow: 'auto', padding: 12 }}>
                                {messages.map((msg, i) => (
                                    <Flex
                                        key={msg.id || i}
                                        justify={msg.role === 'user' ? 'end' : 'start'}
                                        mb="2"
                                    >
                                        <Box
                                            style={{
                                                maxWidth: '85%',
                                                padding: '8px 12px',
                                                borderRadius: msg.role === 'user' ? '12px 12px 2px 12px' : '12px 12px 12px 2px',
                                                background: msg.role === 'user' ? 'var(--accent-9)' : 'var(--gray-3)',
                                                color: msg.role === 'user' ? 'white' : 'var(--gray-12)',
                                                fontSize: '0.875rem',
                                                lineHeight: 1.5,
                                                whiteSpace: 'pre-wrap',
                                                wordBreak: 'break-word',
                                            }}
                                        >
                                            {msg.content || (streaming && i === messages.length - 1 ? '...' : '')}
                                        </Box>
                                    </Flex>
                                ))}
                                <div ref={messagesEndRef} />
                            </Box>

                            {/* Input */}
                            <Box p="2" style={{ borderTop: '1px solid var(--gray-4)', flexShrink: 0 }}>
                                <Flex gap="2">
                                    <TextField.Root
                                        style={{ flex: 1 }}
                                        placeholder={t('ai.input_placeholder')}
                                        value={input}
                                        onChange={(e) => setInput(e.target.value)}
                                        onKeyDown={handleKeyDown}
                                        disabled={streaming}
                                    />
                                    <Button size="2" disabled={streaming || !input.trim()} onClick={sendMessage}>
                                        <Send size={14} />
                                    </Button>
                                </Flex>
                            </Box>
                        </Flex>
                    )}

                    {/* Input when on conversation list */}
                    {messages.length === 0 && !currentConv && (
                        <Box style={{ position: 'absolute', bottom: 0, left: 0, right: 0, padding: 8, borderTop: '1px solid var(--gray-4)', background: 'var(--color-background)' }}>
                            <Flex gap="2">
                                <TextField.Root
                                    style={{ flex: 1 }}
                                    placeholder={t('ai.input_placeholder')}
                                    value={input}
                                    onChange={(e) => setInput(e.target.value)}
                                    onKeyDown={handleKeyDown}
                                    disabled={streaming}
                                />
                                <Button size="2" disabled={streaming || !input.trim()} onClick={sendMessage}>
                                    <Send size={14} />
                                </Button>
                            </Flex>
                        </Box>
                    )}
                </Flex>
            )}
        </Box>
    )
}
