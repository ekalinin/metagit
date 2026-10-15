import { cloneElement, useId, type ReactElement, type ReactNode } from 'react'
import type { AuthKind, TargetState } from './api'

interface FieldProps {
  label: string
  hint?: ReactNode
  children: ReactElement<{ id?: string; 'aria-describedby'?: string }>
}

// Field renders a labelled form control with an optional hint linked to it.
export function Field({ label, hint, children }: FieldProps) {
  const id = useId()
  const hintId = hint ? id + '-hint' : undefined
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {cloneElement(children, { id, 'aria-describedby': hintId })}
      {hint && (
        <span id={hintId} className="hint">
          {hint}
        </span>
      )}
    </div>
  )
}

export function formatTime(value?: string): string {
  if (!value) return ''
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? value : d.toLocaleString('ru-RU')
}

const stateLabels: Record<TargetState, string> = {
  synced: 'синхронизирован',
  pending: 'ожидает',
  error: 'ошибка',
}

export function stateLabel(state: TargetState): string {
  return stateLabels[state] ?? state
}

// The signal color of a lamp: ok, wait and err are states, neutral is an event
// without a state, off is a disabled target.
export type LampTone = 'ok' | 'wait' | 'err' | 'neutral' | 'off'

const stateTones: Record<TargetState, LampTone> = {
  synced: 'ok',
  pending: 'wait',
  error: 'err',
}

export function stateTone(state: TargetState): LampTone {
  return stateTones[state] ?? 'neutral'
}

// Lamp only repeats a state that is also written next to it, so it is hidden
// from assistive technologies.
export function Lamp({ tone, title }: { tone: LampTone; title?: string }) {
  return <span className={`lamp lamp-${tone}`} title={title} aria-hidden="true" />
}

export function StateBadge({ state }: { state: TargetState }) {
  return <span className={`state state-${state}`}>{stateLabel(state)}</span>
}

const authKindLabels: Record<AuthKind, string> = {
  none: 'без учетных данных',
  https: 'HTTPS: логин и токен',
  ssh: 'SSH: приватный ключ',
}

export function authKindLabel(kind: AuthKind): string {
  return authKindLabels[kind] ?? kind
}

export function ErrorMessage({ text }: { text: string }) {
  if (!text) return null
  return (
    <p className="message message-error" role="alert">
      {text}
    </p>
  )
}

export function Notice({ text }: { text: string }) {
  if (!text) return null
  return (
    <p className="message message-ok" role="status">
      {text}
    </p>
  )
}
