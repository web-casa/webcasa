import React from 'react'
import { createRoot } from 'react-dom/client'
import { Theme } from '@radix-ui/themes'
import App from './App.jsx'
import './index.css'

createRoot(document.getElementById('root')).render(
    <React.StrictMode>
        <Theme appearance="dark" accentColor="green" grayColor="sage" radius="medium" scaling="100%">
            <App />
        </Theme>
    </React.StrictMode>
)
