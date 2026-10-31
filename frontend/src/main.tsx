import React from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'
import App from './App'
import { I18nProvider } from './i18n'
import { initTheme } from './theme'
import { initFontPrefs } from './fontPrefs'

initTheme()
initFontPrefs()

const container = document.getElementById('root')
const root = createRoot(container!)

root.render(
  <React.StrictMode>
    <I18nProvider>
      <App />
    </I18nProvider>
  </React.StrictMode>
)
