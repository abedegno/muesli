# testdata

`jfk.wav` is the standard whisper.cpp CPU sample fixture: an ~11 second,
16kHz mono 16-bit PCM WAV clip of a public-domain John F. Kennedy speech
excerpt. It is the same fixture whisper.cpp itself ships under `samples/`
and is used here, unmodified, only by the `whisper_cgo`-tagged integration
test to exercise the real engine end-to-end.

Source: https://github.com/ggml-org/whisper.cpp/blob/master/samples/jfk.wav
(pinned commit 080bbbe85230f624f0b52127f1ae1218247989f9).
