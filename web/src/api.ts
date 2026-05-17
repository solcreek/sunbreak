export type Health = {
  ok: boolean
  fts: boolean
  time_utc: string
}

export type Source = {
  ID: number
  Type: string
  Name: string
  URL: string
  Enabled: boolean
  IntervalSeconds: number
  Checkpoint: string
  ETag: string
  LastModified: string
  NextRunAt: string
  LastRunAt: string
  LastError: string
  ErrorCount: number
}

export type Rule = {
  ID: number
  Name: string
  Type: string
  Pattern: string
  CaseSensitive: boolean
  Enabled: boolean
}

export type Item = {
  ID: number
  SourceID: number
  SourceType: string
  SourceName: string
  ExternalID: string
  URL: string
  Title: string
  Content: string
  Author: string
  PublishedAt: string
  FetchedAt: string
}

export type Match = {
  ID: number
  ItemID: number
  RuleID: number
  MatchedText: string
  Score: number
  CreatedAt: string
  Item: Item
  Rule: Rule
}

export type Digest = {
  ID: number
  WindowStart: string
  WindowEnd: string
  Subject: string
  Body: string
  CreatedAt: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  })
  if (!response.ok) {
    const body = await response.text()
    throw new Error(body || response.statusText)
  }
  return response.json() as Promise<T>
}

function arrayResponse<T>(path: string, init?: RequestInit): Promise<T[]> {
  return request<T[] | null>(path, init).then((value) => value ?? [])
}

export function getHealth() {
  return request<Health>('/healthz')
}

export function getSources() {
  return arrayResponse<Source>('/api/sources')
}

export function getRules() {
  return arrayResponse<Rule>('/api/rules')
}

export function getMatches(hours = 24, limit = 100) {
  return arrayResponse<Match>(`/api/matches?hours=${hours}&limit=${limit}`)
}

export function getDigests(limit = 5) {
  return arrayResponse<Digest>(`/api/digests?limit=${limit}`)
}

export function searchItems(query: string, limit = 50) {
  const params = new URLSearchParams({ query, limit: String(limit) })
  return arrayResponse<Item>(`/api/items?${params}`)
}

export function runCollect() {
  return request<{ ran: boolean }>('/api/collect', { method: 'POST' })
}

export function runDigest() {
  return request<{ ran: boolean }>('/api/digest', { method: 'POST' })
}
