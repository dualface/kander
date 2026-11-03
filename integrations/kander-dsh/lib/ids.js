// Pane and container IDs end up in a card's WINDOW, which outlives the host
// that created them. A WINDOW must never name a later pane, so the sequence
// number is persisted per DSH_HOME and survives host restarts and socket
// handovers, and the host pid is part of the ID as well.
//
// Reading the last number and recording the next one happen under the lock
// the hosts of a DSH_HOME share, so two hosts never take the same number and
// the recorded number never goes back.
import { randomBytes } from 'node:crypto'
import { readFileSync, renameSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

export class PaneSequence {
  /**
   * @param directory - the plugin state directory.
   * @param lock - the DirectoryLock of that directory.
   */
  constructor(directory, lock, pid = process.pid) {
    this.path = join(directory, 'sequence')
    this.lock = lock
    this.pid = pid
  }

  /** Resolves the next {pane, container} pair; the number is recorded before it is returned. */
  next() {
    return this.lock.run(() => {
      const number = this.read() + 1
      const temporary = `${this.path}.${process.pid}.${randomBytes(4).toString('hex')}.tmp`
      writeFileSync(temporary, `${number}\n`, { mode: 0o600 })
      renameSync(temporary, this.path)
      return { pane: `p${number}-${this.pid}`, container: `c${number}-${this.pid}` }
    })
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
