import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/newsreader/400.css'
import '@fontsource/newsreader/600.css'
import './styles/index.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ToastProvider } from './components/ui/Toast'
import { ActivityProvider } from './lib/activityStore'
import { App } from './App'

const savedTheme = localStorage.getItem('muesli-theme') || 'system'
const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
document.documentElement.classList.toggle('dark', savedTheme === 'dark' || (savedTheme === 'system' && prefersDark))

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <MemoryRouter>
      <ToastProvider>
        <ActivityProvider>
          <App />
        </ActivityProvider>
      </ToastProvider>
    </MemoryRouter>
  </StrictMode>,
)
