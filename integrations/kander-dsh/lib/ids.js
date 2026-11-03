// Pane and container IDs end up in a card's WINDOW, which outlives the host
// that created them. A WINDOW must never name a later pane, so the sequence
// number is persisted per DSH_HOME and survives host restarts and socket
// handovers, and the host pid is part of the ID as well.
//
// Only the carrier creates panes, and the carrier lock lets one host at a time
// be the carrier, so the read and the write of the number never interleave
// between hosts. A host also never goes below the last number it took itself.
import { randomBytes } from 'node:crypto'
import { readFileSync, renameSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

export class PaneSequence {
  /** @param directory - the plugin state directory. */
  constructor(directory, pid = process.pid) {
    this.path = join(directory, 'sequence')
    this.pid = pid
    this.last = 0
  }

  /** Returns the next {pane, container} pair; the number is recorded before it is returned. */
  next() {
    const number = Math.max(this.read(), this.last) + 1
    const temporary = `${this.path}.${process.pid}.${randomBytes(4).toString('hex')}.tmp`
    writeFileSync(temporary, `${number}\n`, { mode: 0o600 })
    renameSync(temporary, this.path)
    this.last = number
    return { pane: `p${number}-${this.pid}`, container: `c${number}-${this.pid}` }
  }

  /** The last recorded number; 0 before the first. */
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
