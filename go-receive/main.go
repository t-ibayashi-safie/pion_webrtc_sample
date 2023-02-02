package main

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
	"golang.org/x/image/vp8"

	"github.com/takumi2786/pion-webrtc_sample/v1/internal/signal"
)

// Channel for PeerConnection to push RTP Packets
// This is the read from HTTP Handler for generating jpeg
var rtpChan chan *rtp.Packet = make(chan *rtp.Packet, 20)

// receivePackets is launched in a goroutine because the main thread is needed
func receivePackets() {
	// Everything below is the pion-WebRTC API! Thanks for using it ❤️.

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	// Set a handler for when a new remote track starts, this handler creates a gstreamer pipeline
	// for the given codec
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		go func() {
			ticker := time.NewTicker(time.Second * 3)
			for range ticker.C {
				rtcpSendErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
				if rtcpSendErr != nil {
					fmt.Println(rtcpSendErr)
				}
			}
		}()

		codecName := strings.Split(track.Codec().RTPCodecCapability.MimeType, "/")[1]
		fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), codecName)

		for {
			rtpPacket, _, readErr := track.ReadRTP()
			if readErr != nil {
				panic(readErr)
			}

			select {
			case rtpChan <- rtpPacket:
			default:
			}
			time.Sleep(5 * time.Millisecond)
		}
	})

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("Connection State has changed %s \n", connectionState.String())
	})

	// Wait for the offer to be pasted
	offer := webrtc.SessionDescription{}
	signal.Decode(signal.MustReadStdin(), &offer)

	// Set the remote SessionDescription
	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		panic(err)
	}

	// Create an answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		panic(err)
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	// Output the answer in base64 so we can paste it in browser
	fmt.Println(signal.Encode(*peerConnection.LocalDescription()))
}

func decodePackets() {
	decoder := vp8.NewDecoder()

	// sampleBuilderは、rtpPacketを溜め込み、フレーム単位で取り出すことができる
	sampleBuilder := samplebuilder.New(20, &codecs.VP8Packet{}, 90000)
	for {
		select {
		case data := <-rtpChan:
			// fmt.Println("データ読めたよ")
			sampleBuilder.Push(data)
		default:
			time.Sleep(100 * time.Millisecond)
			continue
		}
		// フレームが出来上がったら取得する。データが足りない場合は読み込みを続ける。
		sample := sampleBuilder.Pop()
		if sample == nil {
			// fmt.Println("データ不足のようだ")
			continue
		}

		decoder.Init(bytes.NewReader(sample.Data), len(sample.Data))

		// Decode header
		var fh vp8.FrameHeader
		var err error
		if fh, err = decoder.DecodeFrameHeader(); err != nil {
			panic(err)
		}
		fmt.Println(fh)

		// Decode Frame
		img, err := decoder.DecodeFrame()
		if err != nil {
			panic(err)
		}
		fmt.Print(img)

	}
}

func init() {
	// This example uses Gstreamer's autovideosink element to display the received video
	// This element, along with some others, sometimes require that the process' main thread is used
	runtime.LockOSThread()
}

func main() {
	// Start a new thread to do the actual work for this application
	go decodePackets()
	go receivePackets()

	for {
		time.Sleep(10 * time.Millisecond)
	}
}
