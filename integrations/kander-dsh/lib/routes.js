// The HTTP routes of the Kander view. dsh registers them through
// connection.fetch.register and serves them only to its authenticated owner,
// after its own Host/Origin checks; the plugin opens no network port.
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { BoardError } from './boards.js'
import { Gone } from './protocol.js'

export const ROUTE_PREFIX = '/api/kander-dsh'
const BODY_LIMIT = 256 << 10
const PING_MS = 15_000
const VENDOR = join(dirname(fileURLToPath(import.meta.url)), '..', 'vendor')
const ASSETS = {
  'xterm.js': 'text/javascript; charset=utf-8',
  'xterm.css': 'text/css; charset=utf-8',
  'addon-fit.js': 'text/javascript; charset=utf-8',
}

const json = (body, status = 200) => Response.json(body, { status, headers: { 'Cache-Control': 'no-store' } })
const failure = (code, message, status, details = {}) => json({ error: { code, message, ...details } }, status)

const statusOf = (code) => ({ 'bad-request': 400, 'no-board': 404, gone: 404, 'not-carrier': 409, 'write-failed': 409 })[code] ?? 500

const handle = (action) => async (request) => {
  try {
    return json(await action(request))
  } catch (error) {
    if (error instanceof Gone) return failure('gone', error.message || 'the pane does not exist', 404)
    if (error instanceof BoardError) return failure(error.code, error.message, statusOf(error.code), error.details)
    return failure('internal', String(error?.message ?? error), 500)
  }
}

const readJson = async (request) => {
  const text = await request.text()
  if (text.length > BODY_LIMIT) throw new BoardError('bad-request', 'request body too large')
  try {
    const value = JSON.parse(text)
    if (value && typeof value === 'object' && !Array.isArray(value)) return value
  } catch {}
  throw new BoardError('bad-request', 'request body is not a JSON object')
}

const positive = (value) => Number.isInteger(value) && value > 0 && value <= 1000

/**
 * Formats one server-sent event. The payload is JSON, so a chunk with
 * newlines stays on one data line.
 */
export const sseEvent = (event, data) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`

/**
 * The route table; register each with connection.fetch.register.
 * @param host - the KanderHost.
 * @param boards - the Boards of this host.
 */
export function createRoutes(host, boards) {
  // Input of one pane is applied in arrival order; size is applied only when it changed.
  const inputs = new Map()
  const sizes = new Map()

  const input = async (request) => {
    const { pane, data, cols, rows } = await readJson(request)
    if (typeof pane !== 'string' || pane === '') throw new BoardError('bad-request', 'missing pane')
    if (data !== undefined && typeof data !== 'string') throw new BoardError('bad-request', 'data must be a string')
    const sized = positive(cols) && positive(rows)
    host.paneOf(pane)
    const previous = inputs.get(pane) ?? Promise.resolve()
    const current = previous.then(async () => {
      const size = `${cols}x${rows}`
      if (sized && sizes.get(pane) !== size) {
        await host.resize(pane, cols, rows)
        sizes.set(pane, size)
      }
      if (data) await host.write(pane, data)
    })
    const settled = current.catch(() => {})
    inputs.set(pane, settled)
    settled.then(() => { if (inputs.get(pane) === settled) inputs.delete(pane) })
    try {
      await current
    } catch (error) {
      if (error instanceof Gone) throw error
      throw new BoardError('write-failed', String(error?.message ?? error))
    }
    return { ok: true }
  }

  const stream = (request) => {
    const paneId = new URL(request.url).searchParams.get('pane') ?? ''
    try {
      host.paneOf(paneId)
    } catch (error) {
      return failure('gone', String(error?.message ?? error), 404)
    }
    const encoder = new TextEncoder()
    let closed = false
    let unsubscribe = () => {}
    let timer
    const body = new ReadableStream({
      start(controller) {
        const close = () => {
          if (closed) return
          closed = true
          clearInterval(timer)
          unsubscribe()
          try { controller.close() } catch {}
        }
        const send = (text) => {
          if (closed) return
          try {
            controller.enqueue(encoder.encode(text))
          } catch {
            close()
          }
        }
        // The replay and an exit can arrive inside subscribe itself.
        unsubscribe = host.subscribe(paneId, (chunk) => {
          if (chunk === null) {
            send(sseEvent('exit', {}))
            close()
          } else {
            send(sseEvent('output', chunk))
          }
        })
        if (closed) {
          unsubscribe()
          return
        }
        timer = setInterval(() => send(': ping\n\n'), PING_MS)
        timer.unref?.()
        request.signal?.addEventListener('abort', close)
      },
      cancel() {
        closed = true
        clearInterval(timer)
        unsubscribe()
      },
    })
    // dsh's gzip middleware buffers chunked bodies and skips only event streams.
    return new Response(body, { headers: { 'Content-Type': 'text/event-stream; charset=utf-8', 'Cache-Control': 'no-store' } })
  }

  const asset = (name) => async () => new Response(await readFile(join(VENDOR, name)), {
    headers: { 'Content-Type': ASSETS[name], 'Cache-Control': 'no-cache' },
  })

  return [
    {
      path: `${ROUTE_PREFIX}/state`,
      methods: ['GET'],
      fetch: handle(async () => ({ carrier: host.carrierStatus(), panes: await host.listPanes(), focus: host.lastFocus() })),
    },
    {
      path: `${ROUTE_PREFIX}/boards`,
      methods: ['GET'],
      fetch: handle(async (request) => {
        const paths = [...new Set(new URL(request.url).searchParams.getAll('path'))].slice(0, 200)
        return { boards: await Promise.all(paths.map(async (path) => ({ path, project: await boards.projectOf(path) }))) }
      }),
    },
    {
      path: `${ROUTE_PREFIX}/board`,
      methods: ['GET'],
      fetch: handle((request) => boards.list(new URL(request.url).searchParams.get('project') ?? '')),
    },
    {
      path: `${ROUTE_PREFIX}/start`,
      methods: ['POST'],
      fetch: handle(async (request) => {
        const { project, task } = await readJson(request)
        if (!host.isCarrier()) throw new BoardError('not-carrier', 'another dsh host runs the dsh agents')
        return boards.start(project, task)
      }),
    },
    { path: `${ROUTE_PREFIX}/input`, methods: ['POST'], fetch: handle(input) },
    { path: `${ROUTE_PREFIX}/stream`, methods: ['GET'], fetch: stream },
    ...Object.keys(ASSETS).map((name) => ({ path: `${ROUTE_PREFIX}/${name}`, methods: ['GET'], fetch: asset(name) })),
  ].map((route) => ({ ...route, requestBody: 'buffered' }))
}
