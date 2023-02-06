/* eslint-env browser */
// stunサーバーとのコンクションを作成する
let pc = new RTCPeerConnection({
  iceServers: [
    {
      urls: 'stun:stun.l.google.com:19302'
    }
  ]
})

// WebRTCでサーバーへ映像を送信する
sendVideoStream()

// 接続状態を監視し、ロギングする
pc.oniceconnectionstatechange = e => log(pc.iceConnectionState)

// 接続先候補が見つ買った場合にlocalSessionDescriptionを記載するイベントハンドラを設定
// SessionDescriptionとは、P2P接続に必要な情報をまとめたもの(IPやポートなどの情報)
pc.onicecandidate = function (event) {
  if (event.candidate === null) {
    document.getElementById('localSessionDescription').value = btoa(JSON.stringify(pc.localDescription))
  }
}

// 受信した場合のイベントハンドラを設定
pc.ontrack = function (event) {
  console.log("ontrack")
  var el = document.createElement(event.track.kind)
  el.srcObject = event.streams[0]
  el.autoplay = true
  el.controls = true
  document.getElementById('remoteVideos').appendChild(el)
}

// ログ出力用
function log(msg) {
  document.getElementById('logs').innerHTML += msg + '<br>'
}

// #localVideosにウェブカメラ映像を放映する
function displayVideo(video) {
  var el = document.createElement('video')
  el.srcObject = video
  el.autoplay = true
  el.muted = true
  el.width = 160
  el.height = 120

  document.getElementById('localVideos').appendChild(el)
  return video
}

async function sendVideoStream() {
  stream = await navigator.mediaDevices.getUserMedia({ video: true, audio: false })

  try {
    stream.getTracks().forEach(function(track) {
      pc.addTrack(track, stream);
    });
  } catch(err) {
    log(err)
  }
  displayVideo(stream);

  try {
    offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
  } catch(err) {
    log(err)
  }
}

// start sessionボタン押下時
async function StartSession () {
  let sd = document.getElementById('remoteSessionDescription').value
  if (sd === '') {
    return alert('Session Description must not be empty')
  }

  try {
    pc.setRemoteDescription(new RTCSessionDescription(JSON.parse(atob(sd))))
  } catch (e) {
    alert(e)
  }
}

// AddDisplayCaptureボタン押下時
async function AddDisplayCapture() {
  stream = await navigator.mediaDevices.getDisplayMedia()
  document.getElementById('displayCapture').disabled = true

  stream.getTracks().forEach(function(track) {
    pc.addTrack(track, displayVideo(stream));
  });
  offer = await pc.createOffer()
  try{
    pc.setLocalDescription(offer)
  } catch (e) {
    alert(e)
  }
}
