package media

import (
	"fmt"

	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v4"

	// Import screen capture driver
	_ "github.com/pion/mediadevices/pkg/driver/screen"
)

var screenTrack mediadevices.Track

// GetScreenTrack returns a VP8 encoded video track for screen sharing
func GetScreenTrack() (webrtc.TrackLocal, error) {
	// Configure VP8 encoder
	vpxParams, err := vpx.NewVP8Params()
	if err != nil {
		return nil, fmt.Errorf("failed to create VP8 params: %w", err)
	}
	vpxParams.BitRate = 1_500_000 // 1.5 Mbps for good quality

	codecSelector := mediadevices.NewCodecSelector(
		mediadevices.WithVideoEncoders(&vpxParams),
	)

	// Get screen capture stream
	stream, err := mediadevices.GetDisplayMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.FrameRate = prop.Float(15) // 15 FPS
			c.Width = prop.Int(1280)
			c.Height = prop.Int(720)
		},
		Codec: codecSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get display media: %w", err)
	}

	tracks := stream.GetVideoTracks()
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no video tracks in stream")
	}

	screenTrack = tracks[0]

	// mediadevices.Track implements webrtc.TrackLocal interface
	return screenTrack, nil
}

// StopScreenShare stops the screen capture
func StopScreenShare() {
	if screenTrack != nil {
		screenTrack.Close()
		screenTrack = nil
	}
}

// StartScreenShare is kept for backwards compatibility but now uses VP8
func StartScreenShare(dc *webrtc.DataChannel) {
	fmt.Println("Warning: StartScreenShare with DataChannel is deprecated. Use GetScreenTrack instead.")
}
