// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RecordControl } from './RecordControl'

const micOpenSettings = vi.hoisted(() => vi.fn())
const systemAudioStatus = vi.hoisted(() => vi.fn())
const systemAudioOpenSettings = vi.hoisted(() => vi.fn())

vi.mock('@/api', () => ({
  muesli: { micOpenSettings, systemAudioStatus, systemAudioOpenSettings },
}))

afterEach(cleanup)
beforeEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
  micOpenSettings.mockReset()
  systemAudioStatus.mockReset()
  systemAudioOpenSettings.mockReset()
  setPlatform('Linux x86_64')
})

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function mockEnumerateDevices(devices: Partial<MediaDeviceInfo>[]) {
  const enumerateDevices = vi.fn().mockResolvedValue(
    devices.map((d) => ({ kind: 'audioinput', deviceId: '', label: '', groupId: '', ...d })),
  )
  Object.defineProperty(navigator, 'mediaDevices', {
    value: { enumerateDevices, addEventListener: vi.fn(), removeEventListener: vi.fn() },
    configurable: true,
    writable: true,
  })
  return enumerateDevices
}

function setPlatform(platform: string) {
  Object.defineProperty(navigator, 'platform', { value: platform, configurable: true })
}

// ---------------------------------------------------------------------------
// State tests
// ---------------------------------------------------------------------------

describe('RecordControl', () => {
  it.each(['idle', 'recording'] as const)(
    'renders the microphone level meter next to the selector while %s',
    (state) => {
      mockEnumerateDevices([])
      render(
        <RecordControl
          state={state}
          elapsedMs={0}
          onStart={() => {}}
          onStop={() => {}}
          recordingLevel={0.4}
        />,
      )
      const selector = screen.getByRole('combobox', { name: /microphone/i })
      const meter = screen.getByTestId('microphone-level-meter')
      expect(selector.parentElement?.parentElement).toContainElement(meter)
      expect(screen.getByRole('meter', { name: /microphone level/i })).toBeVisible()
    },
  )

  it('idle shows Record and calls onStart', async () => {
    mockEnumerateDevices([])
    const onStart = vi.fn()
    render(<RecordControl state="idle" elapsedMs={0} onStart={onStart} onStop={() => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /record/i }))
    expect(onStart).toHaveBeenCalledOnce()
  })

  it('recording shows a mono timer and Stop, announces state, and Stop calls onStop', async () => {
    mockEnumerateDevices([])
    const onStop = vi.fn()
    render(<RecordControl state="recording" elapsedMs={62000} onStart={() => {}} onStop={onStop} />)
    expect(screen.getByText('1:02')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent(/recording/i)
    await userEvent.click(screen.getByRole('button', { name: /stop/i }))
    expect(onStop).toHaveBeenCalledOnce()
  })

  it('processing shows a status and no record/stop buttons', () => {
    mockEnumerateDevices([])
    render(<RecordControl state="processing" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)
    expect(screen.getByRole('status')).toHaveTextContent(/processing/i)
    expect(screen.queryByRole('button')).toBeNull()
  })

  // disabledReason gate scenarios
  it('idle + disabledReason="Server unreachable" shows warning text and no Record button', () => {
    mockEnumerateDevices([])
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        disabledReason="Server unreachable"
      />,
    )
    expect(screen.getByText('Server unreachable')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /record/i })).toBeNull()
  })

  it('idle + disabledReason="Still loading" shows warning text', () => {
    mockEnumerateDevices([])
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        disabledReason="Still loading"
      />,
    )
    expect(screen.getByText('Still loading')).toBeInTheDocument()
  })

  it('idle + no disabledReason shows Record button and no warning', () => {
    mockEnumerateDevices([])
    render(<RecordControl state="idle" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)
    expect(screen.getByRole('button', { name: /record/i })).toBeInTheDocument()
    expect(screen.queryByText('Server unreachable')).toBeNull()
    expect(screen.queryByText('Still loading')).toBeNull()
  })

  it('recording state ignores disabledReason and shows Stop button', () => {
    mockEnumerateDevices([])
    render(
      <RecordControl
        state="recording"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        disabledReason="Server unreachable"
      />,
    )
    expect(screen.getByRole('button', { name: /stop/i })).toBeInTheDocument()
    expect(screen.queryByText('Server unreachable')).toBeNull()
  })

  it('processing state ignores disabledReason', () => {
    mockEnumerateDevices([])
    render(
      <RecordControl
        state="processing"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        disabledReason="Server unreachable"
      />,
    )
    expect(screen.getByRole('status')).toHaveTextContent(/processing/i)
    expect(screen.queryByText('Server unreachable')).toBeNull()
  })
})

// ---------------------------------------------------------------------------
// Device picker
// ---------------------------------------------------------------------------

describe('RecordControl — device picker', () => {
  it('renders with enumerated audioinput devices', async () => {
    mockEnumerateDevices([
      { deviceId: 'dev-1', label: 'Built-in Mic', kind: 'audioinput' },
      { deviceId: 'dev-2', label: 'USB Mic', kind: 'audioinput' },
    ])
    render(<RecordControl state="idle" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)
    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: /microphone/i })).toBeInTheDocument()
      expect(screen.getByRole('option', { name: /built-in mic/i })).toBeInTheDocument()
      expect(screen.getByRole('option', { name: /usb mic/i })).toBeInTheDocument()
    })
  })

  it('calls onDeviceChange with the selected deviceId', async () => {
    mockEnumerateDevices([
      { deviceId: 'dev-1', label: 'Built-in Mic', kind: 'audioinput' },
      { deviceId: 'dev-2', label: 'USB Mic', kind: 'audioinput' },
    ])
    const onDeviceChange = vi.fn()
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        onDeviceChange={onDeviceChange}
      />,
    )
    await waitFor(() =>
      expect(screen.getByRole('option', { name: /usb mic/i })).toBeInTheDocument(),
    )
    await userEvent.selectOptions(
      screen.getByRole('combobox', { name: /microphone/i }),
      'dev-2',
    )
    expect(onDeviceChange).toHaveBeenCalledWith('dev-2')
  })

  it('disables the device picker while recording', async () => {
    mockEnumerateDevices([{ deviceId: 'dev-1', label: 'Built-in Mic', kind: 'audioinput' }])
    render(<RecordControl state="recording" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)
    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: /microphone/i })).toBeDisabled(),
    )
  })
})

// ---------------------------------------------------------------------------
// Gain slider
// ---------------------------------------------------------------------------

describe('RecordControl — gain slider', () => {
  it('renders a range input labelled Gain with default 100%', async () => {
    mockEnumerateDevices([])
    render(<RecordControl state="idle" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)
    const slider = screen.getByRole('slider', { name: /gain/i })
    expect(slider).toBeInTheDocument()
    expect(slider).toHaveValue('100')
  })

  it('calls onGainChange with linear float on change', async () => {
    mockEnumerateDevices([])
    const onGainChange = vi.fn()
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        onGainChange={onGainChange}
      />,
    )
    const slider = screen.getByRole('slider', { name: /gain/i })
    fireEvent.change(slider, { target: { value: '150' } })
    expect(onGainChange).toHaveBeenCalledWith(1.5)
  })

  it('disables the gain slider while recording', async () => {
    mockEnumerateDevices([])
    render(<RecordControl state="recording" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)
    expect(screen.getByRole('slider', { name: /gain/i })).toBeDisabled()
  })
})

// ---------------------------------------------------------------------------
// Permission denied panel
// ---------------------------------------------------------------------------

describe('RecordControl — permission denied', () => {
  function makeMicError(name = 'MicPermissionDeniedError') {
    const err = new Error('Microphone access was denied.') as Error & { code?: string }
    err.name = name
    err.code = 'mic-permission-denied'
    return err
  }

  it('shows error message and recovery hint when micError has code mic-permission-denied', () => {
    mockEnumerateDevices([])
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        micError={makeMicError()}
      />,
    )
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.getByText(/microphone access was denied/i)).toBeInTheDocument()
    expect(
      screen.getByText((_, element) => element?.tagName === 'P' && /system settings|grant microphone permission/i.test(element.textContent ?? '')),
    ).toBeInTheDocument()
  })

  it('shows an Open System Settings button that calls micOpenSettings', async () => {
    mockEnumerateDevices([])
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        micError={makeMicError()}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /open system settings/i }))
    expect(micOpenSettings).toHaveBeenCalledOnce()
  })

  it('shows a Retry button that calls onRetry', async () => {
    mockEnumerateDevices([])
    const onRetry = vi.fn()
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        micError={makeMicError()}
        onRetry={onRetry}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it('shows error for NotAllowedError name too', () => {
    mockEnumerateDevices([])
    const err = new Error('Not allowed')
    err.name = 'NotAllowedError'
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        micError={err}
      />,
    )
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('renders normally when micError is null', () => {
    mockEnumerateDevices([])
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        micError={null}
      />,
    )
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByRole('button', { name: /record/i })).toBeInTheDocument()
  })
})

// ---------------------------------------------------------------------------
// System audio notice
// ---------------------------------------------------------------------------

describe('RecordControl — system audio notice', () => {
  it.each([
    ['denied' as const],
    ['restricted' as const],
  ])('shows the notice on macOS in idle state when system audio is %s', async (status) => {
    mockEnumerateDevices([])
    setPlatform('MacIntel')
    systemAudioStatus.mockResolvedValueOnce(status)

    render(<RecordControl state="idle" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)

    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.getByText(/system audio capture is not enabled/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /record/i })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /open system settings/i }))
    expect(systemAudioOpenSettings).toHaveBeenCalledOnce()
  })

  it('shows the notice on macOS in recording state when system audio is denied', async () => {
    mockEnumerateDevices([])
    setPlatform('MacIntel')
    systemAudioStatus.mockResolvedValueOnce('denied')

    render(<RecordControl state="recording" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)

    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.getByText(/system audio capture is not enabled/i)).toBeInTheDocument()
  })

  it.each([
    ['granted' as const],
    ['unknown' as const],
    ['not-determined' as const],
  ])('does not show the notice on macOS when system audio is %s', async (status) => {
    mockEnumerateDevices([])
    setPlatform('MacIntel')
    systemAudioStatus.mockResolvedValueOnce(status)

    render(<RecordControl state="idle" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)

    await waitFor(() => expect(systemAudioStatus).toHaveBeenCalledOnce())
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByRole('button', { name: /open system settings/i })).toBeNull()
  })

  it('does not show the notice when system audio is denied but platform is not macOS', async () => {
    mockEnumerateDevices([])
    setPlatform('Linux x86_64')
    systemAudioStatus.mockResolvedValueOnce('denied')

    render(<RecordControl state="idle" elapsedMs={0} onStart={() => {}} onStop={() => {}} />)

    await waitFor(() => expect(systemAudioStatus).not.toHaveBeenCalled())
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByRole('button', { name: /open system settings/i })).toBeNull()
  })
})

// ---------------------------------------------------------------------------
// Device-invalid panel (fix for OverconstrainedError / NotFoundError)
// ---------------------------------------------------------------------------

describe('RecordControl — device-invalid', () => {
  function makeDeviceInvalidError() {
    const err = new Error('The selected microphone is unavailable or was unplugged.') as Error & {
      code?: string
    }
    err.name = 'MicDeviceInvalidError'
    err.code = 'mic-device-invalid'
    return err
  }

  it('shows unavailable-mic message and device picker, NOT the OS settings hint', async () => {
    mockEnumerateDevices([
      { deviceId: 'dev-1', label: 'Built-in Mic', kind: 'audioinput' },
    ])
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        micError={makeDeviceInvalidError()}
      />,
    )
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByText(/unavailable or was unplugged/i)).toBeInTheDocument()
    expect(screen.getByText(/choose another microphone/i)).toBeInTheDocument()
    // Device picker must be visible so the user can pick a different mic.
    expect(screen.getByRole('combobox', { name: /microphone/i })).toBeInTheDocument()
    // OS-settings hint must NOT appear.
    expect(screen.queryByText(/system settings|grant microphone permission/i)).toBeNull()
  })

  it('Retry button calls onRetry when device-invalid', async () => {
    mockEnumerateDevices([])
    const onRetry = vi.fn()
    render(
      <RecordControl
        state="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        micError={makeDeviceInvalidError()}
        onRetry={onRetry}
      />,
    )
    await waitFor(() => expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(onRetry).toHaveBeenCalledOnce()
  })
})
