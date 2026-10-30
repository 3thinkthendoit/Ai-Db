import { useCallback, useEffect, useState } from 'react'
import { LLMConfig, LLMProfileView } from '../types'
import { useI18n } from '../i18n'
import {
  ActivateLLMProfile,
  DeleteLLMProfile,
  GetLLMConfig,
  ListLLMProfiles,
  SaveLLMProfile,
  SaveLLMSafety,
  TestLLMProfile,
} from '../../wailsjs/go/main/App'

type FormState = {
  id: string
  name: string
  baseUrl: string
  model: string
  apiKey: string
  clearKey: boolean
}

const emptyForm = (): FormState => ({
  id: '',
  name: '',
  baseUrl: 'https://api.openai.com/v1',
  model: 'gpt-4o-mini',
  apiKey: '',
  clearKey: false,
})

function displayProfileName(name: string, t: (k: string) => string) {
  if (name === 'Default' || name === '默认') return t('settings.defaultName')
  return name
}

export function SettingsView() {
  const { t } = useI18n()
  const [profiles, setProfiles] = useState<LLMProfileView[]>([])
  const [safety, setSafety] = useState({ allowWrites: false, maxRows: 500, maxAgentSteps: 12 })
  const [editing, setEditing] = useState(false)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [profileBusy, setProfileBusy] = useState(false)
  const [safetyBusy, setSafetyBusy] = useState(false)
  const [pageMsg, setPageMsg] = useState('')
  const [modalErr, setModalErr] = useState('')
  const [cardTesting, setCardTesting] = useState<Record<string, boolean>>({})
  const [cardResult, setCardResult] = useState<Record<string, { ok: boolean; message: string }>>({})
  const [modalTesting, setModalTesting] = useState(false)
  const [modalTest, setModalTest] = useState<{ ok: boolean; message: string } | null>(null)
  const [deleting, setDeleting] = useState<LLMProfileView | null>(null)

  const reload = useCallback(async () => {
    const [list, cfg] = await Promise.all([
      ListLLMProfiles() as Promise<LLMProfileView[]>,
      GetLLMConfig() as Promise<LLMConfig>,
    ])
    setProfiles(list || [])
    setSafety({
      allowWrites: !!cfg.allowWrites,
      maxRows: cfg.maxRows || 500,
      maxAgentSteps: cfg.maxAgentSteps || 12,
    })
  }, [])

  useEffect(() => {
    void reload().catch((e) => setPageMsg(String(e)))
  }, [reload])

  function startNew() {
    setForm(emptyForm())
    setModalTest(null)
    setModalErr('')
    setEditing(true)
  }

  function startEdit(p: LLMProfileView) {
    setForm({
      id: p.id,
      name: p.name === 'Default' || p.name === '默认' ? t('settings.defaultName') : p.name,
      baseUrl: p.baseUrl,
      model: p.model,
      apiKey: '',
      clearKey: false,
    })
    setModalTest(null)
    setModalErr('')
    setEditing(true)
  }

  function closeModal() {
    if (profileBusy) return
    setEditing(false)
    setModalErr('')
  }

  async function saveProfile() {
    setProfileBusy(true)
    setModalErr('')
    setPageMsg('')
    try {
      await SaveLLMProfile({
        id: form.id,
        name: form.name,
        baseUrl: form.baseUrl,
        model: form.model,
        apiKey: form.apiKey,
        clearKey: form.clearKey,
      } as any)
      setEditing(false)
      await reload()
      setPageMsg(t('settings.saved'))
    } catch (e: any) {
      setModalErr(String(e?.message || e))
    } finally {
      setProfileBusy(false)
    }
  }

  async function activate(p: LLMProfileView) {
    setProfileBusy(true)
    setPageMsg('')
    try {
      await ActivateLLMProfile(p.id)
      await reload()
      setPageMsg(t('settings.switched', { name: displayProfileName(p.name, t) }))
    } catch (e: any) {
      setPageMsg(String(e?.message || e))
    } finally {
      setProfileBusy(false)
    }
  }

  async function confirmDelete() {
    if (!deleting) return
    const p = deleting
    setProfileBusy(true)
    setPageMsg('')
    try {
      await DeleteLLMProfile(p.id)
      setDeleting(null)
      await reload()
      setPageMsg(t('settings.removed'))
    } catch (e: any) {
      setPageMsg(String(e?.message || e))
    } finally {
      setProfileBusy(false)
    }
  }

  async function testCard(p: LLMProfileView) {
    setCardTesting((prev) => ({ ...prev, [p.id]: true }))
    setCardResult((prev) => {
      const next = { ...prev }
      delete next[p.id]
      return next
    })
    try {
      const res = await TestLLMProfile(p.id, '', '', '')
      setCardResult((prev) => ({
        ...prev,
        [p.id]: { ok: !!res.ok, message: res.message || (res.ok ? t('settings.test.ok') : t('settings.test.fail')) },
      }))
    } catch (e: any) {
      setCardResult((prev) => ({
        ...prev,
        [p.id]: { ok: false, message: String(e?.message || e) },
      }))
    } finally {
      setCardTesting((prev) => ({ ...prev, [p.id]: false }))
    }
  }

  async function testModal() {
    setModalTesting(true)
    setModalTest(null)
    setModalErr('')
    try {
      const res = await TestLLMProfile(form.id, form.baseUrl, form.model, form.apiKey)
      setModalTest({
        ok: !!res.ok,
        message: res.message || (res.ok ? t('settings.test.ok') : t('settings.test.fail')),
      })
    } catch (e: any) {
      setModalTest({ ok: false, message: String(e?.message || e) })
    } finally {
      setModalTesting(false)
    }
  }

  async function saveSafety() {
    setSafetyBusy(true)
    setPageMsg('')
    try {
      await SaveLLMSafety(safety.allowWrites, safety.maxRows, safety.maxAgentSteps)
      await reload()
      setPageMsg(t('settings.saved'))
    } catch (e: any) {
      setPageMsg(String(e?.message || e))
    } finally {
      setSafetyBusy(false)
    }
  }

  const editingHasKey = !!profiles.find((p) => p.id === form.id)?.hasApiKey

  return (
    <div className="panel conn-panel">
      <header className="panel-head panel-head-row">
        <div>
          <h2>{t('settings.title')}</h2>
          <p>
            {t('settings.headHint.a')}
            <span className="mono">/v1/chat/completions</span>
            {t('settings.headHint.b')}
          </p>
        </div>
        <button type="button" className="primary" onClick={startNew}>{t('settings.add')}</button>
      </header>

      <div className="conn-list-cards">
        {profiles.length === 0 && <div className="card muted">{t('settings.empty')}</div>}
        {profiles.map((p) => {
          const result = cardResult[p.id]
          return (
            <div key={p.id} className="card conn-card">
              <div className="conn-card-main">
                <div className="conn-card-name">
                  {displayProfileName(p.name, t)}
                  {p.active && <span className="tag ok">{t('settings.using')}</span>}
                  {!p.hasApiKey && <span className="tag danger">{t('settings.noKey')}</span>}
                </div>
                <div className="conn-card-addr mono">{p.model} · {p.baseUrl}</div>
                {result && (
                  <div className={`conn-card-test${result.ok ? ' ok' : ' bad'}`}>{result.message}</div>
                )}
              </div>
              <div className="conn-card-actions">
                <button
                  type="button"
                  className="sm"
                  disabled={!!cardTesting[p.id]}
                  onClick={() => void testCard(p)}
                >
                  {cardTesting[p.id] ? t('settings.test.testing') : t('settings.test.run')}
                </button>
                {!p.active && (
                  <button type="button" className="sm" disabled={profileBusy} onClick={() => void activate(p)}>
                    {t('settings.activate')}
                  </button>
                )}
                <button type="button" className="sm" onClick={() => startEdit(p)}>{t('settings.edit')}</button>
                {profiles.length > 1 && (
                  <button type="button" className="sm danger" disabled={profileBusy} onClick={() => setDeleting(p)}>
                    {t('settings.delete')}
                  </button>
                )}
              </div>
            </div>
          )
        })}
      </div>

      <section className="block settings-safety">
        <div className="block-label">{t('settings.safety')}</div>
        <div className="stack">
          <div className="field">
            <div className="field-meta">
              <div className="field-title">{t('settings.maxRows')}</div>
            </div>
            <input
              style={{ width: 120 }}
              type="number"
              value={safety.maxRows}
              onChange={(e) => setSafety({ ...safety, maxRows: Number(e.target.value) })}
            />
          </div>
          <div className="field">
            <div className="field-meta">
              <div className="field-title">{t('settings.maxSteps')}</div>
            </div>
            <input
              style={{ width: 120 }}
              type="number"
              value={safety.maxAgentSteps}
              onChange={(e) => setSafety({ ...safety, maxAgentSteps: Number(e.target.value) })}
            />
          </div>
          <div className="field">
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={safety.allowWrites}
                onChange={(e) => setSafety({ ...safety, allowWrites: e.target.checked })}
              />
              {t('settings.allowWrites')}
            </label>
          </div>
        </div>
        <button type="button" className="primary" style={{ marginTop: 14 }} disabled={safetyBusy} onClick={() => void saveSafety()}>
          {t('settings.saveSafety')}
        </button>
      </section>

      {pageMsg && <p className="form-msg">{pageMsg}</p>}

      {deleting && (
        <div className="overlay">
          <div className="modal" role="dialog" aria-modal="true">
            <div className="modal-head">
              <h3>{t('settings.deleteTitle')}</h3>
              <button
                type="button"
                className="modal-close"
                disabled={profileBusy}
                title={t('modal.close')}
                aria-label={t('modal.close')}
                onClick={() => setDeleting(null)}
              >
                ×
              </button>
            </div>
            <p>
              {t('settings.confirmDelete', { name: displayProfileName(deleting.name, t) })}
            </p>
            <div className="modal-actions">
              <button type="button" className="sm" disabled={profileBusy} onClick={() => setDeleting(null)}>
                {t('settings.cancel')}
              </button>
              <button type="button" className="sm danger" disabled={profileBusy} onClick={() => void confirmDelete()}>
                {t('settings.delete')}
              </button>
            </div>
          </div>
        </div>
      )}

      {editing && (
        <div className="modal-backdrop">
          <div className="modal" role="dialog" aria-modal="true">
            <div className="modal-head">
              <h2>{form.id ? t('settings.editTitle') : t('settings.addTitle')}</h2>
              <button
                type="button"
                className="modal-close"
                disabled={profileBusy}
                title={t('modal.close')}
                aria-label={t('modal.close')}
                onClick={closeModal}
              >
                ×
              </button>
            </div>
            <label>
              {t('settings.info.name')}
              <input
                value={form.name}
                placeholder={t('settings.info.namePh')}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
            </label>
            <label>
              {t('settings.baseUrl')}
              <input
                value={form.baseUrl}
                placeholder="https://api.openai.com/v1"
                onChange={(e) => setForm({ ...form, baseUrl: e.target.value })}
              />
            </label>
            <label>
              {t('settings.model')}
              <input
                value={form.model}
                placeholder="gpt-4o-mini"
                onChange={(e) => setForm({ ...form, model: e.target.value })}
              />
            </label>
            <label>
              {t('settings.apiKey')}
              {form.id && editingHasKey ? (
                <span className="muted"> {t('settings.key.saved')}</span>
              ) : null}
              <input
                type="password"
                value={form.apiKey}
                placeholder={form.id && editingHasKey ? t('settings.apiKey.ph') : 'sk-...'}
                onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
              />
            </label>
            {form.id && editingHasKey && (
              <label className="checkbox-row">
                <input
                  type="checkbox"
                  checked={form.clearKey}
                  onChange={(e) => setForm({ ...form, clearKey: e.target.checked })}
                />
                {t('settings.key.clear')}
              </label>
            )}
            <p className="muted" style={{ margin: 0, fontSize: 12, lineHeight: 1.6 }}>{t('settings.notice')}</p>
            {modalTest && (
              <div className={`conn-card-test${modalTest.ok ? ' ok' : ' bad'}`}>{modalTest.message}</div>
            )}
            {modalErr && <div className="error-text">{modalErr}</div>}
            <div className="modal-actions">
              <button type="button" disabled={profileBusy} onClick={closeModal}>{t('settings.cancel')}</button>
              <button type="button" disabled={modalTesting} onClick={() => void testModal()}>
                {modalTesting ? t('settings.test.testing') : t('settings.test.run')}
              </button>
              <button type="button" className="primary" disabled={profileBusy} onClick={() => void saveProfile()}>
                {profileBusy ? t('settings.saving') : t('settings.save')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
