import { useEffect, useMemo, useState } from 'react'
import {
  Activity,
  AlertCircle,
  CheckCircle2,
  Database,
  FileSearch,
  Loader2,
  Play,
  RefreshCcw,
  Rss,
  Search,
  Server,
  Sparkles,
} from 'lucide-react'

import {
  getDigests,
  getHealth,
  getMatches,
  getRules,
  getSources,
  runCollect,
  runDigest,
  searchItems,
  type Digest,
  type Health,
  type Item,
  type Match,
  type Rule,
  type Source,
} from './api'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from './components/ui/card'
import { Input } from './components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './components/ui/table'

type DashboardState = {
  health: Health | null
  sources: Source[]
  rules: Rule[]
  matches: Match[]
  digests: Digest[]
}

const initialState: DashboardState = {
  health: null,
  sources: [],
  rules: [],
  matches: [],
  digests: [],
}

function App() {
  const [state, setState] = useState<DashboardState>(initialState)
  const [searchQuery, setSearchQuery] = useState('sqlite')
  const [items, setItems] = useState<Item[]>([])
  const [loading, setLoading] = useState(true)
  const [action, setAction] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    setError(null)
    setLoading(true)
    try {
      const [health, sources, rules, matches, digests] = await Promise.all([
        getHealth(),
        getSources(),
        getRules(),
        getMatches(24, 100),
        getDigests(5),
      ])
      setState({ health, sources, rules, matches, digests })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed')
    } finally {
      setLoading(false)
    }
  }

  async function runAction(name: string, fn: () => Promise<unknown>) {
    setAction(name)
    setError(null)
    try {
      await fn()
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed')
    } finally {
      setAction(null)
    }
  }

  async function runSearch() {
    setAction('search')
    setError(null)
    try {
      const results = await searchItems(searchQuery, 50)
      setItems(results)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Search failed')
    } finally {
      setAction(null)
    }
  }

  useEffect(() => {
    let canceled = false

    async function loadInitialData() {
      try {
        const [health, sources, rules, matches, digests, results] =
          await Promise.all([
            getHealth(),
            getSources(),
            getRules(),
            getMatches(24, 100),
            getDigests(5),
            searchItems('sqlite', 50),
          ])
        if (canceled) return
        setState({ health, sources, rules, matches, digests })
        setItems(results)
      } catch (err) {
        if (!canceled) {
          setError(err instanceof Error ? err.message : 'Request failed')
        }
      } finally {
        if (!canceled) {
          setLoading(false)
        }
      }
    }

    loadInitialData()
    return () => {
      canceled = true
    }
  }, [])

  const activeSources = state.sources.filter((source) => source.Enabled).length
  const activeRules = state.rules.filter((rule) => rule.Enabled).length
  const failedSources = state.sources.filter((source) => source.LastError).length
  const sourceTypes = useMemo(
    () => Array.from(new Set(state.sources.map((source) => source.Type))),
    [state.sources],
  )

  return (
    <main className="min-h-screen bg-background">
      <div className="mx-auto flex w-full max-w-[1440px] flex-col gap-5 px-4 py-4 sm:px-6 lg:px-8">
        <header className="flex flex-col gap-3 border-b pb-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-2xl font-semibold">Radar</h1>
              <Badge variant={state.health?.ok ? 'secondary' : 'destructive'}>
                {state.health?.ok ? 'online' : 'offline'}
              </Badge>
              <Badge variant="outline">
                {state.health?.fts ? 'FTS5 enabled' : 'FTS fallback'}
              </Badge>
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              Keyword monitoring pipeline for sources, rules, matches, and digests.
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              onClick={refresh}
              disabled={loading || action !== null}
            >
              {loading ? <Loader2 className="animate-spin" /> : <RefreshCcw />}
              Refresh
            </Button>
            <Button
              variant="secondary"
              onClick={() => runAction('digest', runDigest)}
              disabled={action !== null}
            >
              {action === 'digest' ? <Loader2 className="animate-spin" /> : <Sparkles />}
              Digest
            </Button>
            <Button
              onClick={() => runAction('collect', runCollect)}
              disabled={action !== null}
            >
              {action === 'collect' ? <Loader2 className="animate-spin" /> : <Play />}
              Collect
            </Button>
          </div>
        </header>

        {error ? (
          <div className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            <AlertCircle className="mt-0.5 size-4 shrink-0" />
            <span className="break-words">{error}</span>
          </div>
        ) : null}

        <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <MetricCard
            icon={<Rss />}
            label="Active sources"
            value={activeSources}
            detail={`${sourceTypes.length} source types`}
          />
          <MetricCard
            icon={<FileSearch />}
            label="Active rules"
            value={activeRules}
            detail={`${state.rules.length} configured`}
          />
          <MetricCard
            icon={<Activity />}
            label="Matches, 24h"
            value={state.matches.length}
            detail="precomputed matches"
          />
          <MetricCard
            icon={failedSources ? <AlertCircle /> : <CheckCircle2 />}
            label="Source health"
            value={failedSources}
            detail={failedSources ? 'sources need attention' : 'no source errors'}
          />
        </section>

        <section className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_420px]">
          <Card>
            <CardHeader className="flex flex-col gap-3 border-b pb-4 md:flex-row md:items-center md:justify-between">
              <div>
                <CardTitle>Recent matches</CardTitle>
                <p className="mt-1 text-sm text-muted-foreground">
                  Last 24 hours, newest first.
                </p>
              </div>
              <Badge variant="muted">{state.matches.length} rows</Badge>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-[180px]">Rule</TableHead>
                    <TableHead>Item</TableHead>
                    <TableHead className="w-[160px]">Source</TableHead>
                    <TableHead className="w-[150px]">Matched</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {state.matches.map((match) => (
                    <TableRow key={match.ID}>
                      <TableCell>
                        <Badge variant="outline">{match.Rule.Name}</Badge>
                      </TableCell>
                      <TableCell>
                        <a
                          className="line-clamp-1 font-medium hover:underline"
                          href={match.Item.URL || undefined}
                          target="_blank"
                          rel="noreferrer"
                        >
                          {match.Item.Title || trimText(match.Item.Content, 90)}
                        </a>
                        <div className="mt-1 line-clamp-1 text-xs text-muted-foreground">
                          {trimText(match.Item.Content, 140)}
                        </div>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {match.Item.SourceName}
                      </TableCell>
                      <TableCell>
                        <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
                          {match.MatchedText}
                        </code>
                      </TableCell>
                    </TableRow>
                  ))}
                  {!state.matches.length ? (
                    <TableRow>
                      <TableCell
                        colSpan={4}
                        className="h-28 text-center text-muted-foreground"
                      >
                        No matches in the current window.
                      </TableCell>
                    </TableRow>
                  ) : null}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <div className="flex flex-col gap-5">
            <Card>
              <CardHeader className="border-b pb-4">
                <CardTitle>Sources</CardTitle>
              </CardHeader>
              <CardContent className="p-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead className="w-[92px]">Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {state.sources.map((source) => (
                      <TableRow key={source.ID}>
                        <TableCell>
                          <div className="font-medium">{source.Name}</div>
                          <div className="text-xs text-muted-foreground">
                            {source.Type} · every {source.IntervalSeconds}s
                          </div>
                          {source.LastError ? (
                            <div className="mt-1 line-clamp-2 text-xs text-destructive">
                              {source.LastError}
                            </div>
                          ) : null}
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              source.LastError
                                ? 'destructive'
                                : source.Enabled
                                  ? 'secondary'
                                  : 'muted'
                            }
                          >
                            {source.LastError
                              ? 'error'
                              : source.Enabled
                                ? 'active'
                                : 'off'}
                          </Badge>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="border-b pb-4">
                <CardTitle>Latest digest</CardTitle>
              </CardHeader>
              <CardContent className="pt-4">
                {state.digests[0] ? (
                  <pre className="max-h-[260px] overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-xs leading-5 text-muted-foreground">
                    {state.digests[0].Body}
                  </pre>
                ) : (
                  <p className="text-sm text-muted-foreground">No digest yet.</p>
                )}
              </CardContent>
            </Card>
          </div>
        </section>

        <section className="grid gap-5 xl:grid-cols-[420px_minmax(0,1fr)]">
          <Card>
            <CardHeader className="border-b pb-4">
              <CardTitle>Rules</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 pt-4">
              {state.rules.map((rule) => (
                <div
                  key={rule.ID}
                  className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"
                >
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium">{rule.Name}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      {rule.Type} · {rule.Pattern}
                    </div>
                  </div>
                  <Badge variant={rule.Enabled ? 'secondary' : 'muted'}>
                    {rule.Enabled ? 'on' : 'off'}
                  </Badge>
                </div>
              ))}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-col gap-3 border-b pb-4 md:flex-row md:items-center md:justify-between">
              <div>
                <CardTitle>Item search</CardTitle>
                <p className="mt-1 text-sm text-muted-foreground">
                  Uses SQLite FTS5 when available.
                </p>
              </div>
              <form
                className="flex w-full gap-2 md:w-[360px]"
                onSubmit={(event) => {
                  event.preventDefault()
                  runSearch()
                }}
              >
                <Input
                  value={searchQuery}
                  onChange={(event) => setSearchQuery(event.target.value)}
                  placeholder="Search items"
                />
                <Button size="icon" aria-label="Search" disabled={action === 'search'}>
                  {action === 'search' ? (
                    <Loader2 className="animate-spin" />
                  ) : (
                    <Search />
                  )}
                </Button>
              </form>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Title</TableHead>
                    <TableHead className="w-[160px]">Source</TableHead>
                    <TableHead className="w-[120px]">Fetched</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((item) => (
                    <TableRow key={item.ID}>
                      <TableCell>
                        <a
                          className="line-clamp-1 font-medium hover:underline"
                          href={item.URL || undefined}
                          target="_blank"
                          rel="noreferrer"
                        >
                          {item.Title || trimText(item.Content, 100)}
                        </a>
                        <div className="mt-1 line-clamp-1 text-xs text-muted-foreground">
                          {trimText(item.Content, 150)}
                        </div>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {item.SourceName}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {relativeTime(item.FetchedAt)}
                      </TableCell>
                    </TableRow>
                  ))}
                  {!items.length ? (
                    <TableRow>
                      <TableCell
                        colSpan={3}
                        className="h-24 text-center text-muted-foreground"
                      >
                        No items found.
                      </TableCell>
                    </TableRow>
                  ) : null}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </section>

        <footer className="flex flex-wrap items-center gap-3 border-t py-4 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <Server className="size-3.5" />
            API :8080
          </span>
          <span className="inline-flex items-center gap-1">
            <Database className="size-3.5" />
            SQLite WAL
          </span>
          <span>Last refresh: {state.health?.time_utc || 'pending'}</span>
        </footer>
      </div>
    </main>
  )
}

function MetricCard({
  icon,
  label,
  value,
  detail,
}: {
  icon: React.ReactNode
  label: string
  value: number
  detail: string
}) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3 pb-2">
        <CardTitle>{label}</CardTitle>
        <div className="text-muted-foreground [&_svg]:size-4">{icon}</div>
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-semibold">{value}</div>
        <p className="mt-1 text-sm text-muted-foreground">{detail}</p>
      </CardContent>
    </Card>
  )
}

function trimText(value: string, limit: number) {
  const text = value.replace(/\s+/g, ' ').trim()
  if (text.length <= limit) return text
  return `${text.slice(0, limit)}...`
}

function relativeTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'unknown'
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return date.toLocaleDateString()
}

export default App
