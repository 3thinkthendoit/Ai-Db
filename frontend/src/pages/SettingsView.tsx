import { useCallback, useEffect, useState } from 'react'
import { LLMProfileView } from '../types'
import { useI18n } from '../i18n'
import { useTipToast } from '../components/TipToast'
import { BodyPortal } from '../lib/portal'
import {
  ActivateLLMProfile,
  DeleteLLMProfile,
  ListLLMProfiles,
  SaveLLMProfile,
  TestLLMProfile,
} from '../lib/api'

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
  const { showTip, tipNode } = useTipToast()
  const [profiles, setProfiles] = useState<LLMProfileView[]>([])
  const [editing, setEditing] = useState(false)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [profileBusy, setProfileBusy] = useState(false)
  const [modalErr, setModalErr] = useState('')
  const [cardTesting, setCardTesting] = useState<Record<string, boolean>>({})
  const [cardResult, setCardResult] = useState<Record<string, { ok: boolean; message: string }>>({})
  const [modalTesting, setModalTesting] = useState(false)
  const [modalTest, setModalTest] = useState<{ ok: boolean; message: string } | null>(null)
  const [deleting, setDeleting] = useState<LLMProfileView | null>(null)

  const reload = useCallback(async () => {
    const list = (await ListLLMProfiles()) as LLMProfileView[]
    setProfiles(list || [])
  }, [])

  useEffect(() => {
    void reload().catch((e) => showTip(String(e), 'err'))
  }, [reload, showTip])

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
      showTip(t('settings.saved'))
    } catch (e: any) {
      setModalErr(String(e?.message || e))
    } finally {
      setProfileBusy(false)
    }
  }

  async function activate(p: LLMProfileView) {
    setProfileBusy(true)
    try {
      await ActivateLLMProfile(p.id)
      await reload()
      showTip(t('settings.switched', { name: displayProfileName(p.name, t) }))
    } catch (e: any) {
      showTip(String(e?.message || e), 'err')
    } finally {
      setProfileBusy(false)
    }
  }

  async function confirmDelete() {
    if (!deleting) return
    const p = deleting
    setProfileBusy(true)
    try {
      await DeleteLLMProfile(p.id)
      setDeleting(null)
      await reload()
      showTip(t('settings.removed'))
    } catch (e: any) {
      showTip(String(e?.message || e), 'err')
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

  const editingHasKey = !!profiles.find((p) => p.id === form.id)?.hasApiKey

  return (
    <>
      {tipNode}
      <div className="data-table-toolbar">
        <div>
          <h2>{t('settings.title')}</h2>
          <p>
            {t('settings.headHint.a')}
            <span className="mono">/v1/chat/completions</span>
            {t('settings.headHint.b')}
          </p>
        </div>
        <button type="button" className="primary" onClick={startNew}>
          {t('settings.add')}
        </button>
      </div>

      <div className="data-table-wrap">
        {profiles.length === 0 ? (
          <div className="data-table-empty">{t('settings.empty')}</div>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th style={{ width: '18%' }}>{t('settings.col.name')}</th>
                <th style={{ width: '16%' }}>{t('settings.col.model')}</th>
                <th style={{ width: '28%' }}>{t('settings.col.endpoint')}</th>
                <th style={{ width: '14%' }}>{t('settings.col.status')}</th>
                <th style={{ width: '24%' }}>{t('settings.col.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {profiles.map((p) => {
                const result = cardResult[p.id]
                return (
                  <tr key={p.id} className={p.active ? 'is-selected' : ''}>
                    <td>
                      <div className="cell-main">{displayProfileName(p.name, t)}</div>
                      {result && (
                        <div className="cell-sub" style={{ color: result.ok ? 'var(--ok)' : 'var(--danger)' }}>
                          {result.message}
                        </div>
                      )}
                    </td>
                    <td>
                      <div className="cell-muted mono">{p.model}</div>
                    </td>
                    <td>
                      <div className="cell-muted mono" style={{ wordBreak: 'break-all' }}>{p.baseUrl}</div>
                    </td>
                    <td>
                      {p.active ? (
                        <span className="tag ok">{t('settings.using')}</span>
                      ) : (
                        <span className="pill" style={{ marginLeft: 0 }}>{t('settings.idle')}</span>
                      )}
                      {!p.hasApiKey && (
                        <div className="cell-sub">
                          <span className="tag danger">{t('settings.noKey')}</span>
                        </div>
                      )}
                    </td>
                    <td>
                      <div className="cell-actions">
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
                        <button type="button" className="sm" onClick={() => startEdit(p)}>
                          {t('settings.edit')}
                        </button>
                        <button
                          type="button"
                          className="sm danger"
                          disabled={profileBusy || profiles.length <= 1}
                          title={profiles.length <= 1 ? t('settings.delete.last') : t('settings.delete')}
                          onClick={() => {
                            if (profiles.length <= 1) {
                              showTip(t('settings.delete.last'), 'err')
                              return
                            }
                            setDeleting(p)
                          }}
                        >
                          {t('settings.delete')}
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>

      {deleting && (
        <BodyPortal>
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
        </BodyPortal>
      )}

      {editing && (
        <BodyPortal>
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
                  placeholder={form.id && editingHasKey ? t('settings.apiKey.ph') : t('settings.apiKey.need')}
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
        </BodyPortal>
      )}
    </>
  )
}
