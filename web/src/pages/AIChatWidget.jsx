import { useState, useEffect, useRef, useCallback } from 'react'
import { Box, Flex, Text, Button, TextField, Badge, Separator } from '@radix-ui/themes'
import { Bot, X, Send, Plus, Trash2, MessageSquare, Sparkles, Loader2, SquareTerminal, Minus, Maximize2, Minimize2 } from 'lucide-react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { aiAPI, fileManagerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

// ============ Inline Terminal Tab ============
function InlineTerminalTab({ active, wsUrl, tabId }) {
    const containerRef = useRef(null)
    const termRef = useRef(null)
    const fitRef = useRef(null)
    const wsRef = useRef(null)

    useEffect(() => {
        if (!containerRef.current) return

        const term = new Terminal({
            cursorBlink: true,
            fontSize: 13,
            fontFamily: '"JetBrains Mono", "Fira Code", "Cascadia Code", Menlo, monospace',
            theme: {
                background: '#1e1e2e',
                foreground: '#cdd6f4',
                cursor: '#f5e0dc',
                selectionBackground: '#585b7066',
                black: '#45475a',
                red: '#f38ba8',
                green: '#a6e3a1',
                yellow: '#f9e2af',
                blue: '#89b4fa',
                magenta: '#f5c2e7',
                cyan: '#94e2d5',
                white: '#bac2de',
                brightBlack: '#585b70',
                brightRed: '#f38ba8',
                brightGreen: '#a6e3a1',
                brightYellow: '#f9e2af',
                brightBlue: '#89b4fa',
                brightMagenta: '#f5c2e7',
                brightCyan: '#94e2d5',
                brightWhite: '#a6adc8',
            },
        })

        const fitAddon = new FitAddon()
        const webLinksAddon = new WebLinksAddon()
        term.loadAddon(fitAddon)
        term.loadAddon(webLinksAddon)
        term.open(containerRef.current)

        termRef.current = term
        fitRef.current = fitAddon

        requestAnimationFrame(() => { fitAddon.fit() })

        const cols = term.cols
        const rows = term.rows
        const url = wsUrl(cols, rows)
        const ws = new WebSocket(url)
        wsRef.current = ws

        ws.binaryType = 'arraybuffer'
        ws.onopen = () => { term.focus() }
        ws.onmessage = (e) => {
            if (e.data instanceof ArrayBuffer) {
                term.write(new Uint8Array(e.data))
            } else {
                term.write(e.data)
            }
        }
        ws.onclose = () => { term.write('\r\n\x1b[31m[Connection closed]\x1b[0m\r\n') }
        ws.onerror = () => { term.write('\r\n\x1b[31m[Connection error]\x1b[0m\r\n') }

        term.onData((data) => {
            if (ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({ type: 'data', data }))
            }
        })

        term.onResize(({ cols, rows }) => {
            if (ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({ type: 'resize', cols, rows }))
            }
        })

        const resizeObserver = new ResizeObserver(() => {
            requestAnimationFrame(() => {
                if (fitRef.current) {
                    try { fitRef.current.fit() } catch {}
                }
            })
        })
        resizeObserver.observe(containerRef.current)

        return () => {
            resizeObserver.disconnect()
            ws.close()
            term.dispose()
        }
    }, [])

    useEffect(() => {
        if (active && fitRef.current) {
            requestAnimationFrame(() => {
                try { fitRef.current.fit() } catch {}
            })
            termRef.current?.focus()
        }
    }, [active])

    return (
        <Box
            ref={containerRef}
            style={{
                display: active ? 'block' : 'none',
                width: '100%',
                height: '100%',
                background: '#1e1e2e',
            }}
        />
    )
}

// ============ Bottom Terminal Panel ============
let inlineTabId = 1

function BottomTerminalPanel({ open, onClose }) {
    const { t } = useTranslation()
    const [tabs, setTabs] = useState(() => [{ id: inlineTabId++, label: 'Terminal 1' }])
    const [activeTab, setActiveTab] = useState(1)
    const [height, setHeight] = useState(300)
    const draggingRef = useRef(false)
    const startYRef = useRef(0)
    const startHeightRef = useRef(0)

    const addTab = () => {
        const id = inlineTabId++
        setTabs(prev => [...prev, { id, label: `Terminal ${id}` }])
        setActiveTab(id)
    }

    const closeTab = (id) => {
        setTabs(prev => {
            const next = prev.filter(t => t.id !== id)
            if (next.length === 0) {
                onClose()
                return prev
            }
            if (activeTab === id) {
                setActiveTab(next[next.length - 1].id)
            }
            return next
        })
    }

    // Drag to resize
    const onDragStart = useCallback((e) => {
        e.preventDefault()
        draggingRef.current = true
        startYRef.current = e.clientY
        startHeightRef.current = height

        const onMouseMove = (e) => {
            if (!draggingRef.current) return
            const diff = startYRef.current - e.clientY
            const newH = Math.max(150, Math.min(window.innerHeight * 0.8, startHeightRef.current + diff))
            setHeight(newH)
        }
        const onMouseUp = () => {
            draggingRef.current = false
            document.removeEventListener('mousemove', onMouseMove)
            document.removeEventListener('mouseup', onMouseUp)
        }
        document.addEventListener('mousemove', onMouseMove)
        document.addEventListener('mouseup', onMouseUp)
    }, [height])

    if (!open) return null

    return (
        <Box
            style={{
                position: 'fixed',
                bottom: 0,
                left: 0,
                right: 0,
                height,
                zIndex: 999,
                display: 'flex',
                flexDirection: 'column',
                boxShadow: '0 -4px 24px rgba(0,0,0,0.25)',
            }}
        >
            {/* Drag handle */}
            <Box
                onMouseDown={onDragStart}
                style={{
                    height: 4,
                    cursor: 'ns-resize',
                    background: '#313244',
                    flexShrink: 0,
                }}
            />

            {/* Tab bar */}
            <Flex
                align="center"
                justify="between"
                px="2"
                style={{
                    background: '#181825',
                    borderBottom: '1px solid #313244',
                    minHeight: 36,
                    flexShrink: 0,
                }}
            >
                <Flex align="center" gap="1" style={{ overflow: 'hidden', flex: 1 }}>
                    <SquareTerminal size={14} style={{ color: '#94e2d5', marginRight: 4, flexShrink: 0 }} />
                    {tabs.map(tab => (
                        <Flex
                            key={tab.id}
                            align="center"
                            gap="1"
                            px="2"
                            py="1"
                            style={{
                                cursor: 'pointer',
                                borderRadius: '4px 4px 0 0',
                                background: activeTab === tab.id ? '#1e1e2e' : 'transparent',
                                color: activeTab === tab.id ? '#cdd6f4' : '#6c7086',
                                fontSize: 12,
                                userSelect: 'none',
                                flexShrink: 0,
                            }}
                            onClick={() => setActiveTab(tab.id)}
                        >
                            <span>{tab.label}</span>
                            <button
                                style={{
                                    background: 'none', border: 'none', cursor: 'pointer',
                                    color: 'inherit', padding: 0, display: 'flex',
                                    alignItems: 'center', opacity: 0.6,
                                }}
                                onClick={(e) => { e.stopPropagation(); closeTab(tab.id) }}
                            >
                                <X size={11} />
                            </button>
                        </Flex>
                    ))}
                    <button
                        style={{
                            background: 'none', border: 'none', cursor: 'pointer',
                            color: '#94e2d5', padding: '2px 6px', display: 'flex',
                            alignItems: 'center',
                        }}
                        onClick={addTab}
                        title={t('terminal.new_tab')}
                    >
                        <Plus size={13} />
                    </button>
                </Flex>
                <Flex gap="1" style={{ flexShrink: 0 }}>
                    <button
                        style={{
                            background: 'none', border: 'none', cursor: 'pointer',
                            color: '#6c7086', padding: '2px 4px', display: 'flex',
                            alignItems: 'center',
                        }}
                        onClick={() => setHeight(h => h === 300 ? Math.floor(window.innerHeight * 0.6) : 300)}
                        title={height > 300 ? t('terminal.restore') : t('terminal.expand')}
                    >
                        {height > 300 ? <Minimize2 size={13} /> : <Maximize2 size={13} />}
                    </button>
                    <button
                        style={{
                            background: 'none', border: 'none', cursor: 'pointer',
                            color: '#6c7086', padding: '2px 4px', display: 'flex',
                            alignItems: 'center',
                        }}
                        onClick={onClose}
                        title={t('common.close')}
                    >
                        <X size={14} />
                    </button>
                </Flex>
            </Flex>

            {/* Terminal area */}
            <Box style={{ flex: 1, position: 'relative', overflow: 'hidden', background: '#1e1e2e' }}>
                {tabs.map(tab => (
                    <InlineTerminalTab
                        key={tab.id}
                        tabId={tab.id}
                        active={activeTab === tab.id}
                        wsUrl={(cols, rows) => fileManagerAPI.terminalWsUrl(cols, rows)}
                    />
                ))}
            </Box>
        </Box>
    )
}

// ============ AI Chat Widget + Floating Buttons ============
export default function AIChatWidget() {
    const { t } = useTranslation()
    const [aiOpen, setAiOpen] = useState(false)
    const [termOpen, setTermOpen] = useState(false)
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

    useEffect(() => {
        if (aiOpen && configured === null) {
            aiAPI.getConfig().then(res => {
                const cfg = res.data
                setConfigured(!!(cfg.base_url && cfg.model && cfg.api_key !== '****' && cfg.api_key !== ''))
            }).catch(() => setConfigured(false))
        }
    }, [aiOpen, configured])

    useEffect(() => {
        if (aiOpen && configured) {
            aiAPI.listConversations().then(res => {
                setConversations(res.data?.conversations || [])
            }).catch(() => {})
        }
    }, [aiOpen, configured])

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

        const userMsg = { role: 'user', content: msg, id: Date.now() }
        setMessages(prev => [...prev, userMsg])

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
            let buffer = ''
            let currentEvent = ''

            while (true) {
                const { done, value } = await reader.read()
                if (done) break

                buffer += decoder.decode(value, { stream: true })
                const lines = buffer.split('\n')
                buffer = lines.pop() || ''

                for (const line of lines) {
                    if (line.startsWith('event: ')) {
                        currentEvent = line.slice(7).trim()
                        continue
                    }
                    if (line.startsWith('data: ')) {
                        const data = line.slice(6)
                        if (currentEvent === 'done') {
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
                        fullContent += data
                        setMessages(prev => {
                            const updated = [...prev]
                            updated[updated.length - 1] = { ...updated[updated.length - 1], content: fullContent }
                            return updated
                        })
                        currentEvent = ''
                    }
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

    return (
        <>
            {/* Bottom Terminal Panel */}
            <BottomTerminalPanel open={termOpen} onClose={() => setTermOpen(false)} />

            {/* Floating buttons (bottom-right) */}
            {!aiOpen && (
                <Flex
                    direction="column"
                    gap="3"
                    style={{
                        position: 'fixed',
                        bottom: termOpen ? 'calc(300px + 16px)' : 24,
                        right: 24,
                        zIndex: 1000,
                        transition: 'bottom 0.3s ease',
                    }}
                >
                    {/* Terminal toggle button */}
                    <button
                        onClick={() => setTermOpen(prev => !prev)}
                        style={{
                            width: 44,
                            height: 44,
                            borderRadius: '50%',
                            background: termOpen ? '#94e2d5' : '#313244',
                            color: termOpen ? '#1e1e2e' : '#cdd6f4',
                            border: 'none',
                            cursor: 'pointer',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            boxShadow: '0 4px 16px rgba(0,0,0,0.2)',
                            transition: 'transform 0.2s, background 0.2s',
                        }}
                        onMouseEnter={e => e.currentTarget.style.transform = 'scale(1.1)'}
                        onMouseLeave={e => e.currentTarget.style.transform = 'scale(1)'}
                        title={t('nav.terminal')}
                    >
                        <SquareTerminal size={20} />
                    </button>

                    {/* AI chat button */}
                    <button
                        onClick={() => setAiOpen(true)}
                        style={{
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
                            transition: 'transform 0.2s',
                        }}
                        onMouseEnter={e => e.currentTarget.style.transform = 'scale(1.1)'}
                        onMouseLeave={e => e.currentTarget.style.transform = 'scale(1)'}
                        title={t('ai.title')}
                    >
                        <Bot size={24} />
                    </button>
                </Flex>
            )}

            {/* AI Chat panel */}
            {aiOpen && (
                <Box
                    style={{
                        position: 'fixed',
                        bottom: termOpen ? 'calc(300px + 16px)' : 24,
                        right: 24,
                        width: 400,
                        maxWidth: 'calc(100vw - 48px)',
                        height: 560,
                        maxHeight: termOpen ? 'calc(100vh - 400px)' : 'calc(100vh - 100px)',
                        borderRadius: 16,
                        background: 'var(--color-background)',
                        border: '1px solid var(--gray-5)',
                        boxShadow: '0 8px 32px rgba(0,0,0,0.15)',
                        zIndex: 1000,
                        display: 'flex',
                        flexDirection: 'column',
                        overflow: 'hidden',
                        transition: 'bottom 0.3s ease, max-height 0.3s ease',
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
                            <Button variant="ghost" size="1" onClick={() => setAiOpen(false)}>
                                <X size={16} />
                            </Button>
                        </Flex>
                    </Flex>

                    {!configured ? (
                        <Flex direction="column" align="center" justify="center" style={{ flex: 1 }} gap="3" p="4">
                            <Sparkles size={48} style={{ opacity: 0.3 }} />
                            <Text size="2" color="gray" align="center">{t('ai.not_configured')}</Text>
                            <Button variant="soft" size="2" onClick={() => { setAiOpen(false); window.location.href = '/settings' }}>
                                {t('ai.go_settings')}
                            </Button>
                        </Flex>
                    ) : (
                        <Flex style={{ flex: 1, overflow: 'hidden' }}>
                            {/* Conversation list */}
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
            )}
        </>
    )
}
