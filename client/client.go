package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Kind int

const (
	_   Kind = iota
	SND      // Sound connection.
	W_F      // Waterfall connection.
)

type Config struct {
	ServerHost  string
	Password    string
	Kind        Kind
	Identify    string
	Compress    bool
	NoWaterfall bool
	AGC         bool
	ManGain     int
}

type Tuning struct {
	Freq int64 // Hz
	Mode
}
type Mode struct {
	ModeName string // "am" "cw" "lsb" "usb" "nbfm" "iq"
	LowCut   int    // Hz
	HighCut  int    // Hz
	Offset   int    // Hz
}

// Common Modes
var NONE = Mode{"", 0, 0, 0}
var AM = Mode{"am", -4900, 4900, 0}
var CW = Mode{"cw", 300, 700, -500}
var LSB = Mode{"lsb", -2700, -300, 0}
var USB = Mode{"usb", 300, 2700, 0}
var AM_NARROW = Mode{"am", -2500, 2500, 0}
var CW_NARROW = Mode{"cw", 470, 530, -500}
var LSB_NARROW = Mode{"lsb", -2200, -300, 0}
var USB_NARROW = Mode{"usb", 300, 2200, 0}
var AM_3500 = Mode{"am", -3500, 3500, 0}
var CW_3500 = Mode{"cw", 200, 3500, -500}
var LSB_3500 = Mode{"lsb", -3500, -200, 0}
var USB_3500 = Mode{"usb", 200, 3500, 0}

type Client struct {
	*Config
	*Tuning
	ClientNum int64
	Info      map[string]string
	Messages  <-chan Message

	conn  *websocket.Conn
	mutex sync.Mutex
}

type Message struct {
	Tag     string
	Payload []byte
	Err     error
}

var lastClientNum int64
var clientNumMutex sync.Mutex

func GetClientNum() int64 {
	clientNumMutex.Lock()
	defer clientNumMutex.Unlock()
	t := time.Now().Unix()
	if t <= lastClientNum {
		t = lastClientNum + 1
		lastClientNum = t
	}
	return t
}

var dialMutex sync.Mutex

func Dial(cf *Config, tun *Tuning) *Client {
	// We get errors back from the KiwiSDR if multiple clients try to connect at once.
	// So use a mutex to space it out.
	dialMutex.Lock()
	defer func() {
		time.Sleep(100 * time.Millisecond)
		dialMutex.Unlock()
	}()

	messages := make(chan Message, 100)
	if cf.ManGain == 0 {
		cf.ManGain = 50
	}
	c := &Client{
		Config:    cf,
		Tuning:    tun,
		ClientNum: GetClientNum(),
		Info:      make(map[string]string),
		Messages:  messages,
	}
	kind := "?"
	switch c.Kind {
	case SND:
		kind = "SND"
	case W_F:
		kind = "W_F"
	}
	path := fmt.Sprintf("/%d/%s", c.ClientNum, kind)
	u := url.URL{Scheme: "ws", Host: cf.ServerHost, Path: path}
	log.Printf("connecting to %s", u.String())
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalln("dial:", err)
	}
	c.conn = conn

	go func() {
		defer func() {
			close(messages)
			log.Printf("CloseGoingAway...")
			c.mutex.Lock()
			defer c.mutex.Unlock()
			err = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
			if err != nil {
				log.Printf("write CloseGoingAway: %v", err)
			}
		}()
	ReceiveLoop:
		for {
			_, bb, err := conn.ReadMessage()
			if err != nil {
				messages <- Message{Err: err}
				log.Println("read:", err)
				break ReceiveLoop
			}
			if len(bb) < 64 {
				log.Printf("recv: %q", bb)
			}
			if len(bb) < 3 {
				messages <- Message{Err: errors.New("received message too short")}
				log.Println("received message too short")
				break ReceiveLoop
			}

			// Always spilt the tag from the payload and send it on the messages channel.
			tag := string(bb[0:3])
			payload := bb[3:]
			messages <- Message{Tag: tag, Payload: payload}

			// Special MSG handling.  Add new info to info, and check for error messages.
			switch tag {
			case "MSG":
				params := strings.Split(string(payload), " ")
				for _, p := range params {
					if p == "" {
						continue
					}
					kv := strings.SplitN(p, "=", 2)
					switch len(kv) {
					case 0:
						continue
					case 1:
						c.Info[kv[0]] = ""
					case 2:
						if kv[0] == "load_cfg" {
							// Decode extra-long encoded json mesage.
							// The parts will get added to info.
							decodeLoadCfg(kv[1], c.Info)
						} else {
							// Just add other messages to info.
							c.Info[kv[0]] = kv[1]
						}
					}
				}
				if _, ok := c.Info["too_busy"]; ok {
					messages <- Message{Err: errors.New("SERVER_TOO_BUSY")}
					break ReceiveLoop
				}
				if val, ok := c.Info["badp"]; ok && val == "1" {
					messages <- Message{Err: errors.New("BAD_PASSWORD")}
					break ReceiveLoop
				}
				if _, ok := c.Info["down"]; ok {
					messages <- Message{Err: errors.New("SERVER_DOWN")}
					break ReceiveLoop
				}
			}
		}
	}()

	// Now that the background receiver is started, log in.
	c.Sendf("SET auth t=kiwi p=%s", url.QueryEscape(c.Password))

	// This is what kiwiclient.py sends, in this order.
	c.Send("SET AR OK in=12000 out=44100") // TODO: what does this mean?
	c.Send("SET squelch=0 max=0")
	c.Send("SET lms_autonotch=0")
	c.Send("SET genattn=0")
	c.Send("SET gen=0 mix=-1")
	c.Sendf("SET ident_user=%s", url.QueryEscape(c.Identify))

	if c.Tuning.Freq > 0 {
		c.Sendf("SET mod=%s low_cut=%d high_cut=%d freq=%.3f",
			c.Tuning.ModeName,
			c.Tuning.LowCut,
			c.Tuning.HighCut,
			float64(c.Tuning.Freq+int64(c.Tuning.Offset))/1000.0)
	}

	c.Sendf("SET agc=%d hang=0 thresh=-100 slope=6 decay=1000 manGain=%d", bool2int(c.AGC), c.ManGain)
	c.Sendf("SET compression=%d", bool2int(c.Compress))
	c.Send("SET OVERRIDE inactivity_timeout=0")
	return c
}

func bool2int(b bool) int {
	if b {
		return 1
	} else {
		return 0
	}
}

// Send the command string s to the KiwiSDR server.
func (c *Client) Send(s string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, []byte(s))
}

// Send the command string, formatted with args, to the KiwiSDR server.
func (c *Client) Sendf(format string, args ...interface{}) error {
	return c.Send(fmt.Sprintf(format, args...))
}

func (c *Client) HangUp() {
	defer func() {
		r := recover()
		if r != nil {
			log.Printf("Hangup: recover: %v", r)
		}
	}()
	log.Printf("Hangup...")
	c.mutex.Lock()
	defer c.mutex.Unlock()
	err := c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
	if err != nil {
		log.Printf("write Hangup: %v", err)
	}
}

func decodeLoadCfg(load_cfg string, info map[string]string) {
	a, err := url.QueryUnescape(load_cfg)
	if err != nil {
		return
	}
	var obj interface{}
	err = json.Unmarshal([]byte(a), &obj)
	if err != nil {
		return
	}
	for k, v := range obj.(map[string]interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			for k2, v2 := range t {
				info["load_cfg."+k+"."+k2] = fmt.Sprintf("%v", v2)
			}
		default:
			info[k] = fmt.Sprintf("%v", v)
		}
	}
}
