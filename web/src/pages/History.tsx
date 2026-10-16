import type { ArchiveEntry, EventKind, ProjectEvent } from '../api'
import { useT } from '../locale'
import { formatTime, Lamp, type LampTone } from '../ui'

const eventTones: Record<EventKind, LampTone> = {
  delivery_error: 'err',
  diverged: 'wait',
  archive_push: 'neutral',
}

export function EventLog({ events }: { events: ProjectEvent[] }) {
  const text = useT()
  return (
    <section className="card">
      <h2>{text.history.eventLog}</h2>
      {events.length === 0 ? (
        <p className="muted">{text.history.noEvents}</p>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{text.history.time}</th>
                <th>{text.history.event}</th>
                <th>Target</th>
                <th>Ref</th>
                <th>{text.history.archiveRef}</th>
                <th>{text.history.message}</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr key={e.id}>
                  <td className="nowrap mono">{formatTime(e.created_at)}</td>
                  <td>
                    <span className={`state event-${e.kind}`}>
                      <Lamp tone={eventTones[e.kind] ?? 'neutral'} />
                      {text.labels.events[e.kind] ?? e.kind}
                    </span>
                  </td>
                  <td>{e.target_name}</td>
                  <td className="ref">{e.ref && <code>{e.ref}</code>}</td>
                  <td className="ref">{e.archive_ref && <code>{e.archive_ref}</code>}</td>
                  <td className="message-cell">{e.message}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

export function ArchiveList({ entries }: { entries: ArchiveEntry[] }) {
  const text = useT()
  return (
    <section className="card">
      <h2>{text.history.archive}</h2>
      {entries.length === 0 ? (
        <p className="muted">{text.history.archiveEmpty}</p>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{text.history.time}</th>
                <th>{text.history.source}</th>
                <th>{text.history.origRef}</th>
                <th>{text.history.object}</th>
                <th>{text.history.fetchCommand}</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((a) => (
                <tr key={a.id}>
                  <td className="nowrap mono">{formatTime(a.created_at)}</td>
                  <td>{a.source === 'push' ? 'push' : a.target_name}</td>
                  <td className="ref">
                    <code>{a.orig_ref}</code>
                  </td>
                  <td className="nowrap">
                    <code title={a.object}>{a.object.slice(0, 12)}</code>
                  </td>
                  <td>
                    <code className="block">{a.fetch_command}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
