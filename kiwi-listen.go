// +build main

/*
  kiwi-listen.go connects to the KiwiSDR at the --kiwi address for --duration
  and outputs mono raw "PCM" audio in "s16le" (Signed 16-bit Little Endian) format.

  Specify the --freq in Hz and the --mode (like "am", "cw", "lsb", or "usb")
  to listen to.  To disable Automatic Gain Control in the SDR,
  specify --agc=0 and --mangain=50 (or some number to set the gain manually).
  You can boost (or attenuate) the volume of the output with --outgain=3
  (or some number of decibels to amplify by).

  --printinfo causes it to print various info the server sends to the client.

  Example:
    go run kiwi-listen.go --freq=740000 --mode=am --duration=5s --kiwi=sybil.yak.net --printinfo |
      paplay --rate=12000 --format=s16le --channels=1 --raw /dev/stdin
*/
package main

import (
	"bufio"
	"flag"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/strickyak/go-kiwisdr-client/client"
)

var KIWI = flag.String("kiwi", "sybil.yak.net", "KiwiSDR server to connect to.")
var DURATION = flag.Duration("duration", 366*24*time.Hour, "How long to play.")
var FREQ = flag.Int64("freq", 740000, "Frequency in Hz")
var MODE = flag.String("mode", "am", "am, cw, lsb, usb, etc.   See switch statement.")
var OUTGAIN = flag.Float64("outgain", 0, "Output Gain in decibels (negative quieter, positive louder)")
var AGC = flag.Bool("agc", true, "Enable AGC in receiver")
var MANGAIN = flag.Int("mangain", 50, "Manual Gain in SDR (if no AGC) (10 to 90?)")
var PRINTINFO = flag.Bool("printinfo", false, "Print INFO from SDR to stderr log.")

func main() {
	flag.Parse()
	kiwi := *KIWI
	if !strings.Contains(kiwi, ":") {
		kiwi += ":8073"
	}

	var config = &client.Config{
		ServerHost:  kiwi,
		Kind:        client.SND,
		Identify:    "AudioClient(golang)",
		AGC:         *AGC,
		ManGain:     *MANGAIN,
		NoWaterfall: true,
	}

	var mode client.Mode
	switch *MODE {
	case "am":
		mode = client.AM
	case "cw":
		mode = client.CW
	case "lsb":
		mode = client.LSB
	case "usb":
		mode = client.USB

	case "am_narrow":
		mode = client.AM_NARROW
	case "cw_narrow":
		mode = client.CW_NARROW
	case "lsb_narrow":
		mode = client.LSB_NARROW
	case "usb_narrow":
		mode = client.USB_NARROW

	case "am_3500":
		mode = client.AM_3500
	case "cw_3500":
		mode = client.CW_3500
	case "lsb_3500":
		mode = client.LSB_3500
	case "usb_3500":
		mode = client.USB_3500

	default:
		log.Fatalf("Unknown mode name: %q", *MODE)
	}

	var tuning = &client.Tuning{
		Freq: *FREQ,
		Mode: mode,
	}

	gain := math.Pow(10, *OUTGAIN/10.0) // Decibels to Multiplicative Gain

	// Small buffer for bytes output to stdout.
	w := bufio.NewWriterSize(os.Stdout, 512)
	// Create a Kiwi websocket client.
	c := client.Dial(config, tuning)
	// Wrap an audio client around the Kiwi client.
	ac := client.NewAudioClient(c)
	// Read audio packets from a goroutine that does the reading from the websocket.
	for ap := range ac.BackgroundPlayForDuration(*DURATION) {
		// Read the samples from the audio packet.
		for _, s := range ap.Samples {
			// Apply output gain and clip values that are too large or small.
			s2 := int(gain * float64(s))
			if s2 < -0x7FFF {
				s2 = -0x7FFF
			}
			if s2 > +0x7FFF {
				s2 = +0x7FFF
			}

			// Write signed int16 little endian raw so-called PCM.
			lo := byte(s2 & 255)
			hi := byte((s2 >> 8) & 255)

			err := w.WriteByte(lo)
			if err == nil {
				err = w.WriteByte(hi)
			}
			if err != nil {
				log.Fatalf("cannot write audio to stdout: %v", err)
			}

		}
	}
	if *PRINTINFO {
		for ik, iv := range c.Info {
			log.Printf("[ %40s : %v ]", ik, iv)
		}
	}
}
