# send-rocal-media

手動シグナリングによりブラウザ-サーバー間でWebRTC通信を行うサンプルです。


## How to run
ブラウザで``http://localhost/example/js/send-rocal-media/``を開きます。「Browser base64 Session Description」をコピーします。

以下を実行します。
```bash
echo ${BSD} | ./send-local-media
```

表示された文字列をブラウザの「Golang base64 Session Description」に貼り付けます。


「Start Session」ボタンを押します。


## Note

このサンプルは、dockerコンテナ内部で起動した場合、ホスト上のブラウザと通信できません。

コンテナ内部とホストPCの接続は、いわゆる「NAT超えが必要な状況」に該当します。この場合、Turnサーバーを利用した「NAT超え」が必要になります。

しかし、このサンプルでは、Turnサーバーを利用していないために通信が失敗します。

※ Stunサーバーが返す接続情報を直接利用して通信できない場合、NAT超えが必要になります。
