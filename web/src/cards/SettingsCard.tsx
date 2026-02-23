import { useEffect, useState, useCallback } from 'react'
import { api, type SettingsResponse, type ScheduleEntry, type ConfigResponse } from '../lib/api'

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

  // Config editor state
  const [config, setConfig] = useState<ConfigResponse | null>(null)
  const [configEdits, setConfigEdits] = useState<Partial<ConfigResponse>>({})
  const [configSaving, setConfigSaving] = useState(false)
  const [configSaved, setConfigSaved] = useState(false)

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

  // Load config when expanded
  useEffect(() => {
    if (expanded) {
      api.getConfig().then(c => { setConfig(c); setConfigEdits({}) }).catch(() => {})
    }
  }, [expanded])

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
    >
      <div className="card-header">
        <span className="card-title">Settings</span>
        {expanded && (
          <button className="card-close-btn" onClick={e => { e.stopPropagation(); onCollapse() }} title="Collapse (Esc)">✕</button>
        )}
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

          {/* ── Config editor ── */}
          <div className="section-title" style={{ marginTop: '1.5rem' }}>Config</div>
          {!config && <div className="empty">Loading config…</div>}
          {config && (
            <>
              {config.target_paths && config.target_paths.length > 0 && (
                <div style={{ marginBottom: '1rem' }}>
                  <div style={{ fontSize: '0.8rem', color: 'var(--muted)', marginBottom: '0.4rem' }}>Target Paths (read-only)</div>
                  {config.target_paths.map(p => (
                    <div key={p} style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem', color: 'var(--text)', marginBottom: '0.2rem' }}>{p}</div>
                  ))}
                </div>
              )}
              <div className="form-row" style={{ flexWrap: 'wrap', gap: '0.75rem', marginBottom: '0.75rem' }}>
                {[{ label: 'Scrub %', key: 'scrub_percentage', type: 'number', min: 0, max: 100, step: 1 },
                  { label: 'Max Workers', key: 'max_workers', type: 'number', min: 1, max: 64, step: 1 },
                  { label: 'Log Retention (days)', key: 'log_retention_days', type: 'number', min: 1, max: 365, step: 1 }]
                  .map(({ label, key, type, min, max, step }) => (
                    <div key={key} className="form-group">
                      <label>{label}</label>
                      <input
                        type={type}
                        min={min} max={max} step={step}
                        value={((configEdits as Record<string, unknown>)[key] ?? (config as Record<string, unknown>)[key] ?? '') as string | number}
                        onChange={e => setConfigEdits(prev => ({ ...prev, [key]: Number(e.target.value) }))}
                      />
                    </div>
                  ))}
                <div className="form-group">
                  <label>Scrub Frequency</label>
                  <select
                    value={(configEdits.scrub_frequency ?? config.scrub_frequency) || ''}
                    onChange={e => setConfigEdits(prev => ({ ...prev, scrub_frequency: e.target.value }))}
                  >
                    {['daily', 'weekly', 'monthly'].map(f => <option key={f} value={f}>{f}</option>)}
                  </select>
                </div>
                <div className="form-group">
                  <label>Log Level</label>
                  <select
                    value={(configEdits.log_level ?? config.log_level) || ''}
                    onChange={e => setConfigEdits(prev => ({ ...prev, log_level: e.target.value }))}
                  >
                    {['debug', 'info', 'warn', 'error'].map(l => <option key={l} value={l}>{l}</option>)}
                  </select>
                </div>
              </div>
              {/* SMTP (non-secret fields) */}
              <div style={{ fontSize: '0.8rem', color: 'var(--muted)', marginBottom: '0.4rem' }}>SMTP</div>
              <div className="form-row" style={{ flexWrap: 'wrap', gap: '0.75rem', marginBottom: '0.75rem' }}>
                {[{ label: 'Host', key: 'host', type: 'text' },
                  { label: 'Port', key: 'port', type: 'number' },
                  { label: 'Sender', key: 'sender', type: 'email' },
                  { label: 'Recipient', key: 'recipient', type: 'email' }]
                  .map(({ label, key, type }) => (
                    <div key={key} className="form-group">
                      <label>{label}</label>
                      <input
                        type={type}
                        value={((configEdits.smtp ?? config.smtp ?? {}) as Record<string, unknown>)[key] as string ?? ''}
                        onChange={e => setConfigEdits(prev => ({
                          ...prev,
                          smtp: { ...config.smtp, ...(prev.smtp ?? {}), [key]: key === 'port' ? Number(e.target.value) : e.target.value } as typeof config.smtp
                        }))}
                      />
                    </div>
                  ))}
              </div>
              <div style={{ display: 'flex', gap: '0.6rem', alignItems: 'center' }}>
                <button
                  className="primary sm"
                  disabled={configSaving || Object.keys(configEdits).length === 0}
                  onClick={async () => {
                    setConfigSaving(true)
                    setConfigSaved(false)
                    try {
                      const updated = await api.postConfig(configEdits)
                      setConfig(updated)
                      setConfigEdits({})
                      setConfigSaved(true)
                      setTimeout(() => setConfigSaved(false), 3000)
                    } catch (e) { setError(String(e)) }
                    finally { setConfigSaving(false) }
                  }}
                >Save Config</button>
                {configSaved && <span style={{ fontSize: '0.8rem', color: 'var(--ok)' }}>✓ Saved</span>}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
