// The page logic of lib/client.js. The file is a classic script for dsh's
// module loader, so it runs here in a vm context with a fake loader, a React
// stub, and a fake fetch.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'
import vm from 'node:vm'

const source = readFileSync(new URL('../lib/client.js', import.meta.url), 'utf8')

function loadClient({ fetch, storage } = {}) {
  let entry
  const context = {
    window: { localStorage: storage, __ModuleLoader__: { load: (value) => { entry = value } } },
    fetch: fetch ?? (async () => { throw new Error('no fetch in this test') }),
    setTimeout,
    clearTimeout,
  }
  vm.runInNewContext(source, context)
  const react = { createElement: () => null, Fragment: 'fragment' }
  for (const hook of ['useCallback', 'useEffect', 'useMemo', 'useRef', 'useState', 'useSyncExternalStore']) react[hook] = () => { throw new Error('no React here') }
  const modules = { react, '@deepseek-ai/dsh-client-ui-primitives': { Button: 'Button', Modal: 'Modal' } }
  assert.equal(entry.id, 'kander-dsh')
  return entry.factory((name) => modules[name])
}

const { logic } = loadClient()
// Objects from the vm context have another Object prototype; compare plain copies.
const plain = (value) => JSON.parse(JSON.stringify(value))

/** A JSON fetch answering by path prefix; records the requested paths. */
function fakeFetch(answers) {
  const calls = []
  const fetch = async (url) => {
    calls.push(url)
    const key = Object.keys(answers).find((prefix) => url.startsWith(`/api/kander-dsh${prefix}`))
    const body = typeof answers[key] === 'function' ? answers[key]() : answers[key]
    return { ok: true, status: 200, json: async () => body }
  }
  return { fetch, calls }
}

class MemoryStorage {
  constructor(values = {}) { this.values = { ...values } }
  getItem(key) { return key in this.values ? this.values[key] : null }
  setItem(key, value) { this.values[key] = String(value) }
}

describe('board model', () => {
  test('a dsh WINDOW names its pane; other launchers have none', () => {
    assert.deepEqual(plain(logic.dshWindow('dsh:c7-81234:p7-81234')), { container: 'c7-81234', pane: 'p7-81234' })
    assert.equal(logic.dshWindow('tmux:s:w:p'), null)
    assert.equal(logic.dshWindow('herdr:w1:t1:w1:p1'), null)
    assert.equal(logic.dshWindow(''), null)
  })

  test('the board shows five columns and leaves archived and trash out', () => {
    const states = ['backlog', 'todo', 'working', 'review', 'done', 'archived', 'trash']
    const snapshot = { tasks: states.map((state) => ({ task_id: state, state })) }
    const columns = logic.columnsOf(snapshot)
    assert.deepEqual(plain(columns.map((column) => column.state)), ['backlog', 'todo', 'working', 'review', 'done'])
    assert.deepEqual(plain(columns.flatMap((column) => column.tasks.map((task) => task.task_id))), ['backlog', 'todo', 'working', 'review', 'done'])
    assert.equal(logic.columnsOf(null).length, 5)
  })

  test('needs attention: input waiting, exited while working, stopped, or blocked', () => {
    const working = { state: 'working', agent: { status: 'alive', runtime_state: 'unknown' } }
    assert.equal(logic.needsAttention(working, { inputWaiting: true, exited: false }), true)
    assert.equal(logic.needsAttention(working, { inputWaiting: false, exited: true }), true)
    assert.equal(logic.needsAttention({ ...working, state: 'review' }, { inputWaiting: false, exited: true }), false)
    assert.equal(logic.needsAttention({ state: 'working', agent: { status: 'stopped' } }, undefined), true)
    assert.equal(logic.needsAttention({ state: 'review', agent: { status: 'alive', runtime_state: 'blocked' } }, undefined), true)
    assert.equal(logic.needsAttention(working, { inputWaiting: null, exited: false }), false)
    assert.equal(logic.needsAttention({ state: 'todo', agent: null }, undefined), false)
  })

  test('an unchanged change token keeps the previous snapshot object', () => {
    const first = { change_token: 'a', tasks: [] }
    assert.equal(logic.keepSnapshot(first, { change_token: 'a', tasks: [] }), first)
    const next = { change_token: 'b', tasks: [] }
    assert.equal(logic.keepSnapshot(first, next), next)
    assert.equal(logic.keepSnapshot(null, next), next)
  })

  test('a tab shows the task ID of the card whose WINDOW names its pane', () => {
    const snapshot = { tasks: [{ task_id: '20261103-a-task', title: 'A', window: 'dsh:c1-9:p1-9' }] }
    assert.deepEqual(plain(logic.tabLabel({ pane: 'p1-9', task: '', title: 'label' }, snapshot)), { name: '20261103-a-task', title: 'A' })
    assert.deepEqual(plain(logic.tabLabel({ pane: 'p2-9', task: '', title: 'label' }, snapshot)), { name: 'p2-9', title: 'label' })
    assert.deepEqual(plain(logic.tabLabel({ pane: 'p2-9', task: 'kept', title: '' }, null)), { name: 'kept', title: '' })
  })
})

describe('workspace and boards', () => {
  const sessions = {
    byId: {
      s1: { id: 's1', cwd: '/w/one', retainedBy: {} },
      s2: { id: 's2', cwd: '/w/two/sub', retainedBy: { mainView: 1 } },
      s3: { id: 's3', cwd: '/loose', retainedBy: {} },
    },
  }
  const workspaces = { items: [{ path: '/w/one', sessionIds: ['s1'] }, { path: '/w/two', sessionIds: ['s2'] }] }

  test('the main view Session is the one the main view retains', () => {
    assert.equal(logic.mainSessionId(sessions), 's2')
    assert.equal(logic.mainSessionId({ byId: { s1: { retainedBy: { workspaceOperation: 1 } } } }), null)
    assert.equal(logic.mainSessionId(undefined), null)
  })

  test("a Session's workspace path, or its own directory without a workspace", () => {
    assert.equal(logic.workspacePathOf('s2', workspaces, sessions), '/w/two')
    assert.equal(logic.workspacePathOf('s3', workspaces, sessions), '/loose')
    assert.equal(logic.workspacePathOf(null, workspaces, sessions), null)
  })

  test('boards list each project once, in workspace order', () => {
    const boards = logic.boardsOf([
      { path: '/r/wt', project: '/r' },
      { path: '/plain', project: null },
      { path: '/r', project: '/r' },
      { path: '/b', project: '/b' },
    ])
    assert.deepEqual(plain(boards), [{ project: '/r', paths: ['/r/wt', '/r'] }, { project: '/b', paths: ['/b'] }])
  })
})

describe('terminal tabs', () => {
  test('open adds or activates a tab; close keeps the board tab', () => {
    let state = logic.openTab({ tabs: [], active: 'board' }, { pane: 'p1', task: 'a' })
    state = logic.openTab(state, { pane: 'p2', task: 'b' })
    state = logic.openTab(state, { pane: 'p3', task: 'c' })
    assert.deepEqual(plain(state.tabs.map((tab) => tab.pane)), ['p1', 'p2', 'p3'])
    state = logic.openTab(state, { pane: 'p1', task: 'a' })
    assert.equal(state.tabs.length, 3)
    assert.equal(state.active, 'p1')
    state = logic.closeTab(state, 'p1')
    assert.equal(state.active, 'board')
    assert.deepEqual(plain(logic.closeTab({ ...state, active: 'p3' }, 'p2')), { tabs: [{ pane: 'p3', task: 'c', title: '' }], active: 'p3' })
  })

  test('tabs are stored per board and survive a reload', () => {
    const storage = new MemoryStorage()
    const state = { tabs: [{ pane: 'p1', task: 'a', title: 'A' }], active: 'p1' }
    logic.saveTabs(storage, '/r', state)
    assert.deepEqual(plain(logic.loadTabs(storage, '/r')), state)
    assert.deepEqual(plain(logic.loadTabs(storage, '/other')), { tabs: [], active: 'board' })
  })

  test('damaged storage reads as no tabs; an unknown active tab falls back to the board', () => {
    assert.deepEqual(plain(logic.loadTabs(new MemoryStorage({ 'kander-dsh.tabs:/r': '{not json' }), '/r')), { tabs: [], active: 'board' })
    const stored = new MemoryStorage({ 'kander-dsh.tabs:/r': JSON.stringify({ tabs: [{ pane: 'p1' }, { pane: '' }, null], active: 'p9' }) })
    assert.deepEqual(plain(logic.loadTabs(stored, '/r')), { tabs: [{ pane: 'p1', task: '', title: '' }], active: 'board' })
    const throwing = { getItem() { throw new Error('denied') }, setItem() { throw new Error('denied') } }
    assert.deepEqual(plain(logic.loadTabs(throwing, '/r')), { tabs: [], active: 'board' })
    logic.saveTabs(throwing, '/r', { tabs: [], active: 'board' })
  })
})

describe('terminal transport', () => {
  test('the event parser joins split chunks and skips comments', () => {
    const parser = new logic.SseParser()
    assert.deepEqual(plain(parser.push('event: output\ndata: "a\\nb"\n')), [])
    assert.deepEqual(plain(parser.push('\n: ping\n\nevent: exit\ndata: {}\n\nevent: output\nda')), [
      { event: 'output', data: 'a\nb' },
      { event: 'exit', data: {} },
    ])
    assert.deepEqual(plain(parser.push('ta: "c"\n\n')), [{ event: 'output', data: 'c' }])
  })

  test('input is sent one request at a time, in order, with the latest size', async () => {
    const sent = []
    let release
    const queue = new logic.InputQueue(async (message) => {
      sent.push(plain(message))
      if (sent.length === 1) await new Promise((resolve) => { release = resolve })
    })
    const done = queue.push('a', 80, 24)
    queue.push('b', 80, 24)
    queue.push('c', 100, 30)
    assert.equal(sent.length, 1)
    release()
    await done
    assert.deepEqual(sent, [{ data: 'a', cols: 80, rows: 24 }, { data: 'bc', cols: 100, rows: 30 }])
  })

  test('a failed send is reported and later input still goes out', async () => {
    const sent = []
    const errors = []
    const queue = new logic.InputQueue(async (message) => {
      sent.push(message.data)
      if (message.data === 'x') throw new Error('gone')
    }, (error) => errors.push(error.message))
    await queue.push('x', 80, 24)
    await queue.push('y', 80, 24)
    assert.deepEqual(sent, ['x', 'y'])
    assert.deepEqual(errors, ['gone'])
  })

  test('only a newer focus request counts', () => {
    assert.equal(logic.newFocus(undefined, null), null)
    assert.equal(logic.newFocus(5, { pane: 'p', at: 5 }), null)
    assert.equal(logic.newFocus(5, { pane: 'p', at: 6 }).pane, 'p')
    assert.equal(logic.newFocus(undefined, { pane: 'p', at: 1 }).pane, 'p')
  })

  test('the dictionaries follow the dsh language and fill placeholders', () => {
    assert.equal(logic.translator('zh-CN')('card.start'), '启动')
    assert.equal(logic.translator('en')('start.running', { task: 't' }), 'Starting t…')
    assert.equal(logic.translator('ja')('card.attention'), 'Needs attention')
  })

  test('a non-carrier host explains every carrier reason', () => {
    const t = logic.translator('en')
    const notice = (carrier) => logic.carrierNotice({ carrier }, t)
    assert.equal(logic.carrierNotice(null, t), null)
    assert.equal(notice({ carrier: true, reason: 'carrier' }), null)
    assert.match(notice({ carrier: false, reason: 'held', holderPid: 42 }), /another dsh \(pid 42\)/)
    assert.doesNotMatch(notice({ carrier: false, reason: 'held', holderPid: null }), /pid/)
    assert.match(notice({ carrier: false, reason: 'blocked', socket: '/h/kander/mux.sock' }), /^\/h\/kander\/mux\.sock exists and is not a socket/)
    assert.match(notice({ carrier: false, reason: 'unreachable', socket: '/h/kander/mux.sock' }), /cannot serve \/h\/kander\/mux\.sock/)
    for (const reason of ['waiting', 'starting', 'stopped']) {
      // A known reason has its own sentence, not the generic one.
      assert.doesNotMatch(notice({ carrier: false, reason }), /\{|does not host the dsh agents \(/)
    }
    assert.match(notice({ carrier: false, reason: 'later' }), /\(later\)/)
    const zh = logic.translator('zh-CN')
    for (const reason of ['held', 'waiting', 'starting', 'blocked', 'unreachable', 'stopped']) {
      assert.doesNotMatch(logic.carrierNotice({ carrier: { carrier: false, reason, holderPid: 7, socket: '/s' } }, zh), /\{|dsh agents/)
    }
  })
})

describe('client store', () => {
  function source(value) {
    const listeners = new Set()
    return {
      value,
      getSnapshot() { return this.value },
      subscribe(listener) { listeners.add(listener); return () => listeners.delete(listener) },
      set(next) { this.value = next; for (const listener of listeners) listener() },
    }
  }

  function setup(answers) {
    const { fetch, calls } = fakeFetch(answers)
    const client = loadClient({ fetch })
    const selected = []
    const sessions = source({ byId: { s1: { cwd: '/r/wt', retainedBy: { mainView: 1 } }, s2: { cwd: '/b', retainedBy: {} } } })
    const workspaces = source({ items: [{ path: '/r/wt', sessionIds: ['s1'] }, { path: '/b', sessionIds: ['s2'] }] })
    const ctx = { sessions: { list: sessions }, workspaces: { list: workspaces }, layout: { selectPanel: (id) => selected.push(id) } }
    const store = client.logic.createStore(ctx, new MemoryStorage())
    return { store, sessions, workspaces, selected, calls }
  }

  test('the store follows the main view workspace and resets an explicit board on a switch', () => {
    const { store, sessions } = setup({})
    store.refreshWorkspaces()
    assert.equal(store.getState().currentPath, '/r/wt')
    assert.deepEqual(plain(store.getState().workspacePaths), ['/r/wt', '/b'])
    store.request({ project: '/other' })
    assert.equal(store.getState().requested.project, '/other')
    sessions.set({ byId: { s1: { retainedBy: {} }, s2: { cwd: '/b', retainedBy: { mainView: 1 } } } })
    store.refreshWorkspaces()
    assert.equal(store.getState().currentPath, '/b')
    assert.equal(store.getState().requested, null)
  })

  test("the header button asks for the Session's workspace board and shows the panel", () => {
    const { store, selected } = setup({})
    store.openForSession('s2')
    assert.deepEqual(plain(store.getState().requested), { path: '/b' })
    assert.deepEqual(selected, ['kander-dsh'])
  })

  test('a focus request newer than the first poll opens the board and the agent tab', async () => {
    let focus = { pane: 'p1-9', container: 'c1-9', cwd: '/r/wt', at: 100 }
    const panes = [{ pane: 'p1-9', project: '/r', label: 'kb-card' }, { pane: 'p2-9', project: '', label: 'x' }]
    const { store, selected, calls } = setup({
      '/state': () => ({ carrier: { carrier: true }, panes, focus }),
      '/boards': { boards: [{ path: '/elsewhere', project: '/e' }] },
    })
    await store.poll()
    assert.deepEqual(selected, [], 'the focus request before the page loaded is not replayed')
    focus = { ...focus, at: 200 }
    await store.poll()
    assert.deepEqual(selected, ['kander-dsh'])
    assert.deepEqual(plain(store.getState().requested), { project: '/r' })
    assert.deepEqual(plain(store.tabsOf('/r')), { tabs: [{ pane: 'p1-9', task: '', title: 'kb-card' }], active: 'p1-9' })
    await store.poll()
    assert.equal(selected.length, 1, 'the same request is handled once')
    // A pane without a project resolves its directory through the host.
    focus = { pane: 'p2-9', container: 'c2-9', cwd: '/elsewhere', at: 300 }
    await store.poll()
    assert.ok(calls.includes('/api/kander-dsh/boards?path=%2Felsewhere'))
    assert.equal(store.getState().requested.project, '/e')
    assert.equal(store.tabsOf('/e').active, 'p2-9')
  })

  test('a failed poll is reported and keeps the last host state', async () => {
    let fail = false
    const { store } = setup({ '/state': () => { if (fail) throw new Error('offline'); return { carrier: { carrier: true }, panes: [], focus: null } } })
    await store.poll()
    fail = true
    await store.poll()
    assert.equal(store.getState().live.carrier.carrier, true)
    assert.equal(store.getState().liveError, 'offline')
  })
})

describe('plugin registration', () => {
  test('apply registers the panel, its sidebar entry, and the session header button', () => {
    const registered = []
    const effects = []
    const ctx = {
      effect: (callback, label) => effects.push(label),
      locale: { register: () => () => {} },
      slots: {
        inject: (name, callback) => callback(),
        register: (options) => { registered.push(options); return () => {} },
      },
    }
    const client = loadClient({ fetch: async () => ({ ok: true, json: async () => ({}) }), storage: new MemoryStorage() })
    client.apply(ctx)
    assert.deepEqual(registered.map((options) => [options.name, options.key ?? options.id]), [
      ['main', 'kander-dsh'],
      ['sidebar.panellist', 'kander-dsh'],
      ['conversation.session.header.utilities', 'kander-dsh'],
    ])
    assert.equal(registered[1].label(), 'Kander')
    assert.equal(registered[2].inject('s1').sessionId, 's1')
    assert.deepEqual(effects, ['kander-dsh.store', 'kander-dsh.locale'])
    assert.deepEqual(plain(client.inject), ['slots', 'locale', 'layout', 'sessions', 'workspaces'])
  })
})
