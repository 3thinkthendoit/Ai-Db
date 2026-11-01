import React from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'
import App from './App'
import { I18nProvider } from './i18n'
import { initTheme } from './theme'
import { initFontPrefs } from './fontPrefs'
import { isDesktop } from './lib/api'
import { LoginView } from './pages/LoginView'

initTheme()
initFontPrefs()

const container = document.getElementById('root')
const root = createRoot(container!)

// Desktop renders the full app through the Wails bridge. The web build gates on
// login (Phase 0) and proves the HTTP transport end-to-end; later phases mount
// <App/> here too once every bound method has an HTTP endpoint.
root.render(
  <React.StrictMode>
    <I18nProvider>
      {isDesktop() ? <App /> : <LoginView />}
    </I18nProvider>
  </React.StrictMode>
)
