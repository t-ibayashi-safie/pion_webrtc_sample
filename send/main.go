package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264reader"
	"github.com/takumi2786/pion-webrtc_sample/v1/internal/signal"
	"go.uber.org/zap"
)

// Channel for PeerConnection to push RTP Packets
// This is the read from HTTP Handler for generating jpeg

const videoFileName = "output.h264"
const h264FrameDuration = time.Millisecond * 33

var iceConnectedCtx context.Context
var iceConnectedCtxCancel context.CancelFunc
var logger *zap.Logger

func initSendLocalMedia(peerConnection *webrtc.PeerConnection) (*webrtc.TrackLocalStaticSample, *webrtc.RTPSender) {
	sendLocalMediaTrack, videoTrackErr := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
	if videoTrackErr != nil {
		panic(videoTrackErr)
	}
	sendLocalMediaRtpSender, videoTrackErr := peerConnection.AddTrack(sendLocalMediaTrack)
	if videoTrackErr != nil {
		panic(videoTrackErr)
	}
	return sendLocalMediaTrack, sendLocalMediaRtpSender
}

// ローカルファイルをリモートに送信する
func sendLocalMedia(peerConnection *webrtc.PeerConnection, videoTrack *webrtc.TrackLocalStaticSample, rtpSender *webrtc.RTPSender) {
	// 受け取ったRTCPパケットを読み取ります
	// これらのパケットが返される前に、Nackのようなインターセプターによって処理されます。
	// TODO: RTCPパケットに応じた再送処理を追加する
	go func() {
		rtcpPackets, _, rtcpErr := rtpSender.ReadRTCP()
		if rtcpErr != nil {
			panic(rtcpErr)
		}
		//
		for _, r := range rtcpPackets {
			// RTCPパケットの中身を表示する
			if stringer, canString := r.(fmt.Stringer); canString {
				logger.Info(fmt.Sprintf("Received RTCP Packet: %v", stringer.String()))
			}
		}
	}()

	go func() {
		// Open a H264 file and start reading using our IVFReader
		file, h264Err := os.Open(videoFileName)
		if h264Err != nil {
			panic(h264Err)
		}

		h264, h264Err := h264reader.NewReader(file)
		if h264Err != nil {
			panic(h264Err)
		}

		// 接続が確立されるまで待ちます
		<-iceConnectedCtx.Done()

		//
		// Video File1フレームずつ送信する。メディアデータは、それが再生されるのと同じペースで送信する。
		// This isn't required since the video is timestamped, but we will such much higher loss if we send all at once.
		//
		// time.sleep.sleepの代わりにticker.tickerを使用することが重要です!
		// * avoids accumulating skew, just calling time.Sleep didn't compensate for the time spent parsing the data
		// * works around latency issues with Sleep (see https://github.com/golang/go/issues/44343)
		ticker := time.NewTicker(h264FrameDuration)
		for ; true; <-ticker.C { // 一定の間隔でメディアを送信する
			nal, h264Err := h264.NextNAL()
			if h264Err == io.EOF {
				fmt.Printf("All video frames parsed and sent")
				os.Exit(0)
			}
			if h264Err != nil {
				panic(h264Err)
			}
			// NAL: Network Abstraction Layer
			// http://up-cat.net/H%252E264%252FAVC%2528NAL%2529.html
			if h264Err = videoTrack.WriteSample(media.Sample{Data: nal.Data, Duration: time.Second}); h264Err != nil {
				panic(h264Err)
			}
		}
	}()
}

func init() {
	// This example uses Gstreamer's autovideosink element to display the received video
	// This element, along with some others, sometimes require that the process' main thread is used
	runtime.LockOSThread()
}

func main() {
	logger, _ = zap.NewDevelopment()
	iceConnectedCtx, iceConnectedCtxCancel = context.WithCancel(context.Background())

	logger.Info("Send Local Media to Browser!")

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
	sendLocalMediaTrack, sendLocalMediaRtpSender := initSendLocalMedia(peerConnection)

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

	go sendLocalMedia(peerConnection, sendLocalMediaTrack, sendLocalMediaRtpSender)
	select {}
}
