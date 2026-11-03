// kander-dsh browser half: the Kander view. A global panel shows the board of
// the selected workspace with one tab per open agent terminal, a session
// header button opens the view for that session's workspace, and a kander
// focus request opens the view on the agent's terminal. dsh loads client
// entries as classic scripts through window.__ModuleLoader__ with a
// synchronous require for its shared modules, so this file is hand-written
// without a build step. The pure view logic is exported as `logic` for the
// node --test suite.
window.__ModuleLoader__.load({
  id: 'kander-dsh',
  factory: (require) => {
    var module = { exports: {} }
    var exports = module.exports
    Object.defineProperty(exports, Symbol.toStringTag, { value: 'Module' })

    const React = require('react')
    const { Button, Modal } = require('@deepseek-ai/dsh-client-ui-primitives')
    const h = React.createElement
    const { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } = React

    const NS = 'kander-dsh'
    const PANEL_ID = 'kander-dsh'
    const API = '/api/kander-dsh'
    const STATE_POLL_MS = 2000
    const BOARD_POLL_MS = 4000
    const RESOLVE_POLL_MS = 15_000
    const STORAGE_PREFIX = 'kander-dsh.tabs:'
    const COLUMNS = ['backlog', 'todo', 'working', 'review', 'done']

    const en = {
      'view.title': 'Kander',
      'view.board': 'Board',
      'view.switch': 'Board',
      'view.loading': 'Loading the board…',
      'view.noWorkspace': 'Select a session of a workspace to show its board.',
      'view.noBoard': 'The workspace {path} has no Kander board.',
      'view.noBoardHint': 'Create one in its main worktree:',
      'view.otherBoards': 'Other boards can be chosen above.',
      'view.kanderMissing': 'kander was not found for this board.',
      'view.kanderSearched': 'Searched: {paths}',
      'view.kanderConfig': 'Install kander on the PATH of dsh, or set the plugin configuration `kander` to its absolute path.',
      'view.kanderFailed': 'kander list failed:',
      'view.network': 'dsh did not answer: {error}',
      'carrier.other': 'The dsh agents run in another dsh (pid {pid}). Here they show their status only; starting and terminals are available there.',
      'carrier.otherUnknown': 'The dsh agents run in another dsh. Here they show their status only; starting and terminals are available there.',
      'carrier.waiting': 'Another dsh is taking over the dsh agents. Here they show their status only for now.',
      'carrier.starting': 'This dsh is starting to host the dsh agents.',
      'carrier.blocked': '{socket} exists and is not a socket. Remove it to let this dsh host the dsh agents.',
      'carrier.unreachable': 'This dsh cannot serve {socket}; see the dsh log. It tries again every second.',
      'carrier.stopped': 'The kander-dsh plugin of this dsh stopped hosting the dsh agents.',
      'carrier.none': 'This dsh does not host the dsh agents ({reason}).',
      'column.backlog': 'Backlog',
      'column.todo': 'Todo',
      'column.working': 'Working',
      'column.review': 'Review',
      'column.done': 'Done',
      'card.attention': 'Needs attention',
      'card.start': 'Start',
      'card.terminal': 'Open terminal',
      'card.otherHost': 'Runs in another dsh',
      'card.noPane': 'Terminal gone',
      'start.title': 'Start {task}?',
      'start.body': 'kander start --launcher dsh {task} runs in {project}. The agent runs in this dsh.',
      'start.confirm': 'Start',
      'start.cancel': 'Cancel',
      'start.close': 'Close',
      'start.running': 'Starting {task}…',
      'start.failed': 'kander start {task} failed:',
      'start.dismiss': 'Dismiss',
      'tab.close': 'Close tab',
      'term.connecting': 'Connecting…',
      'term.live': 'Live',
      'term.exited': 'The agent exited',
      'term.gone': 'The terminal no longer exists',
      'term.otherHost': 'The agent runs in another dsh',
      'term.error': 'Terminal error: {error}',
      'term.inputFailed': 'Input was not delivered: {error}. Type it again once the terminal answers.',
      'header.open': 'Open the Kander board of this workspace',
    }
    const zh = {
      'view.title': 'Kander',
      'view.board': '看板',
      'view.switch': '看板',
      'view.loading': '正在读取看板…',
      'view.noWorkspace': '选择一个工作区的会话后显示它的看板.',
      'view.noBoard': '工作区 {path} 没有 Kander 看板.',
      'view.noBoardHint': '在它的主 worktree 中创建:',
      'view.otherBoards': '可以在上方选择其他看板.',
      'view.kanderMissing': '没有找到这个看板使用的 kander.',
      'view.kanderSearched': '已查找: {paths}',
      'view.kanderConfig': '把 kander 安装到 dsh 的 PATH 中, 或在插件配置 `kander` 中写它的绝对路径.',
      'view.kanderFailed': 'kander list 失败:',
      'view.network': 'dsh 没有响应: {error}',
      'carrier.other': 'dsh agent 在另一个 dsh 中运行 (pid {pid}). 这里只显示状态; 启动与终端请在那个 dsh 中使用.',
      'carrier.otherUnknown': 'dsh agent 在另一个 dsh 中运行. 这里只显示状态; 启动与终端请在那个 dsh 中使用.',
      'carrier.waiting': '另一个 dsh 正在接管 dsh agent. 这里暂时只显示状态.',
      'carrier.starting': '这个 dsh 正在开始承载 dsh agent.',
      'carrier.blocked': '{socket} 已存在且不是 socket. 删除它后这个 dsh 才能承载 dsh agent.',
      'carrier.unreachable': '这个 dsh 无法提供 {socket}, 详见 dsh 日志. 每秒重试一次.',
      'carrier.stopped': '这个 dsh 的 kander-dsh 插件已停止承载 dsh agent.',
      'carrier.none': '这个 dsh 不承载 dsh agent ({reason}).',
      'column.backlog': 'Backlog',
      'column.todo': 'Todo',
      'column.working': 'Working',
      'column.review': 'Review',
      'column.done': 'Done',
      'card.attention': '需要处理',
      'card.start': '启动',
      'card.terminal': '打开终端',
      'card.otherHost': '在另一个 dsh 中运行',
      'card.noPane': '终端已不存在',
      'start.title': '启动 {task}?',
      'start.body': '在 {project} 中运行 kander start --launcher dsh {task}. agent 在这个 dsh 中运行.',
      'start.confirm': '启动',
      'start.cancel': '取消',
      'start.close': '关闭',
      'start.running': '正在启动 {task}…',
      'start.failed': 'kander start {task} 失败:',
      'start.dismiss': '关闭',
      'tab.close': '关闭标签',
      'term.connecting': '正在连接…',
      'term.live': '已连接',
      'term.exited': 'agent 已退出',
      'term.gone': '终端已不存在',
      'term.otherHost': 'agent 在另一个 dsh 中运行',
      'term.error': '终端错误: {error}',
      'term.inputFailed': '输入未送达: {error}. 终端恢复后请重新输入.',
      'header.open': '打开这个工作区的 Kander 看板',
    }

    // ---- Pure logic (exported as `logic` for tests) ----

    const format = (text, vars) => text.replace(/\{(\w+)\}/g, (match, key) => (vars && key in vars ? String(vars[key]) : match))

    /** A translator over the two dictionaries; the language follows the dsh locale. */
    function translator(language) {
      const dictionary = String(language ?? '').toLowerCase().startsWith('zh') ? zh : en
      return (key, vars) => format(dictionary[key] ?? en[key] ?? key, vars)
    }

    /** The dsh address of a card WINDOW `dsh:<container>:<pane>`, or null for other launchers. */
    function dshWindow(window) {
      const match = /^dsh:([^:]+):([^:]+)$/.exec(window ?? '')
      return match ? { container: match[1], pane: match[2] } : null
    }

    /** The five shown columns with their tasks; archived and trash are left out. */
    function columnsOf(snapshot) {
      return COLUMNS.map((state) => ({ state, tasks: (snapshot?.tasks ?? []).filter((task) => task.state === state) }))
    }

    /**
     * Whether a card needs the user: its dsh agent waits for input, its dsh
     * agent exited while the card is still working, or the snapshot reports
     * the agent stopped or blocked.
     */
    function needsAttention(task, pane) {
      if (pane?.inputWaiting === true) return true
      if (pane?.exited === true && task.state === 'working') return true
      return task.agent?.status === 'stopped' || task.agent?.runtime_state === 'blocked'
    }

    /** Keeps the previous snapshot object while the change token is the same, so nothing redraws. */
    function keepSnapshot(previous, next) {
      return previous && next && previous.change_token === next.change_token ? previous : next
    }

    /** The Session the main view shows: the one the main view retains. */
    function mainSessionId(sessions) {
      for (const id of Object.keys(sessions?.byId ?? {})) {
        if ((sessions.byId[id]?.retainedBy?.mainView ?? 0) > 0) return id
      }
      return null
    }

    /** The directory of a Session's workspace, or the Session's own directory without one. */
    function workspacePathOf(sessionId, workspaces, sessions) {
      if (!sessionId) return null
      const workspace = (workspaces?.items ?? []).find((item) => item.sessionIds?.includes(sessionId))
      return workspace?.path ?? sessions?.byId?.[sessionId]?.cwd ?? null
    }

    /** One entry per board, in workspace order: [{project, paths}]. */
    function boardsOf(resolutions) {
      const boards = new Map()
      for (const { path, project } of resolutions ?? []) {
        if (!project) continue
        if (!boards.has(project)) boards.set(project, { project, paths: [] })
        boards.get(project).paths.push(path)
      }
      return [...boards.values()]
    }

    /** A board's tab state: {tabs: [{pane, task, title}], active: 'board' or a pane}. */
    const EMPTY_TABS = Object.freeze({ tabs: Object.freeze([]), active: 'board' })

    function loadTabs(storage, project) {
      try {
        const value = JSON.parse(storage?.getItem(STORAGE_PREFIX + project) ?? 'null')
        if (!value || !Array.isArray(value.tabs)) return EMPTY_TABS
        const tabs = value.tabs.filter((tab) => tab && typeof tab.pane === 'string' && tab.pane !== '')
          .map((tab) => ({ pane: tab.pane, task: String(tab.task ?? ''), title: String(tab.title ?? '') }))
        const active = tabs.some((tab) => tab.pane === value.active) ? value.active : 'board'
        return { tabs, active }
      } catch {
        return EMPTY_TABS
      }
    }

    function saveTabs(storage, project, value) {
      try {
        storage?.setItem(STORAGE_PREFIX + project, JSON.stringify(value))
      } catch {}
    }

    /** Opens a terminal tab, or activates it when it is already open. */
    function openTab(state, tab) {
      if (state.tabs.some((item) => item.pane === tab.pane)) return { ...state, active: tab.pane }
      return { tabs: [...state.tabs, { pane: tab.pane, task: tab.task ?? '', title: tab.title ?? '' }], active: tab.pane }
    }

    /** Closes a terminal tab; the board tab never closes. */
    function closeTab(state, pane) {
      const tabs = state.tabs.filter((tab) => tab.pane !== pane)
      return { tabs, active: state.active === pane ? 'board' : state.active }
    }

    /** The name and tooltip of a terminal tab: the card whose WINDOW names the pane, else what the tab stored. */
    function tabLabel(tab, snapshot) {
      const task = snapshot?.tasks?.find((item) => dshWindow(item.window)?.pane === tab.pane)
      return task ? { name: task.task_id, title: task.title } : { name: tab.task || tab.pane, title: tab.title }
    }

    /** Splits server-sent events; push returns the complete events as [{event, data}]. */
    class SseParser {
      constructor() { this.buffer = '' }
      push(text) {
        this.buffer += text.replace(/\r\n/g, '\n')
        const events = []
        let end
        while ((end = this.buffer.indexOf('\n\n')) >= 0) {
          const block = this.buffer.slice(0, end)
          this.buffer = this.buffer.slice(end + 2)
          let event = 'message'
          const data = []
          for (const line of block.split('\n')) {
            if (line.startsWith(':')) continue
            if (line.startsWith('event:')) event = line.slice(6).trim()
            else if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''))
          }
          if (data.length === 0) continue
          try {
            events.push({ event, data: JSON.parse(data.join('\n')) })
          } catch {}
        }
        return events
      }
    }

    /**
     * Sends terminal input one request at a time, so keystrokes cannot
     * reorder. Input that arrives while a request runs is joined into the next
     * one, which carries the latest terminal size.
     */
    class InputQueue {
      /**
       * @param send - async ({data, cols, rows}) => void.
       * @param onError - (error, message) for a request that failed; its input is lost.
       * @param onSent - (message) for a request that the host took.
       */
      constructor(send, onError = () => {}, onSent = () => {}) {
        this.send = send
        this.onError = onError
        this.onSent = onSent
        this.data = ''
        this.size = null
        this.waiting = false
        this.running = null
      }

      push(data, cols, rows) {
        this.data += data
        this.size = { cols, rows }
        this.waiting = true
        this.running ??= this.pump().finally(() => { this.running = null })
        return this.running
      }

      async pump() {
        while (this.waiting) {
          const message = { data: this.data, ...this.size }
          this.data = ''
          this.waiting = false
          try {
            await this.send(message)
          } catch (error) {
            this.onError(error, message)
            continue
          }
          this.onSent(message)
        }
      }
    }

    /**
     * Keeps a browser terminal from answering terminal queries (device
     * attributes, cursor position, mode and status requests, color queries).
     * The host's screen of the pane is the only answerer, also while no page
     * shows the pane; an answer from the page would reach the program a second
     * time, and replaying the output buffer would answer old queries again.
     */
    function muteQueries(term) {
      const parser = term.parser
      const swallow = () => true
      for (const id of [
        { final: 'c' }, { prefix: '>', final: 'c' },
        { final: 'n' }, { prefix: '?', final: 'n' },
        { intermediates: '$', final: 'p' }, { prefix: '?', intermediates: '$', final: 'p' },
      ]) parser.registerCsiHandler(id, swallow)
      parser.registerDcsHandler({ intermediates: '$', final: 'q' }, swallow)
      // A color is still set; only a request ("?") is dropped.
      for (const ident of [4, 10, 11, 12]) parser.registerOscHandler(ident, (data) => data.split(';').includes('?'))
    }

    /** The status line of a terminal tab: {shown, text, inputText}. */
    function terminalStatus({ status, error, inputError, carrier, known, paneInfo }, t) {
      let shown = status
      if (carrier && known && !paneInfo) shown = 'gone'
      else if (paneInfo?.exited) shown = 'exited'
      const text = shown === 'error' ? t('term.error', { error }) : t(`term.${shown}`)
      return { shown, text, inputText: inputError ? t('term.inputFailed', { error: inputError }) : '' }
    }

    /** A focus request newer than the last one seen, or null. */
    function newFocus(lastAt, focus) {
      return focus && typeof focus.at === 'number' && (lastAt === undefined || focus.at > lastAt) ? focus : null
    }

    // ---- Host calls ----

    async function call(method, path, body, signal) {
      const init = { method, headers: {}, signal }
      if (body !== undefined) {
        init.headers['Content-Type'] = 'application/json'
        init.body = JSON.stringify(body)
      }
      const response = await fetch(API + path, init)
      const data = await response.json().catch(() => undefined)
      if (!response.ok) {
        const error = new Error(data?.error?.message || `HTTP ${response.status}`)
        error.info = data?.error ?? { code: 'http', message: `HTTP ${response.status}` }
        throw error
      }
      return data
    }

    let localeRuntime
    const language = () => {
      let active = ''
      try { active = localeRuntime?.getSnapshot?.().active ?? '' } catch {}
      return active || (typeof navigator === 'undefined' ? '' : navigator.language ?? '')
    }

    let xtermLoading
    const loadScript = (src) => new Promise((resolve, reject) => {
      const script = document.createElement('script')
      script.src = src
      script.onload = resolve
      script.onerror = () => reject(new Error(`could not load ${src}`))
      document.head.appendChild(script)
    })
    /** Loads the vendored xterm once; dsh does not share its own xterm with plugins. */
    function loadXterm() {
      xtermLoading ??= (async () => {
        const css = document.createElement('link')
        css.rel = 'stylesheet'
        css.href = `${API}/xterm.css`
        document.head.appendChild(css)
        await loadScript(`${API}/xterm.js`)
        await loadScript(`${API}/addon-fit.js`)
        // xterm's bundle spreads its exports onto the global; the fit addon's
        // bundle sets its whole module object as the global FitAddon.
        const FitAddon = window.FitAddon?.FitAddon
        if (typeof window.Terminal !== 'function' || typeof FitAddon !== 'function') throw new Error('xterm did not load')
        return { Terminal: window.Terminal, FitAddon }
      })()
      xtermLoading.catch(() => { xtermLoading = undefined })
      return xtermLoading
    }

    // ---- The client store: workspace selection, host state, tabs, focus ----

    /**
     * One store per client root. It follows the selected workspace through the
     * dsh Session and Workspace controllers, polls the host state for every
     * page, keeps the terminal tabs per board, and turns kander focus requests
     * into a board selection plus an open tab.
     */
    function createStore(ctx, storage) {
      let state = {
        currentPath: null,
        workspacePaths: [],
        requested: null,
        live: null,
        liveError: null,
        tabs: {},
      }
      const listeners = new Set()
      const set = (patch) => {
        state = { ...state, ...patch }
        for (const listener of listeners) listener()
      }
      let lastFocusAt

      const readWorkspaces = () => {
        const sessions = ctx.sessions?.list?.getSnapshot?.()
        const workspaces = ctx.workspaces?.list?.getSnapshot?.()
        const current = workspacePathOf(mainSessionId(sessions), workspaces, sessions)
        const paths = (workspaces?.items ?? []).map((item) => item.path).filter(Boolean)
        const patch = {}
        if (current !== state.currentPath) {
          patch.currentPath = current
          // A different workspace shows its own board again.
          patch.requested = null
        }
        if (paths.join('\n') !== state.workspacePaths.join('\n')) patch.workspacePaths = paths
        if (Object.keys(patch).length) set(patch)
      }

      const tabsOf = (project) => {
        if (!state.tabs[project]) state.tabs[project] = loadTabs(storage, project)
        return state.tabs[project]
      }
      const setTabs = (project, value) => {
        saveTabs(storage, project, value)
        set({ tabs: { ...state.tabs, [project]: value } })
      }

      const focus = async (request) => {
        const pane = state.live?.panes?.find((item) => item.pane === request.pane)
        let project = pane?.project || ''
        if (!project && request.cwd) {
          const answer = await call('GET', `/boards?path=${encodeURIComponent(request.cwd)}`).catch(() => null)
          project = answer?.boards?.[0]?.project ?? ''
        }
        if (!project) return
        // The tab shows the card's task ID once the board snapshot names the pane.
        setTabs(project, openTab(tabsOf(project), { pane: request.pane, task: '', title: pane?.label ?? '' }))
        set({ requested: { project } })
        try { ctx.layout?.selectPanel?.(PANEL_ID) } catch {}
      }

      const poll = async () => {
        try {
          const live = await call('GET', '/state')
          const request = newFocus(lastFocusAt, live.focus)
          // The first answer only sets the baseline: an older request is not replayed.
          const first = state.live === null
          if (live.focus) lastFocusAt = live.focus.at
          set({ live, liveError: null })
          if (request && !first) await focus(request)
        } catch (error) {
          set({ liveError: String(error?.message ?? error) })
        }
      }

      return {
        getState: () => state,
        subscribe(listener) {
          listeners.add(listener)
          return () => listeners.delete(listener)
        },
        start() {
          const unsubscribers = [ctx.sessions?.list?.subscribe?.(readWorkspaces), ctx.workspaces?.list?.subscribe?.(readWorkspaces)]
          readWorkspaces()
          let timer
          let stopped = false
          const tick = async () => {
            await poll()
            if (!stopped) timer = setTimeout(tick, STATE_POLL_MS)
          }
          void tick()
          return () => {
            stopped = true
            clearTimeout(timer)
            for (const unsubscribe of unsubscribers) if (typeof unsubscribe === 'function') unsubscribe()
          }
        },
        refreshWorkspaces: readWorkspaces,
        poll,
        focus,
        tabsOf,
        setTabs,
        /** Shows a board chosen by project, or by a workspace path the page resolves. */
        request(requested) { set({ requested }) },
        /** The header button: the board of a Session's workspace. */
        openForSession(sessionId) {
          const path = workspacePathOf(sessionId, ctx.workspaces?.list?.getSnapshot?.(), ctx.sessions?.list?.getSnapshot?.())
          if (path) set({ requested: { path } })
          try { ctx.layout?.selectPanel?.(PANEL_ID) } catch {}
        },
      }
    }

    // ---- React ----

    const MONO = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'
    const WARNING = 'var(--dsw-alias-state-warning-primary, #d97706)'
    const ERROR = 'var(--dsw-alias-state-error-primary, #d33)'
    const BORDER = '1px solid var(--dsw-alias-border-l2, rgba(128,128,128,.25))'
    const style = {
      page: { display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0, color: 'var(--dsw-alias-label-primary)' },
      toolbar: { display: 'flex', alignItems: 'center', gap: 12, padding: '10px 16px', borderBottom: BORDER, flexWrap: 'wrap' },
      title: { fontSize: 15, fontWeight: 600 },
      select: { fontSize: 13, padding: '3px 6px', maxWidth: 420, background: 'transparent', color: 'inherit', border: BORDER, borderRadius: 6 },
      notice: { fontSize: 12, lineHeight: '18px', color: 'var(--dsw-alias-label-secondary)', padding: '6px 16px', borderBottom: BORDER },
      tabs: { display: 'flex', alignItems: 'stretch', gap: 2, padding: '0 8px', borderBottom: BORDER, overflowX: 'auto', flex: '0 0 auto' },
      tab: { display: 'inline-flex', alignItems: 'center', gap: 6, padding: '6px 10px', fontSize: 13, cursor: 'pointer', borderBottom: '2px solid transparent', whiteSpace: 'nowrap', background: 'none', border: 'none', color: 'inherit' },
      tabActive: { borderBottom: '2px solid var(--dsw-alias-label-primary)', fontWeight: 600 },
      tabClose: { border: 'none', background: 'none', color: 'var(--dsw-alias-label-tertiary)', cursor: 'pointer', fontSize: 14, padding: '0 2px' },
      body: { flex: '1 1 auto', minHeight: 0, position: 'relative' },
      fill: { position: 'absolute', inset: 0, overflow: 'auto' },
      columns: { display: 'grid', gridTemplateColumns: 'repeat(5, minmax(200px, 1fr))', gap: 12, padding: 16, minWidth: 1080 },
      column: { display: 'flex', flexDirection: 'column', gap: 8, minWidth: 0 },
      columnHead: { fontSize: 12, fontWeight: 600, color: 'var(--dsw-alias-label-secondary)', textTransform: 'uppercase' },
      card: { display: 'flex', flexDirection: 'column', gap: 6, padding: 10, borderRadius: 8, border: BORDER, background: 'var(--dsw-alias-settings-card-fill, transparent)' },
      cardAttention: { borderColor: WARNING, boxShadow: `inset 3px 0 0 ${WARNING}` },
      cardTitle: { fontSize: 13, lineHeight: '18px', fontWeight: 600, wordBreak: 'break-word' },
      meta: { fontSize: 11, color: 'var(--dsw-alias-label-tertiary)', fontFamily: MONO, wordBreak: 'break-all' },
      row: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 6 },
      badge: { fontSize: 11, lineHeight: '18px', padding: '0 8px', borderRadius: 9, background: 'var(--dsw-alias-interactive-bg-hover, rgba(128,128,128,.12))', color: 'var(--dsw-alias-label-secondary)' },
      attention: { fontSize: 11, lineHeight: '18px', padding: '0 8px', borderRadius: 9, color: WARNING, background: `color-mix(in srgb, ${WARNING} 14%, transparent)` },
      action: { fontSize: 12, padding: '2px 10px', borderRadius: 6, border: BORDER, background: 'none', color: 'inherit', cursor: 'pointer' },
      actionDisabled: { opacity: 0.45, cursor: 'not-allowed' },
      message: { padding: 24, display: 'flex', flexDirection: 'column', gap: 10, fontSize: 13, lineHeight: '20px', maxWidth: 760 },
      code: { fontFamily: MONO, fontSize: 12, padding: '8px 10px', borderRadius: 6, background: 'var(--dsw-alias-bg-layer-2, rgba(128,128,128,.1))', whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0, maxHeight: 260, overflow: 'auto' },
      error: { color: ERROR },
      failure: { margin: '8px 16px', padding: 10, borderRadius: 8, border: `1px solid ${ERROR}`, display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 },
      terminal: { position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column', background: '#000' },
      termStatus: { fontSize: 11, padding: '3px 10px', color: '#bbb', background: '#111', flex: '0 0 auto' },
      termHost: { flex: '1 1 auto', minHeight: 0, padding: 4 },
    }

    const useStore = (store) => useSyncExternalStore(store.subscribe, store.getState)

    /** Resolves workspace paths to boards on the host; re-resolves while shown. */
    function useResolutions(paths) {
      const key = [...new Set(paths.filter(Boolean))].sort().join('\n')
      const [resolutions, setResolutions] = useState(null)
      useEffect(() => {
        let live = true
        let timer
        const load = async () => {
          const list = key === '' ? [] : key.split('\n')
          try {
            const query = list.map((path) => `path=${encodeURIComponent(path)}`).join('&')
            const answer = list.length ? await call('GET', `/boards?${query}`) : { boards: [] }
            if (live) setResolutions(answer.boards)
          } catch {}
          if (live) timer = setTimeout(load, RESOLVE_POLL_MS)
        }
        void load()
        return () => { live = false; clearTimeout(timer) }
      }, [key])
      return resolutions
    }

    /** Polls one board; keeps the snapshot object while change_token does not change. */
    function useBoard(project, nonce) {
      const [result, setResult] = useState({ project: null, snapshot: null, error: null })
      useEffect(() => {
        if (!project) return undefined
        let live = true
        let timer
        const load = async () => {
          try {
            const answer = await call('GET', `/board?project=${encodeURIComponent(project)}`)
            if (live) {
              setResult((previous) => {
                const snapshot = keepSnapshot(previous.project === project ? previous.snapshot : null, answer.snapshot)
                return previous.project === project && previous.snapshot === snapshot && previous.error === null
                  ? previous
                  : { project, snapshot, error: null }
              })
            }
          } catch (error) {
            if (live) setResult((previous) => ({ project, snapshot: previous.project === project ? previous.snapshot : null, error: error.info ?? { code: 'network', message: String(error?.message ?? error) } }))
          }
          if (live) timer = setTimeout(load, BOARD_POLL_MS)
        }
        void load()
        return () => { live = false; clearTimeout(timer) }
      }, [project, nonce])
      return result.project === project ? result : { project, snapshot: null, error: null }
    }

    function TerminalView({ pane, active, carrier, paneInfo, known, t }) {
      const host = useRef(null)
      const terminal = useRef(null)
      const [status, setStatus] = useState(carrier ? 'connecting' : 'otherHost')
      const [error, setError] = useState('')
      const [inputError, setInputError] = useState('')
      const activeRef = useRef(active)
      activeRef.current = active

      useEffect(() => {
        if (!carrier) {
          setStatus('otherHost')
          return undefined
        }
        let stopped = false
        const controller = new AbortController()
        let term
        let fit
        let observer
        const queue = new InputQueue(
          (message) => call('POST', '/input', { pane, ...message }),
          (failure) => { if (!stopped) setInputError(String(failure?.message ?? failure)) },
          // Input that arrived clears the notice; a size-only request says nothing about typed input.
          (message) => { if (!stopped && message.data) setInputError('') },
        )
        const sendSize = () => { if (term) void queue.push('', term.cols, term.rows) }
        const refit = () => {
          if (!term || !activeRef.current || !host.current?.offsetParent) return
          const before = `${term.cols}x${term.rows}`
          try { fit.fit() } catch {}
          if (`${term.cols}x${term.rows}` !== before) sendSize()
        }
        ;(async () => {
          const { Terminal, FitAddon } = await loadXterm()
          if (stopped) return
          term = new Terminal({ fontSize: 13, fontFamily: MONO, scrollback: 5000, allowProposedApi: true })
          fit = new FitAddon()
          term.loadAddon(fit)
          muteQueries(term)
          term.open(host.current)
          terminal.current = { term, refit, sendSize }
          term.onData((data) => { void queue.push(data, term.cols, term.rows) })
          if (typeof ResizeObserver === 'function') {
            observer = new ResizeObserver(() => refit())
            observer.observe(host.current)
          }
          refit()
          const response = await fetch(`${API}/stream?pane=${encodeURIComponent(pane)}`, { signal: controller.signal })
          if (!response.ok) {
            setStatus(response.status === 404 ? 'gone' : 'error')
            if (response.status !== 404) setError(`HTTP ${response.status}`)
            return
          }
          setStatus('live')
          const parser = new SseParser()
          const reader = response.body.getReader()
          const decoder = new TextDecoder()
          for (;;) {
            const { value, done } = await reader.read()
            if (done || stopped) break
            for (const event of parser.push(decoder.decode(value, { stream: true }))) {
              if (event.event === 'output') term.write(event.data)
              else if (event.event === 'exit') setStatus('exited')
            }
          }
          if (!stopped) setStatus((current) => (current === 'live' ? 'exited' : current))
        })().catch((failure) => {
          if (stopped) return
          setStatus('error')
          setError(String(failure?.message ?? failure))
        })
        return () => {
          stopped = true
          controller.abort()
          observer?.disconnect()
          terminal.current = null
          term?.dispose()
        }
      }, [pane, carrier])

      // A shown tab fits its container and hands its size to the PTY.
      useEffect(() => {
        if (!active || !terminal.current) return
        terminal.current.refit()
        terminal.current.sendSize()
        terminal.current.term.focus()
      }, [active, status])

      const { shown, text, inputText } = terminalStatus({ status, error, inputError, carrier, known, paneInfo }, t)
      return h('div', { style: { ...style.terminal, display: active ? 'flex' : 'none' }, 'data-testid': `kander-terminal-${pane}` },
        h('div', { style: style.termStatus, 'data-testid': `kander-terminal-status-${pane}`, 'data-status': shown }, `${pane} · ${text}`),
        inputText ? h('div', { style: { ...style.termStatus, color: '#f88' }, role: 'alert', 'data-testid': `kander-terminal-input-error-${pane}` }, inputText) : null,
        h('div', { ref: host, style: style.termHost, 'data-testid': `kander-terminal-screen-${pane}` }))
    }

    function AgentLine({ task, pane, carrier, live, onOpen, t }) {
      const address = dshWindow(task.window)
      const items = []
      if (task.agent) {
        items.push(h('span', { key: 'status', style: style.badge }, task.agent.status + (task.agent.runtime_state && task.agent.runtime_state !== 'unknown' ? ` · ${task.agent.runtime_state}` : '')))
      }
      if (address && (task.state === 'working' || task.state === 'review')) {
        if (!carrier) {
          items.push(h('span', { key: 'other', style: style.badge }, t('card.otherHost')))
        } else if (pane) {
          items.push(h('button', { key: 'open', style: style.action, 'data-testid': `kander-open-${task.task_id}`, onClick: () => onOpen(task, address.pane) }, t('card.terminal')))
        } else if (live) {
          items.push(h('span', { key: 'gone', style: style.badge }, t('card.noPane')))
        }
      }
      return items.length ? h('div', { style: style.row }, items) : null
    }

    function Card({ task, panes, carrier, live, onStart, onOpen, t }) {
      const address = dshWindow(task.window)
      const pane = address ? panes.get(address.pane) : undefined
      const attention = needsAttention(task, pane)
      return h('div', { style: attention ? { ...style.card, ...style.cardAttention } : style.card, 'data-testid': `kander-card-${task.task_id}` },
        h('div', { style: style.cardTitle }, task.title || task.task_id),
        h('div', { style: style.meta }, [task.task_id, task.size, task.launcher].filter(Boolean).join(' · ')),
        attention ? h('div', { style: style.row }, h('span', { style: style.attention, 'data-testid': `kander-attention-${task.task_id}` }, t('card.attention'))) : null,
        h(AgentLine, { task, pane, carrier, live, onOpen, t }),
        task.state === 'todo'
          ? h('div', { style: style.row },
            h('button', { style: carrier ? style.action : { ...style.action, ...style.actionDisabled }, disabled: !carrier, title: carrier ? undefined : t('card.otherHost'), 'data-testid': `kander-start-${task.task_id}`, onClick: () => onStart(task) }, t('card.start')))
          : null)
    }

    function BoardTab({ board, panes, carrier, live, onStart, onOpen, t }) {
      if (board.error?.code === 'kander-not-found') {
        return h('div', { style: style.message },
          h('div', { style: style.error }, t('view.kanderMissing')),
          board.error.searched?.length ? h('pre', { style: style.code }, t('view.kanderSearched', { paths: board.error.searched.join('\n') })) : null,
          h('div', null, t('view.kanderConfig')))
      }
      if (board.error && !board.snapshot) {
        return h('div', { style: style.message },
          h('div', { style: style.error }, board.error.code === 'network' ? t('view.network', { error: board.error.message }) : t('view.kanderFailed')),
          board.error.code === 'network' ? null : h('pre', { style: style.code }, board.error.stderr || board.error.message))
      }
      if (!board.snapshot) return h('div', { style: style.message }, t('view.loading'))
      return h('div', { style: style.columns, 'data-testid': 'kander-columns' },
        columnsOf(board.snapshot).map((column) => h('div', { key: column.state, style: style.column, 'data-testid': `kander-column-${column.state}` },
          h('div', { style: style.columnHead }, `${t(`column.${column.state}`)} · ${column.tasks.length}`),
          column.tasks.map((task) => h(Card, { key: task.task_id, task, panes, carrier, live, onStart, onOpen, t })))))
    }

    function StartDialog({ pending, onCancel, onConfirm, t }) {
      if (!pending) return null
      const vars = { task: pending.task.task_id, project: pending.project }
      return h(Modal, {
        open: true,
        onClose: onCancel,
        title: t('start.title', vars),
        closeLabel: t('start.close'),
        footer: h(React.Fragment, null,
          h(Button, { variant: 'outline', onClick: onCancel }, t('start.cancel')),
          h(Button, { variant: 'primary', onClick: onConfirm, 'data-modal-autofocus': true, 'data-testid': 'kander-start-confirm' }, t('start.confirm'))),
      }, h('div', { style: { fontSize: 13, lineHeight: '20px' } }, t('start.body', vars)))
    }

    // The notice for a host that does not hold the socket, by the carrier
    // reason of KanderHost.carrierStatus().
    function carrierNotice(live, t) {
      if (!live || live.carrier?.carrier) return null
      const { reason, holderPid, socket } = live.carrier ?? {}
      switch (reason) {
        case 'held': return holderPid ? t('carrier.other', { pid: holderPid }) : t('carrier.otherUnknown')
        case 'waiting': case 'starting': case 'stopped': return t(`carrier.${reason}`)
        case 'blocked': case 'unreachable': return t(`carrier.${reason}`, { socket: socket ?? 'mux.sock' })
        default: return t('carrier.none', { reason: reason ?? 'unknown' })
      }
    }

    function Page({ store, t: slotT }) {
      const state = useStore(store)
      const t = typeof slotT === 'function' ? slotT : translator(language())
      const requestedPath = state.requested?.path ?? null
      const resolutions = useResolutions([...state.workspacePaths, state.currentPath, requestedPath])
      const projectOfPath = (path) => resolutions?.find((item) => item.path === path)?.project ?? null
      const boards = useMemo(() => boardsOf(resolutions), [resolutions])
      const project = state.requested?.project ?? (requestedPath ? projectOfPath(requestedPath) : projectOfPath(state.currentPath))
      const [nonce, setNonce] = useState(0)
      const board = useBoard(project, nonce)
      const [pending, setPending] = useState(null)
      const [starting, setStarting] = useState(null)
      const [failure, setFailure] = useState(null)
      const [opening, setOpening] = useState(null)

      const live = state.live
      const carrier = live?.carrier?.carrier === true
      const panes = useMemo(() => new Map((live?.panes ?? []).map((pane) => [pane.pane, pane])), [live])
      const tabs = project ? store.tabsOf(project) : EMPTY_TABS

      const open = useCallback((task, pane) => {
        if (project) store.setTabs(project, openTab(store.tabsOf(project), { pane, task: task.task_id, title: task.title }))
      }, [project, store])

      // After a start, the new agent's tab opens once the board shows its dsh WINDOW.
      useEffect(() => {
        if (!opening || opening.project !== project || !board.snapshot) return
        const task = board.snapshot.tasks.find((item) => item.task_id === opening.task)
        const address = dshWindow(task?.window)
        if (address && panes.has(address.pane)) {
          open(task, address.pane)
          setOpening(null)
        }
      }, [opening, project, board.snapshot, panes, open])

      const confirmStart = async () => {
        const target = pending
        setPending(null)
        setStarting(target.task.task_id)
        setFailure(null)
        try {
          const result = await call('POST', '/start', { project: target.project, task: target.task.task_id })
          if (result.ok) setOpening({ project: target.project, task: target.task.task_id })
          else setFailure({ task: target.task.task_id, output: [result.stderr, result.stdout].filter((text) => text?.trim()).join('\n') || `exit ${result.code}` })
        } catch (error) {
          setFailure({ task: target.task.task_id, output: error.info?.message ?? String(error?.message ?? error) })
        } finally {
          setStarting(null)
          setNonce((value) => value + 1)
        }
      }

      const switcher = boards.length
        ? h('select', {
          style: style.select,
          value: project ?? '',
          'aria-label': t('view.switch'),
          'data-testid': 'kander-board-switcher',
          onChange: (event) => store.request(event.target.value ? { project: event.target.value } : null),
        },
        project && !boards.some((item) => item.project === project) ? h('option', { value: project }, project) : null,
        project ? null : h('option', { value: '' }, '—'),
        boards.map((item) => h('option', { key: item.project, value: item.project }, item.project)))
        : null

      const notice = carrierNotice(live, t)
      let content
      if (!project) {
        const path = requestedPath ?? state.currentPath
        content = !path
          ? h('div', { style: style.message }, t('view.noWorkspace'))
          : resolutions === null
            ? h('div', { style: style.message }, t('view.loading'))
            : h('div', { style: style.message, 'data-testid': 'kander-no-board' },
              h('div', null, t('view.noBoard', { path })),
              h('div', null, t('view.noBoardHint')),
              h('pre', { style: style.code }, 'kander init'),
              boards.length ? h('div', null, t('view.otherBoards')) : null)
      }

      return h('div', { style: style.page, 'data-testid': 'kander-view' },
        h('div', { style: style.toolbar },
          h('span', { style: style.title }, t('view.title')),
          switcher,
          starting ? h('span', { style: style.badge }, t('start.running', { task: starting })) : null),
        notice ? h('div', { style: style.notice, 'data-testid': 'kander-carrier-notice' }, notice) : null,
        state.liveError ? h('div', { style: { ...style.notice, ...style.error } }, t('view.network', { error: state.liveError })) : null,
        failure ? h('div', { style: style.failure, 'data-testid': 'kander-start-failure' },
          h('div', { style: style.error }, t('start.failed', { task: failure.task })),
          h('pre', { style: style.code }, failure.output),
          h('div', null, h('button', { style: style.action, onClick: () => setFailure(null) }, t('start.dismiss')))) : null,
        content ?? h(React.Fragment, null,
          h('div', { style: style.tabs, role: 'tablist' },
            h('button', {
              style: tabs.active === 'board' ? { ...style.tab, ...style.tabActive } : style.tab,
              role: 'tab',
              'data-testid': 'kander-tab-board',
              onClick: () => store.setTabs(project, { ...tabs, active: 'board' }),
            }, t('view.board')),
            tabs.tabs.map((tab) => h('span', {
              key: tab.pane,
              role: 'tab',
              style: tabs.active === tab.pane ? { ...style.tab, ...style.tabActive } : style.tab,
              'data-testid': `kander-tab-${tab.pane}`,
              onClick: () => store.setTabs(project, { ...tabs, active: tab.pane }),
            },
            h('span', { title: tabLabel(tab, board.snapshot).title }, tabLabel(tab, board.snapshot).name),
            h('button', {
              style: style.tabClose,
              'aria-label': t('tab.close'),
              'data-testid': `kander-tab-close-${tab.pane}`,
              onClick: (event) => { event.stopPropagation(); store.setTabs(project, closeTab(tabs, tab.pane)) },
            }, '×')))),
          h('div', { style: style.body },
            h('div', { style: { ...style.fill, display: tabs.active === 'board' ? 'block' : 'none' } },
              h(BoardTab, { board, panes, carrier, live: live !== null, onStart: (task) => setPending({ task, project }), onOpen: open, t })),
            tabs.tabs.map((tab) => h(TerminalView, {
              key: tab.pane,
              pane: tab.pane,
              active: tabs.active === tab.pane,
              carrier,
              paneInfo: panes.get(tab.pane),
              known: live !== null,
              t,
            })))),
        h(StartDialog, { pending, onCancel: () => setPending(null), onConfirm: confirmStart, t }))
    }

    function HeaderButton({ sessionId, store, t: slotT }) {
      const t = typeof slotT === 'function' ? slotT : translator(language())
      return h('button', {
        'data-testid': 'kander-header-button',
        title: t('header.open'),
        'aria-label': t('header.open'),
        onClick: () => store.openForSession(sessionId),
        style: { fontSize: 12, padding: '2px 8px', borderRadius: 6, border: BORDER, background: 'none', color: 'inherit', cursor: 'pointer' },
      }, '▦ Kander')
    }

    function Icon({ size }) {
      return h('span', { style: { fontSize: size || 16, lineHeight: 1 } }, '▦')
    }

    function apply(ctx) {
      let storage
      try { storage = window.localStorage } catch {}
      localeRuntime = ctx.locale
      const store = createStore(ctx, storage)
      ctx.effect(() => store.start(), 'kander-dsh.store')
      if (ctx.locale?.register) ctx.effect(() => ctx.locale.register(NS, { en, zh }), 'kander-dsh.locale')
      ctx.slots.inject('main', () => ctx.slots.register({ name: 'main', key: PANEL_ID, locale: NS, inject: () => ({ store }) }, Page))
      ctx.slots.inject('sidebar.panellist', () => ctx.slots.register({ name: 'sidebar.panellist', id: PANEL_ID, order: 50, label: () => 'Kander' }, Icon))
      ctx.slots.inject('conversation.session.header.utilities', () => ctx.slots.register({
        name: 'conversation.session.header.utilities',
        id: 'kander-dsh',
        order: -10,
        locale: NS,
        inject: (sessionId) => ({ sessionId, store }),
      }, HeaderButton))
    }

    exports.name = 'kander-dsh'
    exports.inject = ['slots', 'locale', 'layout', 'sessions', 'workspaces']
    exports.apply = apply
    exports.logic = {
      COLUMNS, InputQueue, SseParser, boardsOf, carrierNotice, closeTab, columnsOf, createStore, dshWindow, keepSnapshot,
      loadTabs, mainSessionId, muteQueries, needsAttention, newFocus, openTab, saveTabs, tabLabel, terminalStatus, translator,
      workspacePathOf,
    }
    return module.exports
  },
})
