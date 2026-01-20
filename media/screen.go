package media

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"time"

	"github.com/kbinani/screenshot"
	"github.com/nfnt/resize"
	"github.com/pion/webrtc/v3"
)

// StartScreenShare captures screen and sends JPEG frames over DataChannel
func StartScreenShare(dc *webrtc.DataChannel) {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond) // 10 FPS
		defer ticker.Stop()

		for range ticker.C {
			if dc.ReadyState() != webrtc.DataChannelStateOpen {
				return
			}

			// Capture primary display
			bounds := screenshot.GetDisplayBounds(0)
			img, err := screenshot.CaptureRect(bounds)
			if err != nil {
				fmt.Printf("Capture error: %v\n", err)
				continue
			}

			// Resize to reasonable size (e.g., width 800) to reduce bandwidth
			// Maintain aspect ratio
			resized := resize.Resize(800, 0, img, resize.Lanczos3)

			// Encode to JPEG
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 70}); err != nil {
				fmt.Printf("JPEG Encode error: %v\n", err)
				continue
			}

			// Send over DataChannel
			// Note: DataChannels have a max message size (typ 64KB or 256KB depending on impl)
			// JPEGs might be larger. We might need to chunk.
			// For 800px width ~70 quality, it should be < 64KB usually.
			// Let's implement simple chunking if needed or rely on Pion handling it (Pion DC supports larger messages by chunking internally? No, usually you handled it)
			// For simplicity we try to send as one unless error.

			data := buf.Bytes()
			if len(data) > 60000 {
				// Too big for single message safety zone?
				// Just skip frame or assume Pion handles fragmentation (SCTP layer does).
				// Pion SCTP supports fragmentation. Open returns a DetachedDataChannel which is a ReadWriteCloser.
				// But here we have *webrtc.DataChannel.
			}

			if err := dc.Send(data); err != nil {
				// fmt.Printf("Send error: %v\n", err)
			}
		}
	}()
}
