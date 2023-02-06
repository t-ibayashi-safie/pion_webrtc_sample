package main

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/ivfwriter"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
	"github.com/takumi2786/pion-webrtc_sample/v1/internal/signal"
	"go.uber.org/zap"
	"golang.org/x/image/vp8"
)

type rtpChanData struct {
	codec     webrtc.RTPCodecParameters
	rtpPacket *rtp.Packet
}

// Channel for PeerConnection to push RTP Packets
// This is the read from HTTP Handler for generating jpeg
var rtpChan chan rtpChanData = make(chan rtpChanData, 1000)

const RECEIVE_INTERVAL = 10
const DECODE_INTERVAL = 200
const SAVE_INTERVAL = time.Millisecond * 33
const FrameDuration = time.Millisecond * 33

var iceConnectedCtx context.Context
var iceConnectedCtxCancel context.CancelFunc
var logger *zap.Logger

var ivfFile *ivfwriter.IVFWriter
var oggFile *oggwriter.OggWriter

// receivePacketsは、RTP パケットを受信してrtpChanに格納します
func receivePackets(peerConnection *webrtc.PeerConnection) {
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		// これは何のために行っている？
		go func() {
			ticker := time.NewTicker(time.Second * 1)
			for range ticker.C {
				rtcpSendErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
				if rtcpSendErr != nil {
					fmt.Println(rtcpSendErr)
				}
			}
		}()

		codecName := strings.Split(track.Codec().RTPCodecCapability.MimeType, "/")[1]
		fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), codecName)

		ticker := time.NewTicker(FrameDuration)
		for range ticker.C {
			rtpPacket, _, readErr := track.ReadRTP()
			if readErr != nil {
				panic(readErr)
			}

			select {
			case rtpChan <- rtpChanData{codec: track.Codec(), rtpPacket: rtpPacket}:
			default:
			}
		}
	})
}

// decodeToJpgAndSaveは、一定の周期でrtpChanに格納されたパケットをデコードし、JPGとして保存します
func decodeToJpgAndSave() {
	decoder := vp8.NewDecoder()

	// sampleBuilderは、rtpPacketを溜め込み、フレーム単位で取り出すことができる
	sampleBuilder := samplebuilder.New(20, &codecs.VP8Packet{}, 90000)
	i := 0
	ticker := time.NewTicker(time.Millisecond * DECODE_INTERVAL)
	for range ticker.C {
		select {
		case data := <-rtpChan:
			if strings.EqualFold(data.codec.MimeType, webrtc.MimeTypeVP8) {
				sampleBuilder.Push(data.rtpPacket)
			}
		default:
			continue
		}
		// フレームが出来上がったら取得する。データが足りない場合は読み込みを続ける。
		sample := sampleBuilder.Pop()
		if sample == nil {
			logger.Info("sample is not enough skip it.")
			continue
		}

		decoder.Init(bytes.NewReader(sample.Data), len(sample.Data))

		// Decode header
		var fh vp8.FrameHeader
		var err error
		if fh, err = decoder.DecodeFrameHeader(); err != nil {
			// logger.Error("Failed in DecodeFrameHeader", Stack=false)
			logger.Warn("Failed in DecodeFrameHeader")
			continue
		}
		if !fh.KeyFrame {
			// キーフレーム以外に対応していないのでスキップする
			logger.Info("Not Key Frame")
			continue
		}
		fmt.Println(fh)
		// Decode Frame
		img, err := decoder.DecodeFrame()
		if err != nil {
			panic(err)
		}
		if img != nil {
			logger.Info("image decoded!")
		}

		// save image to file
		i++
		buffer := new(bytes.Buffer)
		if err = jpeg.Encode(buffer, img, nil); err != nil {
			//  panic(err)
			fmt.Printf("jpeg Encode Error: %s\r\n", err)
		}

		fo, err := os.Create(fmt.Sprintf("%s%d%s", "./out", i, ".jpg"))

		if err != nil {
			fmt.Printf("image create Error: %s\r\n", err)
			//panic(err)
		}

		if _, err := fo.Write(buffer.Bytes()); err != nil {
			fmt.Printf("image write Error: %s\r\n", err)
			//panic(err)
		}
		// close fo on exit and check for its returned error
		fo.Close()
	}
}

// saveWithoutDecodeは、R一定の周期でrtpChanに格納されたパケットをTPをデコードせずにファイルへ保存します
func saveWithoutDecode() {
	logger.Info("saveWithoutDecode!")
	ticker := time.NewTicker(SAVE_INTERVAL)
	var err error

	ivfFile, err = ivfwriter.New("./out/output.ivf")
	if err != nil {
		panic(err)
	}
	oggFile, err = oggwriter.New("./out/output.ogg", 48000, 2)
	if err != nil {
		panic(err)
	}

	defer oggFile.Close()
	defer ivfFile.Close()
	var data rtpChanData
	for range ticker.C {
		select {
		case data = <-rtpChan:
		default:
			continue
		}
		// RTPをファイルに書き込む
		codec := data.codec
		if strings.EqualFold(codec.MimeType, webrtc.MimeTypeOpus) {
			if err := oggFile.WriteRTP(data.rtpPacket); err != nil {
				panic(err)
			}
		} else if strings.EqualFold(codec.MimeType, webrtc.MimeTypeVP8) {
			if err := ivfFile.WriteRTP(data.rtpPacket); err != nil {
				panic(err)
			}
		}
	}
}

func init() {
	// This example uses Gstreamer's autovideosink element to display the received video
	// This element, along with some others, sometimes require that the process' main thread is used
	runtime.LockOSThread()
}

func main() {
	logger, _ = zap.NewDevelopment()
	iceConnectedCtx, iceConnectedCtxCancel = context.WithCancel(context.Background())

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
		} else if connectionState == webrtc.ICEConnectionStateFailed {
			if closeErr := oggFile.Close(); closeErr != nil {
				panic(closeErr)
			}

			if closeErr := ivfFile.Close(); closeErr != nil {
				panic(closeErr)
			}

			fmt.Println("Done writing media files")

			// Gracefully shutdown the peer connection
			if closeErr := peerConnection.Close(); closeErr != nil {
				panic(closeErr)
			}

			os.Exit(0)
		}
	})

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
	fmt.Printf("Answer Session Description: \n%s\n", signal.Encode(*peerConnection.LocalDescription()))

	go receivePackets(peerConnection)
	// go decodeToJpgAndSave()
	go saveWithoutDecode()

	select {}
}
