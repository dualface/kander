# Live TrueColor terminal walkthrough (QA-02)

Record of the human-acceptance walk required by the theme-presets card
and the palette card's Solarized four-combination check. Sequence
assertions and in-process `App.View()` grids do not close that gate.

## Terminal

- Herdr 0.9.0 pane, `COLORTERM=truecolor`, `TERM=xterm-256color`
- Geometry after `terminal session control`: 140 columns × 40 rows
- Binary: task-branch `kander` at `73afc16b8d74` (dev build)
- Isolated `KANDER_CONFIG` / `KANBAN_DIR` (fixture board, one backlog card)
- Theme cycle via the `t` key; surfaces via `?`, `s`, `o`, Enter
- Solarized 16-color palette applied with OSC 4/10/11 on that pane only

`herdr pane read --format ansi --source visible` keeps `48;2;r;g;b`
(TrueColor). The live dump contains the Chinese UI strings (`看板`,
`界面`, `启动所选任务`). Offline PNG rasterization used DejaVu Sans
Mono, so CJK code points render as missing-glyph boxes; that is a
font gap in the rasterizer, not a TUI fault.

## Six named themes × five surfaces

Each canvas center matched the theme `Bg` gold value. TrueColor
backgrounds only; Solarized `#fdf6e3` / `#002b36` pixel count was 0.

| Theme | Bg | board | detail | options | help | start |
| --- | --- | --- | --- | --- | --- | --- |
| light | `#fafafa` | 0.925 | 0.912 | 0.912 | 0.904 | 0.914 |
| light-warm | `#f3ead8` | 0.925 | 0.912 | 0.912 | 0.904 | 0.914 |
| light-contrast | `#ffffff` | 0.925 | 0.913 | 0.912 | 0.904 | 0.914 |
| dark | `#16181d` | 0.925 | 0.912 | 0.912 | 0.904 | 0.914 |
| dark-soft | `#1c2230` | 0.925 | 0.912 | 0.912 | 0.904 | 0.914 |
| dark-contrast | `#000000` | 0.925 | 0.912 | 0.911 | 0.904 | 0.914 |

Ratios are the fraction of rasterized pixels equal to that theme `Bg`
(±2). Chrome, borders, and status bar account for the remainder.
Chrome followed the table (`light` `#6b21a8`, `light-warm` `#6b4423`,
`light-contrast` `#3d0066`, `dark-soft` `#4a3a69`, `dark-contrast`
`#490080`). Foreground text (`KANDER`, task id, key chords, Markdown
headings) stayed readable on the canvas.

## Solarized four combinations (board + detail)

OSC 4 set the 16 ANSI slots to Solarized; OSC 11 set the default
background to Light `#fdf6e3` or Dark `#002b36`.

| Terminal | Theme | Center | Solarized Light px | Solarized Dark px |
| --- | --- | --- | --- | --- |
| Solarized Light | light | `#fafafa` | 0 | 0 |
| Solarized Light | dark | `#16181d` | 0 | 0 |
| Solarized Dark | light | `#fafafa` | 0 | 0 |
| Solarized Dark | dark | `#16181d` | 0 | 0 |

The selected theme kept its own hex canvas. It did not follow the
terminal 0/15 palette and did not invert with the host scheme.

The palette card `20260909-tui-truecolor-palette-task` IMPLEMENTATION
now cites this walkthrough for its Solarized four-combination
acceptance item. PTY sequence tests remain auxiliary evidence. The
six-theme five-surface matrix stays on the presets card.
