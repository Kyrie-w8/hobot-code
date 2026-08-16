import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'
import {applyTheme, readThemePreference} from './appearance-theme.js'

const systemPrefersDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true
applyTheme(document.documentElement, readThemePreference(window.localStorage), systemPrefersDark)

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
)
