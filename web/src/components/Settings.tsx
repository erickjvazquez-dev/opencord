import { useEffect, useRef, useState } from 'react'
import { Avatar } from './Avatar'
import type { User } from '../types'
import { listBlocked } from '../api'
import {
  getAudioProcessing,
  setAudioProcessing,
  getCameraDeviceId,
  setCameraDeviceId,
  getOutputVolume,
  setOutputVolume,
  getInputVolume,
  setInputVolume,
  type AudioProcessing,
} from '../voiceSettings'
import { getDesktopNotify, setDesktopNotify, requestNotifyPermission } from '../notify'

type Tab = 'account' | 'voice' | 'notifications' | 'privacy'

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
  myAbout,
  myPronouns,
  avatarVersion,
  onAvatarPicked,
  onSaveStatus,
  onSaveProfile,
  onChangePresence,
  audioInputs,
  audioOutputs,
  inputDevice,
  outputDevice,
  onChangeInputDevice,
  onChangeOutputDevice,
  onRefreshDevices,
  onSetMasterVolume,
  onSetInputVolume,
  onUnblock,
  onClose,
}: {
  token: string
  user: User
  myPresence: string
  myStatus: string
  myStatusEmoji: string
  myAbout: string
  myPronouns: string
  avatarVersion: number
  onAvatarPicked: (file: File | undefined) => void | Promise<void>
  onSaveStatus: (status: string, emoji: string) => void | Promise<void>
  onSaveProfile: (about: string, pronouns: string) => void | Promise<void>
  onChangePresence: (next: string) => void | Promise<void>
  audioInputs: MediaDeviceInfo[]
  audioOutputs: MediaDeviceInfo[]
  inputDevice: string
  outputDevice: string
  onChangeInputDevice: (id: string) => void
  onChangeOutputDevice: (id: string) => void
  onRefreshDevices: () => void | Promise<void>
  onSetMasterVolume: (volume: number) => void
  onSetInputVolume: (volume: number) => void
  // Unblock a user by id. Resolves once the server has unblocked them and Chat's
  // message-hide set has been refreshed, so the local list can drop the row.
  onUnblock: (userId: number) => void | Promise<void>
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

  // Profile fields (About Me + pronouns), seeded from props with their own Save.
  const [aboutText, setAboutText] = useState(myAbout)
  const [pronounsText, setPronounsText] = useState(myPronouns)
  const [profileSaving, setProfileSaving] = useState(false)
  const [profileSaved, setProfileSaved] = useState(false)
  const profileDirty = aboutText.trim() !== myAbout || pronounsText.trim() !== myPronouns
  const saveProfile = async () => {
    setProfileSaving(true)
    setProfileSaved(false)
    try {
      await onSaveProfile(aboutText.trim(), pronounsText.trim())
      setProfileSaved(true)
    } finally {
      setProfileSaving(false)
    }
  }

  // Voice & Video: DSP toggles (seeded from localStorage) + a live mic-test meter.
  const [dsp, setDsp] = useState<AudioProcessing>(() => getAudioProcessing())
  const [testing, setTesting] = useState(false)
  const [level, setLevel] = useState(0) // 0..1 input level for the meter
  const micStreamRef = useRef<MediaStream | null>(null)
  const micCtxRef = useRef<AudioContext | null>(null)
  const micTimerRef = useRef<number | null>(null)

  // Camera: a videoinput picker (enumerated locally — video isn't in the voice
  // pipeline yet) + a live preview. The chosen camera persists in localStorage.
  const [videoInputs, setVideoInputs] = useState<MediaDeviceInfo[]>([])
  const [camera, setCamera] = useState(() => getCameraDeviceId())
  const [camTesting, setCamTesting] = useState(false)
  const camStreamRef = useRef<MediaStream | null>(null)
  const videoElRef = useRef<HTMLVideoElement>(null)

  // Master output volume (0..1) — persisted + applied live to the current call.
  const [outputVolume, setOutputVolumeState] = useState(() => getOutputVolume())
  const changeOutputVolume = (v: number) => {
    setOutputVolumeState(v)
    setOutputVolume(v)
    onSetMasterVolume(v)
  }

  // Input (mic) volume (0..1) — how loud peers hear you; persisted + applied live.
  const [inputVolume, setInputVolumeState] = useState(() => getInputVolume())
  const changeInputVolume = (v: number) => {
    setInputVolumeState(v)
    setInputVolume(v)
    onSetInputVolume(v)
  }

  // Desktop notifications: the persisted opt-in (default off) + the current OS
  // permission. Enabling requests permission on the click (a user gesture); we only
  // persist enabled=true if the user grants it (denied → stay off + show a hint).
  const notifySupported = typeof window !== 'undefined' && 'Notification' in window
  const [notifyEnabled, setNotifyEnabled] = useState(() => getDesktopNotify())
  const [notifyPermission, setNotifyPermission] = useState<string>(() =>
    notifySupported ? Notification.permission : 'denied',
  )
  const toggleNotify = async () => {
    if (notifyEnabled) {
      setNotifyEnabled(false)
      setDesktopNotify(false)
      return
    }
    // Turning ON: ask the OS (user gesture). Only commit if it's actually granted.
    const perm = await requestNotifyPermission()
    setNotifyPermission(perm)
    const granted = perm === 'granted'
    setNotifyEnabled(granted)
    setDesktopNotify(granted)
  }

  // Privacy: the users I've blocked. Settings owns its own copy of the list (so it can
  // show/refresh it independently) and fetches it whenever the Privacy tab is opened.
  // Unblocking delegates to onUnblock (which hits the server + refreshes Chat's hide set)
  // and then drops the row locally.
  const [blockedUsers, setBlockedUsers] = useState<{ id: number; username: string }[]>([])
  const [blockedLoading, setBlockedLoading] = useState(false)
  const loadBlocked = async () => {
    setBlockedLoading(true)
    try {
      setBlockedUsers(await listBlocked(token))
    } catch {
      /* leave the list as-is on failure */
    } finally {
      setBlockedLoading(false)
    }
  }
  const unblock = async (userId: number) => {
    try {
      await onUnblock(userId)
      setBlockedUsers((prev) => prev.filter((u) => u.id !== userId))
    } catch {
      /* onUnblock surfaces its own error; keep the row on failure */
    }
  }
  // Fetch the block list when the Privacy tab is opened (mirrors the Voice tab's
  // device (re)enumeration on entry).
  useEffect(() => {
    if (tab === 'privacy') void loadBlocked()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab])

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

  // Stop the camera preview (stop tracks, detach from the <video>).
  const stopCameraTest = () => {
    camStreamRef.current?.getTracks().forEach((t) => t.stop())
    camStreamRef.current = null
    if (videoElRef.current) videoElRef.current.srcObject = null
    setCamTesting(false)
  }

  // Enumerate video inputs (cameras) — kept local to Settings since video isn't part
  // of the voice capture pipeline yet (slice 3b is preview-only).
  const refreshCameras = async () => {
    try {
      const devs = await navigator.mediaDevices.enumerateDevices()
      setVideoInputs(devs.filter((d) => d.kind === 'videoinput'))
    } catch {
      /* enumeration unsupported — the picker just stays at Auto */
    }
  }

  // Open the selected camera into the preview <video>. Takes an explicit deviceId so a
  // live device swap doesn't read a stale `camera` from this render's closure.
  const startCameraTest = async (deviceId = camera) => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        video: deviceId ? { deviceId: { exact: deviceId } } : true,
      })
      camStreamRef.current = stream
      setCamTesting(true)
      if (videoElRef.current) {
        videoElRef.current.srcObject = stream
        await videoElRef.current.play().catch(() => {})
      }
      void refreshCameras() // labels populate once permission is granted
    } catch {
      window.alert('Could not access the camera. Check the browser permission.')
      stopCameraTest()
    }
  }

  const changeCamera = (id: string) => {
    setCamera(id)
    setCameraDeviceId(id)
    // If a preview is live, swap to the newly chosen camera immediately.
    if (camTesting) {
      stopCameraTest()
      void startCameraTest(id)
    }
  }

  // Entering the Voice tab: (re)enumerate audio + video devices so labels populate.
  // Leaving it (or unmounting): always stop both tests so the mic/camera lights die.
  useEffect(() => {
    if (tab === 'voice') {
      void onRefreshDevices()
      void refreshCameras()
    } else {
      stopMicTest()
      stopCameraTest()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab])
  useEffect(
    () => () => {
      stopMicTest()
      stopCameraTest()
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  ) // unmount cleanup

  const toggleDsp = (key: keyof AudioProcessing) => {
    const next = { ...dsp, [key]: !dsp[key] }
    setDsp(next)
    setAudioProcessing({ [key]: next[key] })
  }

  // Re-sync local fields if the status changes upstream (e.g. another tab/session).
  useEffect(() => setStatusText(myStatus), [myStatus])
  useEffect(() => setStatusEmoji(myStatusEmoji), [myStatusEmoji])
  useEffect(() => setAboutText(myAbout), [myAbout])
  useEffect(() => setPronounsText(myPronouns), [myPronouns])

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
          <button
            type="button"
            className={`settings-tab${tab === 'notifications' ? ' active' : ''}`}
            onClick={() => setTab('notifications')}
          >
            Notifications
          </button>
          <button
            type="button"
            className={`settings-tab${tab === 'privacy' ? ' active' : ''}`}
            onClick={() => setTab('privacy')}
          >
            Privacy
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

              {/* Profile: pronouns + About Me (shown on your profile card). */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-pronouns">
                  Pronouns
                </label>
                <input
                  id="settings-pronouns"
                  className="settings-input"
                  aria-label="pronouns"
                  placeholder="e.g. they/them"
                  maxLength={40}
                  value={pronounsText}
                  onChange={(e) => setPronounsText(e.target.value)}
                />
              </div>
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-about">
                  About Me
                </label>
                <textarea
                  id="settings-about"
                  className="settings-input settings-about"
                  aria-label="about me"
                  placeholder="Tell people a bit about yourself"
                  maxLength={190}
                  rows={3}
                  value={aboutText}
                  onChange={(e) => setAboutText(e.target.value)}
                />
                <button
                  type="button"
                  className="settings-btn primary settings-profile-save"
                  onClick={() => void saveProfile()}
                  disabled={profileSaving || !profileDirty}
                >
                  {profileSaving ? 'Saving…' : profileSaved && !profileDirty ? 'Saved' : 'Save profile'}
                </button>
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

              {/* Output volume — master playback level, applied live to the call. */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-output-volume">
                  Output Volume — {Math.round(outputVolume * 100)}%
                </label>
                <input
                  id="settings-output-volume"
                  className="settings-slider"
                  type="range"
                  min={0}
                  max={100}
                  value={Math.round(outputVolume * 100)}
                  aria-label="output volume"
                  onChange={(e) => changeOutputVolume(Number(e.target.value) / 100)}
                />
              </div>

              {/* Input volume — mic gain, how loud peers hear you, applied live. */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-input-volume">
                  Input Volume — {Math.round(inputVolume * 100)}%
                </label>
                <input
                  id="settings-input-volume"
                  className="settings-slider"
                  type="range"
                  min={0}
                  max={100}
                  value={Math.round(inputVolume * 100)}
                  aria-label="input volume"
                  onChange={(e) => changeInputVolume(Number(e.target.value) / 100)}
                />
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

              {/* Camera — device picker + live preview (preview only for now). */}
              <div className="settings-field">
                <label className="settings-label" htmlFor="settings-camera-device">
                  Camera
                </label>
                <select
                  id="settings-camera-device"
                  className="settings-input settings-device-select"
                  aria-label="camera"
                  value={camera}
                  onChange={(e) => changeCamera(e.target.value)}
                >
                  <option value="">Auto (system default)</option>
                  {videoInputs.map((d, i) => (
                    <option key={d.deviceId} value={d.deviceId}>
                      {d.label || `Camera ${i + 1}`}
                    </option>
                  ))}
                </select>
                <div className={`cam-preview${camTesting ? ' live' : ''}`}>
                  <video
                    ref={videoElRef}
                    className="cam-preview-video"
                    aria-label="camera preview"
                    muted
                    playsInline
                  />
                  {!camTesting && <span className="cam-preview-empty">Camera preview</span>}
                </div>
                <button
                  type="button"
                  className={`settings-btn${camTesting ? '' : ' primary'}`}
                  onClick={() => (camTesting ? stopCameraTest() : void startCameraTest())}
                >
                  {camTesting ? 'Stop Camera' : 'Test Camera'}
                </button>
              </div>
            </section>
          )}

          {tab === 'notifications' && (
            <section className="settings-section" aria-label="Notifications">
              <h2 className="settings-title">Notifications</h2>

              {/* Desktop notifications opt-in. Enabling requests OS permission on the
                  click (a user gesture); we only stay on if it's granted. Off by
                  default — Discord's low-noise default (DMs + @-mentions while away). */}
              <div className="settings-field">
                <label className="settings-label">Desktop Notifications</label>
                <label className="settings-toggle">
                  <input
                    type="checkbox"
                    aria-label="desktop notifications"
                    checked={notifyEnabled}
                    disabled={!notifySupported}
                    onChange={() => void toggleNotify()}
                  />
                  <span>Show a desktop notification for DMs and @-mentions</span>
                </label>
                <span className="settings-hint">
                  {!notifySupported
                    ? 'Your browser does not support desktop notifications.'
                    : notifyPermission === 'denied'
                      ? 'Allow notifications in your browser to enable this.'
                      : notifyEnabled
                        ? 'You’ll be notified of DMs and @-mentions while this tab is in the background.'
                        : 'Only fires when this tab is in the background — never for your own messages.'}
                </span>
              </div>
            </section>
          )}

          {tab === 'privacy' && (
            <section className="settings-section" aria-label="Privacy">
              <h2 className="settings-title">Privacy</h2>

              {/* Blocked users: avatar/initials + username + an Unblock button each.
                  Blocking someone hides their messages everywhere and blocks DMs both
                  ways (server-enforced). Empty state when you've blocked no one. */}
              <div className="settings-field">
                <label className="settings-label">Blocked Users</label>
                {blockedLoading && blockedUsers.length === 0 ? (
                  <div className="blocked-empty">Loading…</div>
                ) : blockedUsers.length === 0 ? (
                  <div className="blocked-empty">No blocked users.</div>
                ) : (
                  <div className="blocked-list" aria-label="blocked users">
                    {blockedUsers.map((u) => (
                      <div className="blocked-row" key={u.id} data-blocked-user={u.id}>
                        <Avatar token={token} userId={u.id} username={u.username} />
                        <span className="blocked-name">{u.username}</span>
                        <button
                          type="button"
                          className="settings-btn blocked-unblock-btn"
                          onClick={() => void unblock(u.id)}
                        >
                          Unblock
                        </button>
                      </div>
                    ))}
                  </div>
                )}
                <span className="settings-hint">
                  Blocking someone hides their messages and prevents direct messages both ways.
                </span>
              </div>
            </section>
          )}
        </div>
      </div>
    </div>
  )
}
