export interface ServiceReadinessSnapshot {
  services: Array<{
    id: 'transcriber' | 'streaming-transcriber' | 'agent' | 'semantic-search'
    configured: boolean
    healthy: boolean
  }>
  models: Array<{
    name: string
    status: 'downloading' | 'ready' | 'failed'
    percent: number
    error?: string
  }>
  managed_models: boolean
}
