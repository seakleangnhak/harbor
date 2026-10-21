import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import * as Tooltip from '@radix-ui/react-tooltip'
import App from './App'
import './styles.css'

document.documentElement.classList.toggle('dark', localStorage.getItem('harbor:theme') === 'dark' || (!localStorage.getItem('harbor:theme') && matchMedia('(prefers-color-scheme: dark)').matches))

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 15_000, refetchOnWindowFocus: false }, mutations: { retry: false } },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <Tooltip.Provider delayDuration={350}><App /></Tooltip.Provider>
    </QueryClientProvider>
  </React.StrictMode>,
)
