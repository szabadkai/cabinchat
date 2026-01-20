package media

import (
	"encoding/binary"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

var (
	captureCtx     *malgo.AllocatedContext
	playbackCtx    *malgo.AllocatedContext
	captureDevice  *malgo.Device
	playbackDevice *malgo.Device
)

// StartAudioCapture initializes microphone capture and sends to WebRTC track
// Uses 48kHz sample rate for Opus codec (no manual encoding needed)
func StartAudioCapture(track *webrtc.TrackLocalStaticSample) error {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
	})
	if err != nil {
		return err
	}
	captureCtx = ctx

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = 48000 // Native macOS rate - no resampling needed
	deviceConfig.PeriodSizeInMilliseconds = 20

	onRecv := func(pOutputSample, pInputSample []byte, framecount uint32) {
		// Input is S16LE (2 bytes per sample) at 48kHz
		// Send raw samples - Opus encoding happens in WebRTC layer
		if len(pInputSample) == 0 {
			return
		}

		// Calculate proper duration based on sample count
		duration := time.Duration(float64(framecount) / 48000.0 * float64(time.Second))

		if err := track.WriteSample(media.Sample{Data: pInputSample[:framecount*2], Duration: duration}); err != nil {
			// Silently ignore write errors
		}
	}

	device, err := malgo.InitDevice(captureCtx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onRecv,
	})
	if err != nil {
		return err
	}
	captureDevice = device

	if err := device.Start(); err != nil {
		return err
	}

	return nil
}

// StartAudioPlayback plays audio from a WebRTC track at 48kHz
func StartAudioPlayback(track *webrtc.TrackRemote) error {
	if playbackCtx == nil {
		ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		})
		if err != nil {
			return err
		}
		playbackCtx = ctx
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = 1
	deviceConfig.SampleRate = 48000 // Match native macOS rate
	deviceConfig.PeriodSizeInMilliseconds = 40

	// Buffer for raw S16LE samples (2 bytes each)
	const bufferSize = 48000 // 1 second of audio
	audioBuffer := make(chan int16, bufferSize)

	var lastSample int16 = 0

	// Goroutine to read from WebRTC track
	go func() {
		buf := make([]byte, 4096)
		for {
			n, _, err := track.Read(buf)
			if err != nil {
				return
			}

			// Decode S16LE samples (2 bytes per sample)
			for i := 0; i+1 < n; i += 2 {
				sample := int16(binary.LittleEndian.Uint16(buf[i : i+2]))
				select {
				case audioBuffer <- sample:
				default:
					// Buffer full - drop oldest
					select {
					case <-audioBuffer:
						audioBuffer <- sample
					default:
					}
				}
			}
		}
	}()

	onSend := func(pOutputSample, pInputSample []byte, framecount uint32) {
		for i := 0; i < int(framecount); i++ {
			select {
			case lastSample = <-audioBuffer:
			default:
				// Use last sample for smooth continuation
			}
			binary.LittleEndian.PutUint16(pOutputSample[2*i:2*i+2], uint16(lastSample))
		}
	}

	device, err := malgo.InitDevice(playbackCtx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onSend,
	})
	if err != nil {
		return err
	}
	playbackDevice = device

	// Pre-buffer before starting
	time.Sleep(100 * time.Millisecond)

	if err := device.Start(); err != nil {
		return err
	}

	return nil
}

// StopAudio stops capture and playback
func StopAudio() {
	if captureDevice != nil {
		captureDevice.Uninit()
		captureDevice = nil
	}
	if playbackDevice != nil {
		playbackDevice.Uninit()
		playbackDevice = nil
	}
	if captureCtx != nil {
		captureCtx.Free()
		captureCtx = nil
	}
	if playbackCtx != nil {
		playbackCtx.Free()
		playbackCtx = nil
	}

}
