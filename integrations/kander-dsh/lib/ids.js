// Pane and container IDs end up in a card's WINDOW, which outlives the host
// that created them. A WINDOW must never name a later pane, so the sequence
// number is persisted per DSH_HOME and survives host restarts and socket
// handovers, and the host pid is part of the ID as well.
//
// A number belongs to the process that creates the empty file <taken>/<n>
// with O_EXCL, so two processes, even two carrier hosts during a handover,
// never get the same number. The file `sequence` only says where to start
// looking; a stale or regressed value costs a few failed creations, never a
// reused number. Marks far below the newest number are removed: a search
// starts at the last recorded number, which never lags that far behind.
import { randomBytes } from 'node:crypto'
import { closeSync, mkdirSync, openSync, readFileSync, renameSync, unlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const KEEP = 64
const ATTEMPTS = 100000

export class PaneSequence {
  /** @param directory - the plugin state directory. */
  constructor(directory, pid = process.pid) {
    this.hintPath = join(directory, 'sequence')
    this.taken = join(directory, 'taken')
    this.pid = pid
  }

  /** Returns the next {pane, container} pair; the number is taken before it is returned. */
  next() {
    mkdirSync(this.taken, { recursive: true, mode: 0o700 })
    let number = this.read() + 1
    for (let attempt = 0; attempt < ATTEMPTS; attempt++, number++) {
      let fd
      try {
        fd = openSync(join(this.taken, String(number)), 'wx', 0o600)
      } catch (error) {
        if (error?.code === 'EEXIST') continue
        throw error
      }
      closeSync(fd)
      this.record(number)
      return { pane: `p${number}-${this.pid}`, container: `c${number}-${this.pid}` }
    }
    throw new Error(`could not take a pane number in ${this.taken}`)
  }

  /** The last recorded number; 0 before the first. */
  read() {
    let text
    try {
      text = readFileSync(this.hintPath, 'utf8')
    } catch (error) {
      if (error?.code === 'ENOENT') return 0
      throw error
    }
    const value = text.trim()
    if (!/^[0-9]+$/.test(value) || !Number.isSafeInteger(Number(value))) {
      throw new Error(`${this.hintPath} does not hold a pane sequence number`)
    }
    return Number(value)
  }

  record(number) {
    const temporary = `${this.hintPath}.${process.pid}.${randomBytes(4).toString('hex')}.tmp`
    writeFileSync(temporary, `${number}\n`, { mode: 0o600 })
    renameSync(temporary, this.hintPath)
    try { unlinkSync(join(this.taken, String(number - KEEP))) } catch {}
  }
}
