// Pane and container IDs end up in a card's WINDOW, which outlives the host
// that created them. A WINDOW must never name a later pane, so the sequence
// number is persisted per DSH_HOME and survives host restarts and socket
// handovers, and the host pid separates two hosts that read the same number
// during a handover.
import { readFileSync, renameSync, writeFileSync } from 'node:fs'

export class PaneSequence {
  constructor(path, pid = process.pid) {
    this.path = path
    this.pid = pid
  }

  /** Returns the next {pane, container} pair and persists the number first. */
  next() {
    const number = this.read() + 1
    const temporary = `${this.path}.${this.pid}.tmp`
    writeFileSync(temporary, `${number}\n`, { mode: 0o600 })
    renameSync(temporary, this.path)
    return { pane: `p${number}-${this.pid}`, container: `c${number}-${this.pid}` }
  }

  read() {
    let text
    try {
      text = readFileSync(this.path, 'utf8')
    } catch (error) {
      if (error?.code === 'ENOENT') return 0
      throw error
    }
    // A damaged file could restart the sequence and reuse IDs; refuse instead.
    const value = text.trim()
    if (!/^[0-9]+$/.test(value) || !Number.isSafeInteger(Number(value))) {
      throw new Error(`${this.path} does not hold a pane sequence number`)
    }
    return Number(value)
  }
}
