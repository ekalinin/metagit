import type { ArchiveEntry, EventKind, ProjectEvent } from '../api'
import { formatTime } from '../ui'

const eventLabels: Record<EventKind, string> = {
  delivery_error: 'ошибка доставки',
  diverged: 'расхождение',
  archive_push: 'архив при push',
}

export function EventLog({ events }: { events: ProjectEvent[] }) {
  return (
    <section className="card">
      <h2>Журнал событий</h2>
      {events.length === 0 ? (
        <p className="muted">Событий пока нет.</p>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Время</th>
                <th>Событие</th>
                <th>Target</th>
                <th>Ref</th>
                <th>Архивный ref</th>
                <th>Сообщение</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr key={e.id}>
                  <td className="nowrap">{formatTime(e.created_at)}</td>
                  <td>
                    <span className={`badge badge-event-${e.kind}`}>{eventLabels[e.kind] ?? e.kind}</span>
                  </td>
                  <td>{e.target_name}</td>
                  <td>{e.ref && <code>{e.ref}</code>}</td>
                  <td>{e.archive_ref && <code>{e.archive_ref}</code>}</td>
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
  return (
    <section className="card">
      <h2>Архив</h2>
      {entries.length === 0 ? (
        <p className="muted">Архив пуст.</p>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Время</th>
                <th>Источник</th>
                <th>Исходный ref</th>
                <th>Объект</th>
                <th>Команда для получения</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((a) => (
                <tr key={a.id}>
                  <td className="nowrap">{formatTime(a.created_at)}</td>
                  <td>{a.source === 'push' ? 'push' : a.target_name}</td>
                  <td>
                    <code>{a.orig_ref}</code>
                  </td>
                  <td>
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
