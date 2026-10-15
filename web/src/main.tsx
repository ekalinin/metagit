import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource-variable/golos-text'
// "standard" has both the weight and the width axes: the width tells data from display text.
import '@fontsource-variable/martian-mono/standard.css'
import { App } from './App'
import './style.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
