package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"github.com/takumi2786/pion-webrtc_sample/v1/internal/signal"
	"go.uber.org/zap"
)

var iceConnectedCtx context.Context
var iceConnectedCtxCancel context.CancelFunc
var logger *zap.Logger

func initReflect(peerConnection *webrtc.PeerConnection) (*webrtc.TrackLocalStaticRTP, *webrtc.RTPSender) {
	// Create Track that we send video back to browser on
	reflectTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if err != nil {
		panic(err)
	}

	// Add this newly created track to the PeerConnection
	reflectRtpSender, err := peerConnection.AddTrack(reflectTrack)
	if err != nil {
		panic(err)
	}
	return reflectTrack, reflectRtpSender
}

// リモートから送られたRTPをそのまま送り返す
func reflect(peerConnection *webrtc.PeerConnection, outputTrack *webrtc.TrackLocalStaticRTP, rtpSender *webrtc.RTPSender) {
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		// This is a temporary fix until we implement incoming RTCP events, then we would push a PLI only when a viewer requests it
		go func() {
			ticker := time.NewTicker(time.Second * 3)
			for range ticker.C {
				errSend := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
				if errSend != nil {
					fmt.Println(errSend)
				}
			}
		}()

		fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), track.Codec().MimeType)
		for {
			// Read RTP packets being sent to Pion
			rtp, _, readErr := track.ReadRTP()
			if readErr != nil {
				panic(readErr)
			}

			if writeErr := outputTrack.WriteRTP(rtp); writeErr != nil {
				panic(writeErr)
			}
		}
	})
}

func init() {
	// This example uses Gstreamer's autovideosink element to display the received video
	// This element, along with some others, sometimes require that the process' main thread is used
	runtime.LockOSThread()
}

func main() {
	logger, _ = zap.NewDevelopment()
	iceConnectedCtx, iceConnectedCtxCancel = context.WithCancel(context.Background())

	logger.Info("Reflect !")
	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new RTCPeerConnection
	logger.Info("NewPeerConnection")
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	// 候補先情報を受信した場合にそれを表示する
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		logger.Info("OnICECandidate")
		if candidate == nil {
			logger.Info("No candidate")
			return
		}
		logger.Info(fmt.Sprintf("Address: %s, Port: %d", candidate.Address, candidate.Port))
	})

	// 接続状態変更を検知した際に起動するイベントハンドラを設定する
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("Connection State has changed %s \n", connectionState.String())
		// 接続が成功したことをcontextに伝える
		if connectionState == webrtc.ICEConnectionStateConnected {
			iceConnectedCtxCancel()
		}
	})

	// 送信するメディアを設定する
	// videoTrack, rtpSenderは、メディアを送信する際に利用する
	// ※ Local Session Descriptionを生成する前に実行する必要がある
	reflectTrack, reflectRtpSender := initReflect(peerConnection)

	// (オファー) Remote Session DescriptionをpeerConnectionに設定する
	offer := webrtc.SessionDescription{}
	signal.Decode(signal.MustReadStdin(), &offer)
	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		panic(err)
	}

	// (アンサー) Local Session Descriptionを生成する
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
	fmt.Printf("Answer Session Description: \n%s", signal.Encode(*peerConnection.LocalDescription()))

	go reflect(peerConnection, reflectTrack, reflectRtpSender)

	select {}
}
