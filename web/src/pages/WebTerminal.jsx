import { useState, useEffect, useRef, useCallback } from 'react'
import { Box, Flex, Text, Button } from '@radix-ui/themes'
import { Plus, X, SquareTerminal } from 'lucide-react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { fileManagerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

function TerminalTab({ active, wsUrl, onClose, tabId }) {
    const containerRef = useRef(null)
    const termRef = useRef(null)
    const fitRef = useRef(null)
    const wsRef = useRef(null)

    useEffect(() => {
        if (!containerRef.current) return

        const term = new Terminal({
            cursorBlink: true,
            fontSize: 14,
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

        // Fit after open
        requestAnimationFrame(() => {
            fitAddon.fit()
        })

        // Connect WebSocket
        const cols = term.cols
        const rows = term.rows
        const url = wsUrl(cols, rows)
        const ws = new WebSocket(url)
        wsRef.current = ws

        ws.binaryType = 'arraybuffer'

        ws.onopen = () => {
            term.focus()
        }

        ws.onmessage = (e) => {
            if (e.data instanceof ArrayBuffer) {
                term.write(new Uint8Array(e.data))
            } else {
                term.write(e.data)
            }
        }

        ws.onclose = () => {
            term.write('\r\n\x1b[31m[Connection closed]\x1b[0m\r\n')
        }

        ws.onerror = () => {
            term.write('\r\n\x1b[31m[Connection error]\x1b[0m\r\n')
        }

        // Terminal → WebSocket
        term.onData((data) => {
            if (ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({ type: 'data', data }))
            }
        })

        // Resize handling
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

    // Refit when becoming active
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

let nextTabId = 1

export default function WebTerminal() {
    const { t } = useTranslation()
    const [tabs, setTabs] = useState(() => [{ id: nextTabId++, label: 'Terminal 1' }])
    const [activeTab, setActiveTab] = useState(1)

    const addTab = () => {
        const id = nextTabId++
        setTabs(prev => [...prev, { id, label: `Terminal ${id}` }])
        setActiveTab(id)
    }

    const closeTab = (id) => {
        setTabs(prev => {
            const next = prev.filter(t => t.id !== id)
            if (next.length === 0) return prev // Keep at least one tab
            if (activeTab === id) {
                setActiveTab(next[next.length - 1].id)
            }
            return next
        })
    }

    return (
        <Box style={{ height: 'calc(100vh - 140px)', display: 'flex', flexDirection: 'column' }}>
            {/* Tab bar */}
            <Flex
                align="center"
                gap="1"
                px="2"
                style={{
                    background: '#181825',
                    borderRadius: '8px 8px 0 0',
                    borderBottom: '1px solid #313244',
                    minHeight: 40,
                    flexShrink: 0,
                }}
            >
                <SquareTerminal size={16} style={{ color: '#94e2d5', marginRight: 4 }} />
                {tabs.map(tab => (
                    <Flex
                        key={tab.id}
                        align="center"
                        gap="1"
                        px="3"
                        py="1"
                        style={{
                            cursor: 'pointer',
                            borderRadius: '6px 6px 0 0',
                            background: activeTab === tab.id ? '#1e1e2e' : 'transparent',
                            color: activeTab === tab.id ? '#cdd6f4' : '#6c7086',
                            fontSize: 13,
                            userSelect: 'none',
                        }}
                        onClick={() => setActiveTab(tab.id)}
                    >
                        <span>{tab.label}</span>
                        {tabs.length > 1 && (
                            <Button
                                size="1"
                                variant="ghost"
                                style={{ padding: 0, minWidth: 18, height: 18, color: 'inherit' }}
                                onClick={(e) => { e.stopPropagation(); closeTab(tab.id) }}
                            >
                                <X size={12} />
                            </Button>
                        )}
                    </Flex>
                ))}
                <Button
                    size="1"
                    variant="ghost"
                    style={{ color: '#94e2d5', minWidth: 28, height: 28 }}
                    onClick={addTab}
                >
                    <Plus size={14} />
                </Button>
            </Flex>

            {/* Terminal area */}
            <Box style={{ flex: 1, position: 'relative', borderRadius: '0 0 8px 8px', overflow: 'hidden' }}>
                {tabs.map(tab => (
                    <TerminalTab
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
