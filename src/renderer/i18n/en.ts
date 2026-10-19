// English source catalogue. Keep complete messages here rather than joining
// fragments, so translators can reorder words and punctuation naturally.
export const en = {
  'audio.testMic': 'Test microphone',
  'audio.stopMicTest': 'Stop microphone test',
  'audio.micIdle': 'Your microphone is off until you record or test it.',
  'audio.testFailed':
    'Could not test this microphone. Check microphone access and choose an available device.',
  'audio.computerTitle': 'Computer audio (optional)',
  'audio.computerHelp':
    'Include other people speaking in your calls. macOS grants this separately from microphone access. This brief check may show a second permission prompt; no test audio is saved.',
  'audio.enable': 'Enable computer audio',
  'audio.checking': 'Checking computer audio…',
  'audio.ready': 'Computer audio is available. No test audio was saved.',
  'audio.unconfirmed':
    'Computer audio could not be confirmed. Allow Muesli in your system audio privacy settings, then try again. You can continue with microphone-only recording.',
  'audio.settings': 'Open audio privacy settings',
  'help.label': 'Help: {label}',
  'common.loading': 'Loading…',
  'common.retry': 'Try again',
  'common.saving': 'Saving…',
  'common.saved': 'Saved',
  'common.save': 'Save service',
  'transcription.readOnly':
    'Your administrator manages these shared settings. You can record and take notes without changing them.',
  'transcription.title': 'Transcription',
  'transcription.intro':
    'Turn recordings into text. The final transcript is processed after you stop; live transcription shows words while you record.',
  'transcription.local':
    'Whisper is included on this device, with live transcription on by default. You can keep these defaults without installing anything else. AI summaries use a separate service. The bundled model is for English meetings; use a multilingual service for other languages.',
  'transcription.hosted':
    'These choices affect everyone using this server. Your administrator provides the available services.',
  'transcription.batch': 'Final transcript',
  'transcription.batchHelp':
    'Processes the complete recording after you stop. Use the included Whisper service on a local installation, or a service recommended by your server administrator.',
  'transcription.live': 'Live transcript',
  'transcription.liveHelp':
    'Shows an evolving transcript during recording. Turning this off keeps recording and the final transcript available. Live text can differ from the final transcript.',
  'transcription.bundled': 'Whisper on this device',
  'transcription.bundledLive': 'Whisper on this device (live)',
  'transcription.bundledServer': 'Whisper on the server',
  'transcription.bundledLiveServer': 'Whisper on the server (live)',
  'transcription.off': 'Off — final transcript only',
  'transcription.choose': 'Choose a service',
  'transcription.empty':
    'No service is registered. Ask your administrator or add a custom service below.',
  'transcription.loadError':
    'Could not load transcription services. Try again before changing your selection.',
  'transcription.saveError': 'Could not save your choice. Your previous selection is still shown.',
  'transcription.advanced': 'Advanced: connect another transcription service',
  'transcription.customHelp':
    'Only needed if you run a compatible transcription plugin. Your recordings will be sent to the selected service for processing.',
  'transcription.kind': 'Service purpose',
  'transcription.url': 'Service URL',
  'transcription.token': 'Access token',
  'transcription.tokenHelp':
    'Provided by whoever runs the service. It lets Muesli authenticate; leave it blank only if the service does not require one.',
  'transcription.urlPlaceholder': 'https://transcription.example.com',
  'transcription.checking': 'Checking connection…',
  'live.stopped': 'Recording stopped — preview',
  'live.previewHelp':
    'Your live words stay here while the final transcript is prepared. This preview may differ from the final transcript.',
  'live.disconnected': 'Live connection interrupted. Words captured so far are kept below.',
  'transcription.test': 'Test connection',
  'transcription.healthy': 'Connection healthy.',
  'transcription.unhealthy':
    'The service did not respond successfully. Check its URL, token, and availability.',
  'transcription.authError': 'Authentication failed. Check the access token for this service.',
  'transcription.httpError': 'The service returned HTTP {status}. Check its configuration.',
  'transcription.unreachable':
    'Could not reach {url}. Check that the service is running and reachable from the Muesli server.',
  'settings.aiUnknown': 'Could not check AI availability.',
  'transcription.noSelection': 'Select a service before testing its connection.',
  'settings.intro':
    'The default setup is enough to record and transcribe. Personalise the app here; advanced connections and integrations are optional.',
  'settings.appearance': 'Appearance',
  'settings.system': 'Follow system',
  'settings.light': 'Light',
  'settings.dark': 'Dark',
  'settings.templatesHelp':
    'Templates control how meeting summaries are organised. You can keep the included templates.',
  'settings.tagsHelp': 'Use tags to organise and filter your notes. They are optional.',
  'settings.ai': 'AI summaries and search',
  'settings.aiHelp':
    'Optional AI turns transcripts into summaries, answers questions about notes, and finds notes by meaning. Recording and transcription work without AI.',
  'settings.aiHosted':
    'AI services are managed by your server administrator. You do not need to install Ollama on this device.',
  'settings.aiDetected':
    'Ollama was detected when Muesli started. It provides AI summaries and search; transcription uses the services below.',
  'settings.connection': 'Advanced: storage and server connection',
  'settings.connectionHelp':
    'The built-in server stores and processes notes on this device. A team server stores them on infrastructure managed by your team. Switching does not transfer notes between servers.',
  'settings.connectionLocal':
    'Using this device’s built-in server. No separate server setup is needed.',
  'settings.connectionHosted': 'Using a connected server managed by your team.',
  'settings.integrations': 'Advanced: scheduled summaries',
  'settings.digest': 'Scheduled summaries',
  'settings.digestFrequency': 'Frequency',
  'settings.digestHelp':
    'Sends a summary of notes and outstanding actions to your configured delivery integrations. Leave this off unless a delivery integration has been set up.',
  'settings.digestOff': 'Off (default)',
  'settings.digestDaily': 'Daily',
  'settings.digestWeekly': 'Weekly',
  'settings.backgroundHelp':
    'Keeps Muesli available after closing its window. Use Quit to stop the app completely. Automatic recording only runs while Muesli is running.',
  'settings.recordHelp':
    'Starts recording eligible calendar meetings automatically while Muesli is running. Leave this off if you prefer to start each recording yourself.',
  'settings.calendarHelp':
    'Connect a calendar to see upcoming meetings and link notes to events. Calendar access and automatic recording are optional.',
  'wizard.transcriptionHint': 'You can change these choices later in Settings.',
  'dates.today': 'Today',
  'dates.yesterday': 'Yesterday',
  'dates.earlier': 'Earlier',
  'dates.lastWeek': 'Last week',
  'dates.thisMonth': 'This month',
  'dates.older': 'Older',
  'dates.pinned': 'Pinned',
  'dates.meeting': 'Meeting — {date}, {time}',
} as const
