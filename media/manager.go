package media

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/pion/webrtc/v4"
)

// SignalMessage represents the JSON payload in a MsgTypeWebRTC
type SignalMessage struct {
	Type          string `json:"type"` // "offer", "answer", "candidate"
	SDP           string `json:"sdp,omitempty"`
	Candidate     string `json:"candidate,omitempty"`
	CandidateMid  string `json:"mid,omitempty"`
	CandidateLine int    `json:"line,omitempty"`
}

// NetworkCallback is a function to send a message over the network
type NetworkCallback func(targetNick string, data string)

// MediaManager handles WebRTC sessions
type MediaManager struct {
	mutex          sync.Mutex
	peerConnection *webrtc.PeerConnection
	sendSignal     NetworkCallback
	app            fyne.App    // Reference to App to create new windows
	mediaWindow    fyne.Window // The separate window for the call
	remoteVideo    *canvas.Image
	localStream    *webrtc.TrackLocalStaticSample

	currentTarget   string
	isSharingScreen bool
}

// NewMediaManager creates a new MediaManager
func NewMediaManager(app fyne.App, sender NetworkCallback) *MediaManager {
	return &MediaManager{
		app:        app,
		sendSignal: sender,
	}
}

// createPeerConnection initializes a new PeerConnection
func (m *MediaManager) createPeerConnection() error {
	if m.peerConnection != nil {
		m.peerConnection.Close()
	}

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return err
	}

	m.peerConnection = pc

	// ICE Candidates
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidate := c.ToJSON()
		payload := SignalMessage{
			Type:          "candidate",
			Candidate:     candidate.Candidate,
			CandidateMid:  *candidate.SDPMid,
			CandidateLine: int(*candidate.SDPMLineIndex),
		}
		data, _ := json.Marshal(payload)
		m.sendSignal(m.currentTarget, string(data))
	})

	// Track Handling (Received Video/Audio)
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Printf("Track has started: %s (%s)\n", track.ID(), track.Kind())
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			err := StartAudioPlayback(track)
			if err != nil {
				fmt.Printf("Failed to start audio playback: %v\n", err)
			}
		}
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			// Handle Screen Share Video
			// For now, just print. Actual rendering requires decoding VP8/H264 frames to image.
			// Fyne doesn't natively support video stream decoding.
			// We might need a simpler visual indicator or use a frame breakdown if possible.
			// Or we assume Audio is the main thing for VOIP.
			// Screenshot sharing sends discrete images which is easier?
			// But WebRTC sends a stream.

			// For this iteration, we will implement Audio fully.
			// Screen sharing might need a custom renderer.
			// We can try to read RTP packets -> decode -> update Fyne image.
			// This is complex.
			// Alternative: "Screensharing" sends screenshots via DataChannel or just low framerate images?
			// The USER asked for "screensharing". WebRTC Video Track is the standard way.
			// To render it in Fyne, we need to decode the frames.
			// We can use `github.com/pion/webrtc/v4/pkg/media/ivfwriter` to dump to file,
			// or use `vpx-go` bindings? No, stick to pure Go if possible.
			// Maybe just Audio for now and basic stub for Video?
			// Or finding a way to display it.
		}
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			// Play audio
			// StartAudioPlayback(track) -> we need playback logic in audio.go too?
			// malgo handles duplex. If we initialized duplex, we just need to feed the speaker.
			// But for now let's focus on sending. Recv playback is needed for 2-way.
			// Currently audio.go only does capture.
		}
	})

	// Handle DataChannel for Screen Share
	pc.OnDataChannel(func(d *webrtc.DataChannel) {
		if d.Label() == "screen" {
			fmt.Println("Received Screen Share DataChannel")
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				img, err := jpeg.Decode(bytes.NewReader(msg.Data))
				if err == nil {
					// Update UI on main thread
					fyne.Do(func() {
						if m.remoteVideo == nil {
							m.createVideoCanvas()
						}
						m.remoteVideo.Image = img
						m.remoteVideo.Refresh()
						if m.mediaWindow != nil {
							m.mediaWindow.Show()
						}
					})
				}
			})
		}
	})

	return nil
}

// createVideoCanvas sets up the Fyne canvas for video
func (m *MediaManager) createVideoCanvas() {
	m.remoteVideo = canvas.NewImageFromImage(nil)
	m.remoteVideo.FillMode = canvas.ImageFillContain
	m.mediaWindow.SetContent(m.remoteVideo)
}

// StartCall initiates a VOIP call (Audio Only)
func (m *MediaManager) StartCall(target string) {
	m.startSession(target, false)
}

// StartShare initiates a Screen Share (Audio + Screen)
func (m *MediaManager) StartShare(target string) {
	m.startSession(target, true)
}

func (m *MediaManager) startSession(target string, shareScreen bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.currentTarget = target
	m.isSharingScreen = shareScreen

	// Create Media Window
	m.mediaWindow = m.app.NewWindow("Call with " + target)
	m.mediaWindow.Resize(fyne.NewSize(600, 400))
	m.mediaWindow.SetOnClosed(func() {
		m.Stop()
	})

	m.setupUI("Calling " + target + "...")
	m.mediaWindow.Show()

	if err := m.createPeerConnection(); err != nil {
		fmt.Printf("Error creating PC: %v\n", err)
		return
	}

	// Add Audio Track (Opus at 48kHz - standard WebRTC codec)
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2,
		}, "audio", "pion_audio")
	if err != nil {
		fmt.Printf("Error creating track: %v\n", err)
		return
	}
	m.peerConnection.AddTrack(audioTrack)
	m.localStream = audioTrack

	// Start Audio Capture
	go StartAudioCapture(audioTrack)

	// If sharing screen, add video track
	if shareScreen {
		videoTrack, err := GetScreenTrack()
		if err != nil {
			fmt.Printf("Error getting screen track: %v\n", err)
		} else {
			_, err = m.peerConnection.AddTrack(videoTrack)
			if err != nil {
				fmt.Printf("Error adding video track: %v\n", err)
			}
		}
	}

	// Create Offer
	offer, err := m.peerConnection.CreateOffer(nil)
	if err != nil {
		fmt.Printf("Error creating offer: %v\n", err)
		return
	}

	if err = m.peerConnection.SetLocalDescription(offer); err != nil {
		fmt.Printf("Error setting local desc: %v\n", err)
		return
	}

	payload := SignalMessage{
		Type: "offer",
		SDP:  offer.SDP,
	}
	data, _ := json.Marshal(payload)
	m.sendSignal(target, string(data))
}

// HandleSignal processes incoming signaling messages
func (m *MediaManager) HandleSignal(from string, data string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var msg SignalMessage
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		fmt.Printf("Error decoding signal: %v\n", err)
		return
	}

	fmt.Printf("Received %s signal from %s\n", msg.Type, from)

	if m.peerConnection == nil {
		m.currentTarget = from
		if err := m.createPeerConnection(); err != nil {
			fmt.Printf("Error creating PC: %v\n", err)
			return
		}

		// Create Media Window on main thread (must wait for it to complete)
		m.mutex.Unlock() // Release lock while waiting for UI
		fyne.DoAndWait(func() {
			m.mediaWindow = m.app.NewWindow("Call with " + from)
			m.mediaWindow.Resize(fyne.NewSize(600, 400))
			m.mediaWindow.SetOnClosed(func() {
				m.Stop()
			})

			m.setupUI("Call from " + from)
			m.mediaWindow.Show()
		})
		m.mutex.Lock() // Re-acquire lock
	}

	switch msg.Type {
	case "offer":
		offer := webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  msg.SDP,
		}
		if err := m.peerConnection.SetRemoteDescription(offer); err != nil {
			fmt.Printf("Error setting remote desc: %v\n", err)
			return
		}

		// Create Answer
		// But first add our own tracks so they are included
		audioTrack, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeOpus,
				ClockRate: 48000,
				Channels:  2,
			}, "audio", "pion_audio")
		if err != nil {
			fmt.Printf("Error creating track: %v\n", err)
		} else {
			m.peerConnection.AddTrack(audioTrack)
			m.localStream = audioTrack
			go StartAudioCapture(audioTrack)
		}

		answer, err := m.peerConnection.CreateAnswer(nil)
		if err != nil {
			fmt.Printf("Error creating answer: %v\n", err)
			return
		}
		if err = m.peerConnection.SetLocalDescription(answer); err != nil {
			fmt.Printf("Error setting local desc: %v\n", err)
			return
		}

		payload := SignalMessage{
			Type: "answer",
			SDP:  answer.SDP,
		}
		respData, _ := json.Marshal(payload)
		m.sendSignal(from, string(respData))

	case "answer":
		answer := webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer,
			SDP:  msg.SDP,
		}
		if err := m.peerConnection.SetRemoteDescription(answer); err != nil {
			fmt.Printf("Error setting remote desc: %v\n", err)
		}

	case "candidate":
		candidate := webrtc.ICECandidateInit{
			Candidate:     msg.Candidate,
			SDPMid:        &msg.CandidateMid,
			SDPMLineIndex: uint16Ptr(msg.CandidateLine),
		}
		if err := m.peerConnection.AddICECandidate(candidate); err != nil {
			fmt.Printf("Error adding candidate: %v\n", err)
		}
	}
}

func (m *MediaManager) setupUI(status string) {
	label := widget.NewLabel(status)
	label.Alignment = fyne.TextAlignCenter

	// Red background for hangup button?
	hangupBtn := widget.NewButton("End Call", func() {
		m.Stop()
	})

	content := container.NewVBox(
		label,
		widget.NewSeparator(),
		hangupBtn,
	)
	m.mediaWindow.SetContent(content)
}

func (m *MediaManager) Stop() {
	if m.peerConnection != nil {
		m.peerConnection.Close()
		m.peerConnection = nil
	}
	StopAudio()

	if m.mediaWindow != nil {
		// Avoid recursive close loop if called from OnClosed
		m.mediaWindow.SetOnClosed(nil)
		m.mediaWindow.Close()
		m.mediaWindow = nil
	}
	m.currentTarget = ""
}

func uint16Ptr(i int) *uint16 {
	v := uint16(i)
	return &v
}
