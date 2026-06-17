import { useEffect, useRef, useState } from 'react'
import { Avatar } from './Avatar'
import type { User } from '../types'
import { getAudioProcessing, setAudioProcessing, type AudioProcessing } from '../voiceSettings'

type Tab = 'account' | 'voice'

// Settings is the Discord-style User Settings overlay. It consolidates the account
// controls that used to be scattered in the chat header (avatar, custom status +
// emoji, presence) behind a single ⚙ entry point, with a left tab rail. No new
// endpoints — it reuses the handlers passed down from Chat (POST /avatar via
// onAvatarPicked, PUT /me/status via onSaveStatus, PUT /me/presence via
// onChangePresence). Esc or an overlay click closes it.
export function Settings({
  token,
  user,
  myPresence,
  myStatus,
  myStatusEmoji,
  avatarVersion,
  onAvatarPicked,
  onSaveStatus,
  onChangePresence,
  audioInputs,
  audioOutputs,
  inputDevice,
  outputDevice,
  onChangeInputDevice,
  onChangeOutputDevice,
  onRefreshDevices,
  onClose,
}: {
  token: string
  user: User
  myPresence: string
  myStatus: string
  myStatusEmoji: string
  avatarVersion: number
  onAvatarPicked: (file: File | undefined) => void | Promise<void>
  onSaveStatus: (status: string, emoji: string) => void | Promise<void>
  onChangePresence: (next: string) => void | Promise<void>
  audioInputs: MediaDeviceInfo[]
  audioOutputs: MediaDeviceInfo[]
  inputDevice: string
  outputDevice: string
  onChangeInputDevice: (id: string) => void
  onChangeOutputDevice: (id: string) => void
  onRefreshDevices: () => void | Promise<void>
  onClose: () => void
}) {
  const [tab, setTab] = useState<Tab>('account')
  // Local, editable copies of the status fields; seeded from props and re-synced
  // if the upstream value changes while the modal is open. Save commits them.
  const [statusText, setStatusText] = useState(myStatus)
  const [statusEmoji, setStatusEmoji] = useState(myStatusEmoji)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const avatarInputRef = useRef<HTMLInputElement>(null)

  // Voice & Video: DSP toggles (seeded from localStorage) + a live mic-test meter.
  const [dsp, setDsp] = useState<AudioProcessing>(() => getAudioProcessing())
  const [testing, setTesting] = useState(false)
  const [level, setLevel] = useState(0) // 0..1 input level for the meter
  const micStreamRef = useRef<MediaStream | null>(null)
  const micCtxRef = useRef<AudioContext | null>(null)
  const micTimerRef = useRef<number | null>(null)

  // Esc closes the modal (Discord-style).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // Fully tear down the mic-test graph (stop tracks, close ctx, cancel the poll).
  const stopMicTest = () => {
    if (micTimerRef.current !== null) {
      clearInterval(micTimerRef.current)
      micTimerRef.current = null
    }
    micStreamRef.current?.getTracks().forEach((t) => t.stop())
    micStreamRef.current = null
    void micCtxRef.current?.close().catch(() => {})
    micCtxRef.current = null
    setLevel(0)
    setTesting(false)
  }

  // Open the selected mic and drive the level meter from its RMS. Uses setInterval
  // (not rAF — reliable when the tab isn't foregrounded) like voice.ts's VAD.
  const startMicTest = async () => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: inputDevice ? { deviceId: { exact: inputDevice } } : true,
      })
      micStreamRef.current = stream
      const ctx = new AudioContext()
      micCtxRef.current = ctx
      void ctx.resume().catch(() => {}) // ensure it's running (autoplay-policy guard)
      const src = ctx.createMediaStreamSource(stream)
      const analyser = ctx.createAnalyser()
      analyser.fftSize = 512
      src.connect(analyser)
      const data = new Uint8Array(analyser.frequencyBinCount)
      micTimerRef.current = window.setInterval(() => {
        analyser.getByteTimeDomainData(data)
        let sum = 0
        for (let i = 0; i < data.length; i++) {
          const v = (data[i] - 128) / 128
          sum += v * v
        }
        const rms = Math.sqrt(sum / data.length)
        setLevel(Math.min(1, rms * 3)) // scale so normal speech fills the bar
      }, 80)
      setTesting(true)
    } catch {
      window.alert('Could not access the microphone. Check the browser permission.')
      stopMicTest()
    }
  }

  // Entering the Voice tab: (re)enumerate devices so labels populate. Leaving it (or
  // unmounting): always stop the mic test so the mic light goes out.
  useEffect(() => {
    if (tab === 'voice') void onRefreshDevices()
    else stopMicTest()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab])
  useEffect(() => stopMicTest, []) // unmount cleanup

  const toggleDsp = (key: keyof AudioProcessing) => {
    const next = { ...dsp, [key]: !dsp[key] }
    setDsp(next)
    setAudioProcessing({ [key]: next[key] })
  }

  // Re-sync local fields if the status changes upstream (e.g. another tab/session).
  useEffect(() => setStatusText(myStatus), [myStatus])
  useEffect(() => setStatusEmoji(myStatusEmoji), [myStatusEmoji])

  const dirty = statusText.trim() !== myStatus || statusEmoji.trim() !== myStatusEmoji

  const saveStatus = async () => {
    setSaving(true)
    setSaved(false)
    try {
      await onSaveStatus(statusText.trim(), statusEmoji.trim())
      setSaved(true)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="settings-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="User settings"
      onClick={onClose}
    >
      <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
        <nav className="settings-nav" aria-label="settings sections">
          <div className="settings-nav-head">User Settings</div>
          <button
            type="button"
            className={`settings-tab${tab === 'account' ? ' active' : ''}`}
            onClick={() => setTab('account')}
          >
            My Account
          </button>
          <button
            type="button"
            className={`settings-tab${tab === 'voice' ? ' active' : ''}`}
            onClick={() => setTab('voice')}
          >
            Voice &amp; Video
          </button>
        </nav>

        <div className="settings-body">
          <button
            type="button"
            className="settings-close"
            aria-label="close settings"
            title="Close (Esc)"
            onClick={onClose}
          >
            ✕
          </button>

          {tab === 'account' && (
            <section className="settings-section" aria-label="My Account">
              <h2 className="settings-title">My Account</h2>

              {/* Avatar + username */}
              <div className="settings-row settings-avatar-row">
                <span className="settings-avatar-preview">
                  <Avatar
                    token={token}
                    userId={user.id}
                    username={user.username}
                    className="avatar avatar-settings"
                    bust={avatarVersion}
                  />
                  <span
                    className={`presence-pip presence-${myPresence} settings-avatar-pip`}
                    aria-hidden
                  />
                </span>
                <div className="settings-avatar-meta">
                  <div className="settings-username">{user.username}</div>
                  <input
                    ref={avatarInputRef}
                    type="file"
                    accept="image/png,image/jpeg,image/gif,image/webp"
                    className="file-input-hidden"
                    onChange={(e) => {
                      // Capture the File synchronously, then clear the input so the
                      // same file can be re-picked (onChange won't fire twice otherwise).
                      void onAvatarPicked(e.target.files?.[0])
                      e.target.value = ''
                    }}
                  />
                  <button
                    type="button"
                    className="settings-btn"
                    onClick={() => avatarInputRef.current?.click()}
                  >
                    Change Avatar
                  </button>
                </div>
              </div>

              {/* Custom status + emoji */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-status">
                  Custom Status
                </label>
                <div className="settings-status-row">
                  <input
                    id="settings-status-emoji"
                    className="settings-input settings-emoji-input"
                    aria-label="status emoji"
                    placeholder="🙂"
                    maxLength={16}
                    value={statusEmoji}
                    onChange={(e) => setStatusEmoji(e.target.value)}
                  />
                  <input
                    id="settings-status"
                    className="settings-input"
                    aria-label="custom status"
                    placeholder="What's happening?"
                    maxLength={128}
                    value={statusText}
                    onChange={(e) => setStatusText(e.target.value)}
                  />
                  <button
                    type="button"
                    className="settings-btn primary"
                    onClick={() => void saveStatus()}
                    disabled={saving || !dirty}
                  >
                    {saving ? 'Saving…' : saved && !dirty ? 'Saved' : 'Save'}
                  </button>
                </div>
              </div>

              {/* Presence */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-presence">
                  Presence
                </label>
                <span className="presence-pill settings-presence">
                  <span className={`presence-pip presence-${myPresence}`} />
                  <select
                    id="settings-presence"
                    className="presence-select"
                    value={myPresence}
                    onChange={(e) => void onChangePresence(e.target.value)}
                    aria-label="set your presence"
                  >
                    <option value="online">Online</option>
                    <option value="idle">Idle</option>
                    <option value="dnd">Do Not Disturb</option>
                    <option value="invisible">Invisible</option>
                  </select>
                </span>
              </div>
            </section>
          )}

          {tab === 'voice' && (
            <section className="settings-section" aria-label="Voice & Video">
              <h2 className="settings-title">Voice &amp; Video</h2>

              {/* Input / output device pickers (Auto = OS default, auto-follows). */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-input-device">
                  Input Device
                </label>
                <select
                  id="settings-input-device"
                  className="settings-input settings-device-select"
                  aria-label="input device"
                  value={inputDevice}
                  onChange={(e) => onChangeInputDevice(e.target.value)}
                >
                  <option value="">Auto (system default)</option>
                  {audioInputs.map((d, i) => (
                    <option key={d.deviceId} value={d.deviceId}>
                      {d.label || `Microphone ${i + 1}`}
                    </option>
                  ))}
                </select>
              </div>

              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-output-device">
                  Output Device
                </label>
                <select
                  id="settings-output-device"
                  className="settings-input settings-device-select"
                  aria-label="output device"
                  value={outputDevice}
                  onChange={(e) => onChangeOutputDevice(e.target.value)}
                >
                  <option value="">Auto (system default)</option>
                  {audioOutputs.map((d, i) => (
                    <option key={d.deviceId} value={d.deviceId}>
                      {d.label || `Output ${i + 1}`}
                    </option>
                  ))}
                </select>
              </div>

              {/* Mic test — live input-sensitivity meter from the selected device. */}
              <div className="settings-field">
                <label className="settings-label">Mic Test</label>
                <div className="mic-test">
                  <button
                    type="button"
                    className={`settings-btn${testing ? '' : ' primary'}`}
                    onClick={() => (testing ? stopMicTest() : void startMicTest())}
                  >
                    {testing ? "Stop Testing" : "Let's Check"}
                  </button>
                  <div className="mic-meter" aria-label="input sensitivity meter">
                    <div
                      className="mic-meter-fill"
                      data-level={level.toFixed(2)}
                      style={{ width: `${Math.round(level * 100)}%` }}
                    />
                  </div>
                </div>
                <span className="settings-hint">
                  {testing
                    ? 'Speak — the bar moves with your mic input.'
                    : 'Start a test and watch the bar respond to your voice.'}
                </span>
              </div>

              {/* DSP toggles — applied to the next voice capture (default on). */}
              <div className="settings-field">
                <label className="settings-label">Audio Processing</label>
                <label className="settings-toggle">
                  <input
                    type="checkbox"
                    aria-label="noise suppression"
                    checked={dsp.ns}
                    onChange={() => toggleDsp('ns')}
                  />
                  <span>Noise Suppression</span>
                </label>
                <label className="settings-toggle">
                  <input
                    type="checkbox"
                    aria-label="echo cancellation"
                    checked={dsp.ec}
                    onChange={() => toggleDsp('ec')}
                  />
                  <span>Echo Cancellation</span>
                </label>
                <label className="settings-toggle">
                  <input
                    type="checkbox"
                    aria-label="automatic gain control"
                    checked={dsp.agc}
                    onChange={() => toggleDsp('agc')}
                  />
                  <span>Automatic Gain Control</span>
                </label>
                <span className="settings-hint">Applied the next time you join a voice channel.</span>
              </div>
            </section>
          )}
        </div>
      </div>
    </div>
  )
}
