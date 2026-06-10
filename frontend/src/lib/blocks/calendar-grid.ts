// Calendar month-grid render core, shared by the editor block
// (components/app/milkdown-calendar.ts nodeView) and the read-only view
// renderer (components/view/MarkdownView.tsx) — same lib/diagrams idea: one
// Milkdown-free DOM builder so view and edit render the grid identically.

const MONTHS = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
]
const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

export const pad2 = (n: number) => String(n).padStart(2, '0')

// `YYYY-MM-DD Title` event line, one per top-level list item.
export const CALENDAR_EVENT_RE = /^(\d{4}-\d{2}-\d{2})\s+(.+)$/

export function buildCalendarGrid(
  month: string,
  events: Map<string, string[]>,
): HTMLElement {
  const grid = document.createElement('div')
  grid.className = 'tela-calendar-grid'
  grid.setAttribute('contenteditable', 'false')

  const m = /^(\d{4})-(\d{2})$/.exec(month.trim())
  if (!m || +m[2] < 1 || +m[2] > 12) {
    grid.classList.add('tela-calendar-invalid')
    grid.textContent = 'Set a month — :::calendar{month=YYYY-MM}'
    return grid
  }
  const year = +m[1]
  const mon = +m[2] - 1

  const caption = document.createElement('div')
  caption.className = 'tela-calendar-caption'
  caption.textContent = `${MONTHS[mon]} ${year}`
  grid.appendChild(caption)

  const table = document.createElement('table')
  table.className = 'tela-calendar-table'

  const thead = document.createElement('thead')
  const headRow = document.createElement('tr')
  for (const w of WEEKDAYS) {
    const th = document.createElement('th')
    th.textContent = w
    headRow.appendChild(th)
  }
  thead.appendChild(headRow)
  table.appendChild(thead)

  const tbody = document.createElement('tbody')
  const firstDow = new Date(Date.UTC(year, mon, 1)).getUTCDay()
  const daysInMonth = new Date(Date.UTC(year, mon + 1, 0)).getUTCDate()
  const todayIso = new Date().toISOString().slice(0, 10)

  let day = 1 - firstDow
  while (day <= daysInMonth) {
    const tr = document.createElement('tr')
    for (let i = 0; i < 7; i++) {
      const td = document.createElement('td')
      td.className = 'tela-calendar-cell'
      if (day >= 1 && day <= daysInMonth) {
        const iso = `${year}-${pad2(mon + 1)}-${pad2(day)}`
        if (iso === todayIso) td.dataset.today = 'true'
        const num = document.createElement('div')
        num.className = 'tela-calendar-daynum'
        num.textContent = String(day)
        td.appendChild(num)
        const dayEvents = events.get(iso)
        if (dayEvents) {
          for (const title of dayEvents) {
            const chip = document.createElement('div')
            chip.className = 'tela-calendar-event'
            chip.textContent = title
            chip.title = title
            td.appendChild(chip)
          }
        }
      } else {
        td.dataset.out = 'true'
      }
      tr.appendChild(td)
      day++
    }
    tbody.appendChild(tr)
  }
  table.appendChild(tbody)
  grid.appendChild(table)
  return grid
}
