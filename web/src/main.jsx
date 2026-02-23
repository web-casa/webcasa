import React from 'react'
import { createRoot } from 'react-dom/client'
import { Theme, Tooltip } from '@radix-ui/themes'
import App from './App.jsx'
import './index.css'
import { useThemeStore } from './stores/theme.js'

// Init theme on app load
useThemeStore.getState().init()

function Root() {
    const theme = useThemeStore((s) => s.theme)
    return (
        <React.StrictMode>
            <Theme
                appearance={theme}
                accentColor="green"
                grayColor="sage"
                radius="medium"
                scaling="100%"
            >
                <Tooltip.Provider delayDuration={200}>
                    <App />
                </Tooltip.Provider>
            </Theme>
        </React.StrictMode>
    )
}

createRoot(document.getElementById('root')).render(<Root />)
