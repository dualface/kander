// The socket protocol between kander's embedded dsh.json definition and this
// plugin: POST /op/<name> with a form-encoded body, answered with one JSON
// object. A missing pane or container is HTTP 404 with {"error":"gone"}, which
// the definition classifies as gone; other failures are 4xx with
// {"error":"<message>"}. The version changes whenever an operation, a
// parameter, or a result field changes incompatibly; kander checks it with
// `hello` before it creates a pane.
import { createServer } from 'node:http'

export const PROTOCOL_VERSION = 1
const BODY_LIMIT = 1 << 20

/** A request named a pane or container that does not exist. */
export class Gone extends Error {}

/** A request is malformed. */
export class BadRequest extends Error {}

/**
 * Compares kander's protocol version with this plugin's.
 * @returns '' when they match, 'kander' or 'plugin' for the side to upgrade.
 */
export function upgradeSide(kanderVersion) {
  if (kanderVersion === PROTOCOL_VERSION) return ''
  return kanderVersion < PROTOCOL_VERSION ? 'kander' : 'plugin'
}

const readBody = (request) => new Promise((resolve, reject) => {
  let body = ''
  request.setEncoding('utf8')
  request.on('data', (chunk) => {
    body += chunk
    if (body.length > BODY_LIMIT) {
      reject(new BadRequest('request body too large'))
      request.destroy()
    }
  })
  request.on('end', () => resolve(body))
  request.on('error', reject)
})

/**
 * Builds the unlistened socket server.
 * @param ops - operation name to async (URLSearchParams) => result object.
 */
export function createOpsServer(ops) {
  return createServer(async (request, response) => {
    const reply = (status, body) => {
      response.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' })
      response.end(JSON.stringify(body))
    }
    const match = /^\/op\/([a-z_]+)$/.exec(new URL(request.url ?? '/', 'http://dsh').pathname)
    const op = match && Object.hasOwn(ops, match[1]) ? ops[match[1]] : undefined
    if (!op) return reply(404, { error: 'unknown op' })
    if (request.method !== 'POST') return reply(405, { error: 'use POST' })
    try {
      reply(200, await op(new URLSearchParams(await readBody(request))))
    } catch (error) {
      if (error instanceof Gone) return reply(404, { error: 'gone' })
      reply(error instanceof BadRequest ? 400 : 409, { error: String(error?.message ?? error) })
    }
  })
}

export function required(params, key) {
  const value = params.get(key)
  if (value === null || value === '') throw new BadRequest(`missing ${key}`)
  return value
}
