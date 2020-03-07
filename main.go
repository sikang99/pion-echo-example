package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v2"
)

// Reference
// https://github.com/pion/webrtc/tree/master/examples/broadcast/main.go
// https://github.com/pion/webrtc/tree/master/examples/play-from-disk/main.go

const rtcpPLIInterval = time.Second * 3

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

type message struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

func messageReceiver(conn *websocket.Conn, msgch chan message) {
	m := message{}
	for {
		if err := websocket.ReadJSON(conn, &m); err != nil {
			log.Printf("websocket.ReadJSON() returns ClosedError %v\n", err)
			close(msgch)
			return
		} else {
			log.Printf("websocket.ReadJSON() returns %v\n", m)
			msgch <- m
		}
	}
}

func main() {
	vcodec := flag.String("vcodec", "H264", "video codec type (H264/VP8/VP9)")
	acodec := flag.String("acodec", "OPUS", "audio codec type (OPUS)")
	log.Println("codecs:", *vcodec, *acodec)
	flag.Parse()

	m := webrtc.MediaEngine{}

	//-- setting video and audio codec choosed
	switch *vcodec {
	case "H264":
		m.RegisterCodec(webrtc.NewRTPH264Codec(webrtc.DefaultPayloadTypeH264, 90000))
	case "VP8":
		m.RegisterCodec(webrtc.NewRTPVP8Codec(webrtc.DefaultPayloadTypeVP8, 90000))
	case "VP9":
		m.RegisterCodec(webrtc.NewRTPVP9Codec(webrtc.DefaultPayloadTypeVP9, 90000))
	default:
		log.Println("Not support video codec", *vcodec)
		return
	}

	switch *acodec {
	case "OPUS":
		m.RegisterCodec(webrtc.NewRTPOpusCodec(webrtc.DefaultPayloadTypeOpus, 48000))
	default:
		log.Println("Not support audio codec", *acodec)
		return
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data, err := ioutil.ReadFile("index.html")
		if err != nil {
			log.Println(err)
		}
		fmt.Fprintf(w, string(data))
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("upgrader.Upgrade() failed.")
			return
		}
		defer conn.Close()

		msgch := make(chan message)
		go messageReceiver(conn, msgch)

		peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
			ICEServers: []webrtc.ICEServer{
				{
					URLs: []string{"stun:stun.l.google.com:19302", "stun:stun.stunprotocol.org:3478"},
				},
			},
		})
		if err != nil {
			log.Printf("api.NewPeerConnection() failed. %v\n", err)
			return
		}

		var localVideoTrack *webrtc.Track = nil
		var localAudioTrack *webrtc.Track = nil

		peerConnection.OnTrack(func(remoteTrack *webrtc.Track, receiver *webrtc.RTPReceiver) {
			fmt.Printf("peerConnection.OnTrack(%v)\n", remoteTrack)

			go func() {
				ticker := time.NewTicker(rtcpPLIInterval)
				fmt.Printf("On rtcpPLIInterval.\n")
				for range ticker.C {
					fmt.Printf("On rtcpPLIInterval.\n")
					if rtcpSendErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: remoteTrack.SSRC()}}); rtcpSendErr != nil {
						log.Println(rtcpSendErr)
						return
					}
				}
			}()

			for {
				rtpPacket, err := remoteTrack.ReadRTP()
				if err != nil {
					log.Println("remoteTrack.ReadRTP:", err)
					return
				}

				if localVideoTrack == nil {
					log.Printf("no localVideoTrack continue.\n")
					continue
				}

				switch rtpPacket.PayloadType {
				case localVideoTrack.PayloadType():
					rtpPacket.SSRC = localVideoTrack.SSRC()
				case localAudioTrack.PayloadType():
					rtpPacket.SSRC = localAudioTrack.SSRC()
				default:
					continue
				}

				err = localVideoTrack.WriteRTP(rtpPacket)
				if err != nil {
					log.Printf("localVideoTrack.Write() failed. %v\n", err)
					return
				}
			}
		})

		switch *vcodec {
		case "H264":
			localVideoTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypeH264, rand.Uint32(), "video", "pion")
		case "VP8":
			localVideoTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypeVP8, rand.Uint32(), "video", "pion")
		case "VP9":
			localVideoTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypeVP9, rand.Uint32(), "video", "pion")
		}
		if err != nil {
			log.Printf("peerConnection.NewTrack(%s) failed. %v\n", *vcodec, err)
			return
		}

		_, err = peerConnection.AddTrack(localVideoTrack)
		if err != nil {
			log.Printf("peerConnection.AddTrack(Video) failed. %v\n", err)
			return
		}

		localAudioTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypeOpus, rand.Uint32(), "audio", "pion")
		if err != nil {
			log.Printf("peerConnection.NewTrack(OPUS) failed. %v\n", err)
			return
		}

		_, err = peerConnection.AddTrack(localAudioTrack)
		if err != nil {
			log.Printf("peerConnection.AddTrack(Audio) failed. %v\n", err)
			return
		}

		for {
			select {
			case m, ok := <-msgch:
				if ok {
					switch m.Type {
					case "offer":
						{
							desc := webrtc.SessionDescription{
								Type: webrtc.SDPTypeOffer,
								SDP:  m.Payload,
							}
							err = peerConnection.SetRemoteDescription(desc)
							if err != nil {
								log.Printf("peerConnection.SetRemoteDescription() failed. %v\n", err)
								goto close
							}

							answer, err := peerConnection.CreateAnswer(nil)
							if err != nil {
								log.Printf("peerConnection.CreateAnswer() failed. %v\n", err)
								goto close
							}

							err = peerConnection.SetLocalDescription(answer)
							if err != nil {
								log.Printf("peerConnection.CreateAnswer() failed. %v\n", err)
								goto close
							}

							websocket.WriteJSON(conn, &message{
								Type:    "answer",
								Payload: answer.SDP,
							})
						}
					case "candidate":
						{
							candidate := webrtc.ICECandidateInit{
								Candidate: m.Payload,
							}
							err := peerConnection.AddICECandidate(candidate)
							if err != nil {
								log.Printf("peerConnection.AddICECandidate() failed. %v\n", err)
							}
						}
					}
				} else {
					log.Println("channel recv error")
					goto close
				}
			}
		}
	close:
	})

	fmt.Println("Please connect to http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Printf("httpListenAndServe() failed: %v\n", err)
	}
}
