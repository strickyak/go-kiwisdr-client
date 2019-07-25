// player for KiwiSDF Audio.
package client

import (
	"bytes"
	"encoding/binary"
	"log"
	"time"
)

type AudioClient struct {
	Client *Client
}

type AudioPacket struct {
	Flag     byte
	Sequence int32
	SMeter   uint16
	Samples  []int16
}

func NewAudioClient(client *Client) *AudioClient {
	return &AudioClient{
		Client: client,
	}
}

func (ac *AudioClient) BackgroundPlayForDuration(d time.Duration) <-chan AudioPacket {
	out := make(chan AudioPacket, 500)
	go func() {
		defer close(out)
		stop := time.After(d)
		alive := time.Now().Unix()
		for {
			select {
			case <-stop:
				ac.Client.HangUp()
				return
			case msg := <-ac.Client.Messages:
				if msg.Err == nil && msg.Tag == "SND" {
					out <- ac.ExtractAudioFromMessage(msg)
				}
			}
			if alive != time.Now().Unix() { // Once per second
				ac.Client.Send("SET keepalive")
				alive = time.Now().Unix()
			}
		}
	}()
	return out
}
func (ac *AudioClient) ExtractAudioFromMessage(msg Message) AudioPacket {
	bb := bytes.NewBuffer(msg.Payload)
	// First 10 bytes are header:
	//   0..2 : 'SND'
	//   3: flag: (unsigned byte)
	//   4..7: sequence: little-endian signed int32
	//   8..9: smeter: big-endian unsigned uint16
	// Rest are big-endian signed int16 audio samples.
	// Flag	byte
	// Sequence int32
	// SMeter	uint16
	// Samples []int16
	var p AudioPacket
	err := binary.Read(bb, binary.BigEndian, &p.Flag)
	if err != nil {
		panic("short audio packet (at Flag)")
	}
	err = binary.Read(bb, binary.LittleEndian, &p.Sequence)
	if err != nil {
		panic("short audio packet (at Sequenc)")
	}
	err = binary.Read(bb, binary.BigEndian, &p.SMeter)
	if err != nil {
		panic("short audio packet (at SMeter)")
	}

	// Now we stop using bb.
	// Now we use raw encoded audio bytes bs, for efficiency inside the loop.
	var samples []int16
	bs := msg.Payload[7:]
	if ac.Client.Compress {
		n := len(bs) * 2
		samples = make([]int16, n)
		log.Fatal("decompression not implemented yet")
	} else {
		n := len(bs) / 2
		samples = make([]int16, n)
		for i := 0; i < n; i++ {
			high := 0xFF00 & (uint16(bs[2*i]) << 8)
			low := 0x00FF & uint16(bs[2*i+1])
			samples[i] = int16(high | low)
		}
	}
	p.Samples = samples
	return p
}
