import CoreAudio
import Foundation

// muesli-audiotap (S4)
// Read the macOS system-audio process tap and stream interleaved float32 PCM on
// stdout. Protocol: one text header line "META sr=<rate> ch=<n> fmt=f32le\n",
// then raw little-endian interleaved float32 frames until SIGTERM/SIGINT, then
// tear down the IOProc + aggregate device + tap.
//
// NOTE: the IOProc copies frames and dispatches the stdout write off the audio
// thread. A preallocated lock-free ring buffer is the RT-safe hardening follow-up.

let stderrH = FileHandle.standardError
func elog(_ s: String) { stderrH.write((s + "\n").data(using: .utf8)!) }
func fail(_ m: String, _ st: OSStatus? = nil) -> Never {
    elog("muesli-audiotap: \(m)" + (st.map { " (OSStatus=\($0))" } ?? "")); exit(1)
}

// Globals so the IOProc (a C function pointer) captures nothing.
let gStdout = FileHandle.standardOutput
let gWriteQueue = DispatchQueue(label: "org.muesli.audiotap.write")
var gChannels = 2

func defaultOutputUID() -> String? {
    var devID = AudioObjectID(kAudioObjectUnknown)
    var size = UInt32(MemoryLayout<AudioObjectID>.size)
    var addr = AudioObjectPropertyAddress(
        mSelector: kAudioHardwarePropertyDefaultOutputDevice,
        mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
    if AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size, &devID) != noErr { return nil }
    if devID == kAudioObjectUnknown { return nil }
    var uid: Unmanaged<CFString>?
    var us = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
    var ua = AudioObjectPropertyAddress(
        mSelector: kAudioDevicePropertyDeviceUID,
        mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
    if AudioObjectGetPropertyData(devID, &ua, 0, nil, &us, &uid) != noErr { return nil }
    return uid?.takeRetainedValue() as String?
}

// 1) Global stereo tap of all system output.
let tapDesc = CATapDescription(stereoGlobalTapButExcludeProcesses: [])
tapDesc.name = "muesli-audiotap"
let tapUID = tapDesc.uuid.uuidString
var tapID = AudioObjectID(kAudioObjectUnknown)
if AudioHardwareCreateProcessTap(tapDesc, &tapID) != noErr { fail("create tap failed") }

// 2) Tap format (sample rate + channels + interleaving).
var asbd = AudioStreamBasicDescription()
var asbdSize = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
var fmtAddr = AudioObjectPropertyAddress(
    mSelector: kAudioTapPropertyFormat, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
if AudioObjectGetPropertyData(tapID, &fmtAddr, 0, nil, &asbdSize, &asbd) != noErr {
    asbd.mSampleRate = 48000; asbd.mChannelsPerFrame = 2
}
let sampleRate = Int(asbd.mSampleRate > 0 ? asbd.mSampleRate : 48000)
gChannels = Int(asbd.mChannelsPerFrame > 0 ? asbd.mChannelsPerFrame : 2)
elog("muesli-audiotap: tap format sr=\(sampleRate) ch=\(gChannels)")

// 3) Aggregate device: private, with the default output as main sub-device (clock).
guard let outUID = defaultOutputUID() else { AudioHardwareDestroyProcessTap(tapID); fail("no default output UID") }
let aggUID = "org.muesli.audiotap." + UUID().uuidString
let aggDesc: [String: Any] = [
    kAudioAggregateDeviceNameKey as String: "Muesli System Audio",
    kAudioAggregateDeviceUIDKey as String: aggUID,
    kAudioAggregateDeviceIsPrivateKey as String: 1,
    kAudioAggregateDeviceTapAutoStartKey as String: 1,
    kAudioAggregateDeviceMainSubDeviceKey as String: outUID,
    kAudioAggregateDeviceSubDeviceListKey as String: [[kAudioSubDeviceUIDKey as String: outUID] as [String: Any]],
    kAudioAggregateDeviceTapListKey as String: [
        [kAudioSubTapDriftCompensationKey as String: 1, kAudioSubTapUIDKey as String: tapUID] as [String: Any],
    ],
]
var aggID = AudioObjectID(kAudioObjectUnknown)
if AudioHardwareCreateAggregateDevice(aggDesc as CFDictionary, &aggID) != noErr {
    AudioHardwareDestroyProcessTap(tapID); fail("create aggregate device failed")
}

// 4) IOProc -> interleaved float32 -> stdout (off the audio thread).
let ioProc: AudioDeviceIOProc = { (_, _, inInputData, _, _, _, _) -> OSStatus in
    let abl = UnsafeMutableAudioBufferListPointer(UnsafeMutablePointer(mutating: inInputData))
    if abl.count == 0 { return noErr }
    var out = Data()
    if abl.count == 1 {
        // Interleaved (or mono): copy raw float bytes.
        let b = abl[0]
        if let d = b.mData, b.mDataByteSize > 0 { out.append(Data(bytes: d, count: Int(b.mDataByteSize))) }
    } else {
        // Non-interleaved planar: interleave the per-channel buffers.
        let nCh = abl.count
        let frames = Int(abl[0].mDataByteSize) / 4
        if frames > 0 {
            var inter = [Float](repeating: 0, count: frames * nCh)
            for c in 0..<nCh {
                guard let d = abl[c].mData else { continue }
                let p = d.bindMemory(to: Float.self, capacity: frames)
                for i in 0..<frames { inter[i * nCh + c] = p[i] }
            }
            inter.withUnsafeBytes { raw in out.append(contentsOf: raw) }
        }
    }
    if !out.isEmpty { gWriteQueue.async { gStdout.write(out) } }
    return noErr
}

var procID: AudioDeviceIOProcID?
if AudioDeviceCreateIOProcID(aggID, ioProc, nil, &procID) != noErr {
    AudioHardwareDestroyAggregateDevice(aggID); AudioHardwareDestroyProcessTap(tapID); fail("create IOProc failed")
}

// 5) Header first (serialized through the same write queue), then start IO.
gWriteQueue.sync { gStdout.write("META sr=\(sampleRate) ch=\(gChannels) fmt=f32le\n".data(using: .utf8)!) }
if AudioDeviceStart(aggID, procID) != noErr {
    AudioDeviceDestroyIOProcID(aggID, procID!); AudioHardwareDestroyAggregateDevice(aggID); AudioHardwareDestroyProcessTap(tapID)
    fail("device start failed")
}
elog("muesli-audiotap: streaming (SIGTERM to stop)")

// 6) Teardown on signal.
signal(SIGTERM, SIG_IGN); signal(SIGINT, SIG_IGN)
let teardown: () -> Void = {
    if let p = procID { AudioDeviceStop(aggID, p); AudioDeviceDestroyIOProcID(aggID, p) }
    AudioHardwareDestroyAggregateDevice(aggID)
    AudioHardwareDestroyProcessTap(tapID)
    elog("muesli-audiotap: torn down")
    exit(0)
}
let s1 = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
let s2 = DispatchSource.makeSignalSource(signal: SIGINT, queue: .main)
s1.setEventHandler(handler: teardown); s2.setEventHandler(handler: teardown)
s1.resume(); s2.resume()
dispatchMain()
