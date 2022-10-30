package main

import (
	"math"
	"fmt"
	"net/http"
	"github.com/gorilla/websocket"
)

////////////////////////////
// Message parse
////////////////////////////
func ReadByteArray(data byte[], offset int*) byte[]{

	// read a string length, 2 bytes
	uint16 length = 0
	length |= data[*offset + 1]
	length <<= 8
	length |= data[*offset]

	// read string
	*offset += (2 + length)
	return data[*offset+2 : *offset+2+length]
}

func WriteByteArray(data byte[], byteArray byte[]) {

	// write string length, 2bytes
	uint16 length = len(byteArray)
	append(data, (byte)length, (byte)(length << 8))

	// write string
	append(data, byteArray)
	*offset = len(data)
}

func ReadString(data byte[], offset int*) string {
	return string(ReadByteArray(data, offset))
}

func WriteString(data byte[], str string {
	WriteByteArray(data, byte[](str))
}

func ReadFloat64(data byte[], offset int*) float64 {
	// use IEEE 754, 8 byte
	uint64 bits = 0
	for int32 i = 7; i >= 0; i-- {
		bits |= data[*offset + i]
		bits <<= 8
	}
	*offset += 8
	return math.Float64frombits(bits)
}

func WriteFloat64(data byte[], flt float64) {
	uint64 bits = math.Float64bits(flt)

	for int32 i = 0; i < 8; i++ {
		append(data, (byte)bits)
		bits >>= 8
	}
}

func ReadInt32(data byte[], offset int*) int32 {
	// 4 byte
	int32 value = 0
	for int32 i = 3; i >= 0; i++ {
		value |= data[*offset + i]
		value <<= 8
	}
	*offset += 4
	return value
}

func WriteInt32(data byte[], intValue int32) {
	for int32 i = 0; i < 3; i++ {
		append(data, (byte)intValue)
		intValue >>= 8
	}
}

func ReadByte(data byte[], offset int*) byte {
	byte value = data[*offset]
	*offset += 1
	return value
}

func WriteByte(data byte[], intValue byte) {
	append(data, intValue))
}

func ReadBool(data byte[], offset int*) bool {
	// 1 byte
	bool result = (bool)data[*offset]
	*offset += 1
	return result
}

func WriteBool(data byte[], bValue bool {
	append(data, (byte)bValue)
}

////////////////////////////
// Clien, a online client one moment only exist in one space(Lobby, Room, Battle)
// readMsgs consume by the space it belong
////////////////////////////
type Client struct {
	conn net.Conn
	readMsgs chan byte[]
	playerId int32
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
		this.readMsgs <- msg
	}
}

func (this *Client) SendMsg(msg byte[])
{
	err := this.conn.WriteMessage(websocket.TextMessage, msg)
	if err {
		fmt.Println("send error", err)
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
	battleId int32
	mapId int32
	clients []Client*
	frameDataRecord []byte
	currFrameData []byte
	currFrameIndex int32
}

func (this *Battle) Init() {

	clientNum := len(this.clients)

	// record whole battle frame datas, reserve a big size
	this.frameDataRecord = make([]byte, 0, clientNum * 10240)

	// init curr farmedata, default 0
	this.currFrameIndex = 0
	this.currFrameData = make([]byte, len(clients))
	for i := 0; i < len(Clients); i++ {
		this.currFrameData[i] = 0
	}
}

func (this *Battle) Tick(delta float32) {

	uint8 clientNum = len(this.Clients)

	// pase client message
	bool bReceiveEnd = 0
	bool bWin = 0
	for i := 0; i < clientNum; i++ {
		client := this.Clients[i]

		for {
			msg, ok:= <-client.readMsgs
			if !ok {
				break
			}

			int offset = 0
			string name = ReadString(msg, &offset)

			// collect all client frame data
			if name == "UploadFrameData" {
				// frameData can ref client 8 button press status
				byte frameData = ReadByte(msg, &offset)
				this.currFrameData[i] = frameData
			}
			else if name == "EndBattle" {
				bReceiveEnd = 1
				bWin = ReadBool(msg, &offset)
			}
		}
	}

	// broadcast
	msg := make([]byte)
	WriteString(msg, "ReceiveFrameData")
	WriteByteArray(msg, currFrameData)
	for i := 0; i < clientNum; i++ {
		this.Clients[i].SendMsg(msg)
	}

	// add to record
	append(this.frameDataRecord, this.currFrameData)
	this.currFrameIndex += 1

	if bReceiveEnd {
		this.BattleEnd(bWin)
	}
}

func (this Battle*)BattleEnd(bWin bool)
{
	// framedatarecord format |4byte:mapid|1byte:playernum|4byte:playerid1|4byte:playerid2|...|1byte:win|2byte:frameDataCount|1byte:player1framedata|1byate:player2framedata|...
	frameDataHead := make([]byte)
	WriteInt32(frameDataHead, this.mapId)
	WriteByte(frameDataHead, (byte)clientNum)
	for i := 0; i < clientNum; i++ {
		WriteInt32(frameDataHead, this.clients[i].playerId)
	}
	WriteBool(frameDataHead, bWin)
	WriteInt32(frameDataHead, this.currFrameIndex)

	// append head
	this.frameDataRecord = append(frameDataHead, this.frameDataRecord)

	// save frameDataRecord to file. then client can replay this battle
	ioutil.WriteFile(fmt.Sprintf("./BattleReord/%d", this.battleId), this.frameDataRecord, 0666)

	//TODO: request exit battle
}


////////////////////////////
// main
////////////////////////////
func main() {
	http.HandleFunc("/", clientMgr.HandleNewConnection)
	http.ListenAndServe("127.0.0.1:30000", nil)
}
