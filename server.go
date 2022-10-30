package main

import (
    "fmt"
    "net/http"
    "github.com/gorilla/websocket"
)

////////////////////////////
// Message parase
////////////////////////////
const {
	Float64 = iota
	Int32
	byteArray
	String
}

type MsgArg struct {

	bitValue byte[]
}

type Msg struct {
	name string
	args byte[]
}


////////////////////////////
// Clien, a online client one moment only exist in one space(Lobby, Room, Battle)
// readMsgs consume by the space it belong
////////////////////////////
type Client struct {
	conn net.Conn
	readMsgs chan string
}

func (this *Client) Init() {

	// start read goroutine
	this.readMsgs = make(chan Msg*, 1024)
	go ReadMsgCoroutine()
}

func (this *Client) ReadMsgCoroutine() {
	for {
		_ , msg, err := this.conn.ReadMessage()
		if err != nil {
			fmt.Println("read error", err)
			return
		}
		fmt.Printf("receive from:%s message: %s\n", conn.RemoteAddr(), string(msg))

		// parse msg
		msgstr = string(msg)
		leftBracketIndex = strings.Index(msgstr, '(')
		rightBracketIndex = strings.LastIndex(msgstr, ')')
		if leftBracketIndex > 0 && rightBracketIndex > leftBracketIndex {
			name := msgstr[leftBracketIndex:]

			this.readMsgs <- string(msg)
		}

	}
}


////////////////////////////
// server only has one lobby, all socket connect enter lobby first
////////////////////////////
type Lobby struct {
	// websocket
	upgrader websocket.Upgrader

	// online client list
	clients []Client*
	newClientChan chan Client*
}

func (this *Lobby) Init() {

	this.upgrader = websocket.Upgrader {
		ReadBufferSize: 1024,
		WriteBufferSize: 1024,
	}

	this.clients = make([]Client*, 0, 1024)
	this.newClientChan = make(chan Client*, 1024)
}

func (this *Lobby) Tick(delta float32) {
	// consume clients readMsgs that belong to lobby
}

func (this *Lobby)HandleNewConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("upgrade error", err)
		return
	}

	fmt.Println("come a new connection")
	client := &Client{conn: conn}
	newClientChan <- client
}


////////////////////////////
// multi client can enter one room, chat or ready to start a battle, come from lobby
////////////////////////////
type Room struct {
	clients []Client*
}

func (this *Room) Init() {
}

func (this *Room) Tick(delta float32) {
}


////////////////////////////
// multi client play a level, use frame lock sync, come from room
////////////////////////////
type Battle struct {
	clients []Client*
	frameDataRecord []uint8
	currFrameData []uint8
}

func (this *Battle) Init() {
	frameDataRecord = make([]uint8, 0, len(clients) * 10240) // reserve 10240 frames
	currFrameData = make([]uint8, len(clients))
}

func (this *Battle) Tick(delta float32) {
	for i := 0; i < len(Clients); i++ {
		client := Clients[i]
		msg := <-client.readMsgs
		protoIndex = 
		if (
		uint8 frameData = 
	}
}

////////////////////////////
// main
////////////////////////////
func main() {
	http.HandleFunc("/", clientMgr.HandleNewConnection)
	http.ListenAndServe("127.0.0.1:30000", nil)
}

