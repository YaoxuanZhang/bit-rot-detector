import { useEffect, useState, useCallback } from 'react'
import { api, type SettingsResponse, type ScheduleEntry } from '../lib/api'

interface Props {
  expanded: boolean
  onExpand: () => void
  onCollapse: () => void
}

export default function SettingsCard({ expanded, onExpand, onCollapse }: Props) {
  const [settings, setSettings] = useState<SettingsResponse | null>(null)
  const [schedule, setSchedule] = useState<ScheduleEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testResult, setTestResult] = useState('')
  const [error, setError] = useState('')

  // local form states
  const [warnPct, setWarnPct] = useState(80)
  const [errorPct, setErrorPct] = useState(90)
  const [notifyCorruption, setNotifyCorruption] = useState(true)
  const [notifyError, setNotifyError] = useState(true)
  const [notifyWarn, setNotifyWarn] = useState(false)
  const [notifyCompletion, setNotifyCompletion] = useState(false)
  const [scheduleEdits, setScheduleEdits] = useState<Record<string, ScheduleEntry>>({})

  const load = useCallback(() => {
    setLoading(true)
    Promise.all([api.getSettings(), api.getSchedule()])
      .then(([s, sc]) => {
        setSettings(s)
        setSchedule(sc)
        setWarnPct(s.disk_thresholds.warn_pct)
        setErrorPct(s.disk_thresholds.error_pct)
        setNotifyCorruption(s.notification_rules.on_corruption)
        setNotifyError(s.notification_rules.on_error)
        setNotifyWarn(s.notification_rules.on_warn)
        setNotifyCompletion(s.notification_rules.on_completion)
      })
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { if (expanded) load() }, [expanded, load])

  // Collapsed: load just enough for summary
  useEffect(() => {
    if (!expanded) {
      api.getSettings().then(s => setSettings(s)).catch(() => {})
      api.getSchedule().then(s => setSchedule(s)).catch(() => {})
    }
  }, [expanded])

  async function saveThresholds() {
    if (!settings) return
    setSaving(true)
    try {
      const updated = await api.postSettings({
        ...settings,
        disk_thresholds: { warn_pct: warnPct, error_pct: errorPct },
      })
      setSettings(updated)
    } catch (e) { setError(String(e)) }
    finally { setSaving(false) }
  }

  async function saveNotifications() {
    if (!settings) return
    setSaving(true)
    try {
      const updated = await api.postSettings({
        ...settings,
        notification_rules: {
          on_corruption: notifyCorruption,
          on_error: notifyError,
          on_warn: notifyWarn,
          on_completion: notifyCompletion,
        },
      })
      setSettings(updated)
    } catch (e) { setError(String(e)) }
    finally { setSaving(false) }
  }

  async function testEmail() {
    setSaving(true)
    setTestResult('')
    try {
      const res = await api.postTestEmail()
      setTestResult(res.ok ? '✓ Email sent' : `Error ${res.status}`)
    } catch (e) { setTestResult(String(e)) }
    finally { setSaving(false) }
  }

  async function saveScheduleEntry(entry: ScheduleEntry) {
    setSaving(true)
    try {
      await api.postSchedule(entry)
      await load()
    } catch (e) { setError(String(e)) }
    finally { setSaving(false) }
  }

  function getScheduleEdit(entry: ScheduleEntry): ScheduleEntry {
    return scheduleEdits[entry.id] ?? entry
  }

  const nextSchedule = schedule.find(s => s.enabled)

  const notifyOn = [
    settings?.notification_rules.on_corruption && 'corruption',
    settings?.notification_rules.on_error && 'error',
    settings?.notification_rules.on_warn && 'warn',
    settings?.notification_rules.on_completion && 'completion',
  ].filter(Boolean).join(', ') || 'none'

  function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
    return (
      <label className="toggle">
        <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} />
        <span className="toggle-track"><span className="toggle-thumb" /></span>
      </label>
    )
  }

  return (
    <div
      className={`card${expanded ? ' expanded' : ''}`}
      onClick={!expanded ? onExpand : undefined}
      onContextMenu={e => { if (expanded) { e.preventDefault(); onCollapse() } }}
    >
      <div className="card-header">
        <span className="card-title">Settings</span>
        <button className="card-close-btn" onClick={e => { e.stopPropagation(); onCollapse() }} title="Collapse (Esc)">✕</button>
      </div>

      {!expanded ? (
        /* ── Collapsed summary ── */
        <div>
          <div style={{ fontSize: '0.85rem', marginBottom: '0.4rem' }}>
            <span style={{ color: 'var(--muted)' }}>Next run: </span>
            {nextSchedule ? <code style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem' }}>{nextSchedule.cron_expr}</code> : '—'}
          </div>
          <div style={{ fontSize: '0.85rem', marginBottom: '0.4rem' }}>
            <span style={{ color: 'var(--muted)' }}>Notify on: </span>{notifyOn}
          </div>
          {settings && (
            <div style={{ fontSize: '0.85rem' }}>
              <span style={{ color: 'var(--muted)' }}>Disk: </span>
              warn {settings.disk_thresholds.warn_pct}% · err {settings.disk_thresholds.error_pct}%
            </div>
          )}
        </div>
      ) : (
        /* ── Expanded view ── */
        <div onClick={e => e.stopPropagation()}>
          {error && <div className="alert err">{error}<button className="sm" style={{ marginLeft: '0.5rem' }} onClick={() => setError('')}>✕</button></div>}
          {loading && <div className="empty">Loading…</div>}

          {/* Disk thresholds */}
          <div className="section-title">Disk Thresholds</div>
          <div className="form-row">
            <div className="form-group">
              <label>Warn %</label>
              <input type="number" min={0} max={100} value={warnPct} onChange={e => setWarnPct(Number(e.target.value))} />
            </div>
            <div className="form-group">
              <label>Error %</label>
              <input type="number" min={0} max={100} value={errorPct} onChange={e => setErrorPct(Number(e.target.value))} />
            </div>
          </div>
          <button className="primary sm" disabled={saving} onClick={saveThresholds}>Save Thresholds</button>

          {/* Notification rules */}
          <div className="section-title" style={{ marginTop: '1.5rem' }}>Notification Rules</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.6rem', marginBottom: '1rem' }}>
            {[
              { label: 'On Corruption', val: notifyCorruption, set: setNotifyCorruption },
              { label: 'On Error',      val: notifyError,      set: setNotifyError },
              { label: 'On Warn',       val: notifyWarn,       set: setNotifyWarn },
              { label: 'On Completion', val: notifyCompletion, set: setNotifyCompletion },
            ].map(({ label, val, set }) => (
              <label key={label} style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', fontSize: '0.875rem', cursor: 'pointer' }}>
                <input type="checkbox" checked={val} onChange={e => set(e.target.checked)} />
                {label}
              </label>
            ))}
          </div>
          <div style={{ display: 'flex', gap: '0.6rem', alignItems: 'center', flexWrap: 'wrap' }}>
            <button className="primary sm" disabled={saving} onClick={saveNotifications}>Save Notifications</button>
            <button className="sm" disabled={saving} onClick={testEmail}>✉ Test Email</button>
            {testResult && <span style={{ fontSize: '0.8rem', color: testResult.startsWith('✓') ? 'var(--ok)' : 'var(--err)' }}>{testResult}</span>}
          </div>

          {/* Schedule table */}
          <div className="section-title" style={{ marginTop: '1.5rem' }}>Schedule</div>
          {schedule.length === 0 && <div className="empty">No schedule entries.</div>}
          {schedule.length > 0 && (
            <div className="tbl-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Label</th>
                    <th>Cron Expression</th>
                    <th>Enabled</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {schedule.map(entry => {
                    const edit = getScheduleEdit(entry)
                    return (
                      <tr key={entry.id}>
                        <td>
                          <input
                            type="text"
                            value={edit.label}
                            style={{ background: 'transparent', border: 'none', color: 'var(--text)', fontFamily: 'inherit', fontSize: 'inherit', width: '100%' }}
                            onChange={e => setScheduleEdits(s => ({ ...s, [entry.id]: { ...edit, label: e.target.value } }))}
                          />
                        </td>
                        <td>
                          <input
                            type="text"
                            value={edit.cron_expr}
                            style={{ background: 'transparent', border: 'none', color: 'var(--text)', fontFamily: 'var(--font-mono)', fontSize: '0.82rem', width: '100%' }}
                            onChange={e => setScheduleEdits(s => ({ ...s, [entry.id]: { ...edit, cron_expr: e.target.value } }))}
                          />
                        </td>
                        <td>
                          <Toggle
                            checked={edit.enabled}
                            onChange={v => setScheduleEdits(s => ({ ...s, [entry.id]: { ...edit, enabled: v } }))}
                          />
                        </td>
                        <td>
                          <button className="sm primary" disabled={saving} onClick={() => saveScheduleEntry(edit)}>Save</button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
