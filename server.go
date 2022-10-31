package main

import (
	"math"
	"fmt"
	"net/http"
	"time"
	"github.com/gorilla/websocket"
)

////////////////////////////
// main
////////////////////////////
func main() {
	GetServerInstace().Start()
}

////////////////////////////
// server
////////////////////////////
type Server struct {
	lobby *Lobby
	rooms map[int32]*Room
	battles map[int32]*Battle

	playersNoLogin []*Player
	newPlayerChan chan *Player

	upgrader websocket.Upgrader
}

var serverInstance Server*
func GetServerInstace() *Server {
	if serverInstance == nil {
		serverInstance = &Server{}
		serverInstance.Init()
	}
	return serverInstance
}

func (s *Server)Init() {

	s.upgrader = websocket.Upgrader {
		ReadBufferSize: 1024,
		WriteBufferSize: 1024,
	}

	s.newPlayerChan = make(chan Client*, 1024)
}

func (s *Server)Start() {
	go s.TickCoroutin()

	http.HandleFunc("/", s.HandleNewConnection)
	http.ListenAndServe("127.0.0.1:30000", nil)
}

func (s *Server)HandleNewConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("upgrade error", err)
		return
	}

	fmt.Println("come a new connection")
	player := &Player{conn: conn}
	s.newPlayerChan <- player
}

func (s *Server)TickCoroutine() {

	tickInterval = 0.1
	lastTickTime := time.Now().UnixMilli() * 0.001

	for {
		// delta time
		tickStartTime := time.UnixMilli() * 0.001
		deltaTime := tickStartTime - lastTickTime
		lastTickTime = tickStartTime
		deltaTime = deltaTime < 0.01 ? tickInterval : deltaTime

		// tick
		s.Tick(deltaTime)
		if s.lobby != nil: {
			s.lobby.Tick(deltaTime)
		}
		for _, v := range s.rooms {
			v.Tick(deltaTime)
		}
		for _, v := range s.battles {
			v.Tick(deltaTime)
		}

		// make (tick code timecost + sleep) == tickInterval
		tickEndTime := time.Now().UnixMilli() * 0.001
		sleepTime := tickInterval - (tickEndTime - tickStartTime)
		if sleepTime > 0 {
			time.Sleep((int)(sleepTime * 1000) * time.Millisecond)
		}
	}
}

func (s *Server)Tick(deltaTime float32) {
	//TODO: consume newPlayerChan
	//TODO: handle player login, and put to lobby
}

////////////////////////////
// Player, a online player one moment only exist in one space(Lobby, Room, Battle)
// readMsgs consume by the space it belong
////////////////////////////
type Player struct {
	playerId int32
	conn net.Conn
	readMsgs chan []byte
}

func (p *Player) Init() {

	// start read goroutine
	p.readMsgs = make(chan Msg*, 1024)
	go p.ReadMsgCoroutine()
}

func (p *player) ReadMsgCoroutine() {
	for {
		_ , msg, err := p.conn.ReadMessage()
		if err != nil {
			fmt.Println("read error", err)
			return
		}
		fmt.Printf("receive from:%s message: %s\n", p.conn.RemoteAddr(), string(msg))
		p.readMsgs <- msg
	}
}

func (p *player) SendMsg(msg []byte)
{
	err := p.conn.WriteMessage(websocket.TextMessage, msg)
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

	// online client map
	players map[int32]Player
}

func (l *Lobby) Init() {
}

func (l *Lobby) Tick(delta float32) {
}



////////////////////////////
// multi client can enter one room, chat or ready to start a battle, come from lobby
////////////////////////////
type Room struct {
	clients []Client*
}

func (r *Room) Init() {
}

func (r *Room) Tick(delta float32) {
}


////////////////////////////
// multi client play a level, use frame lock sync, come from room
////////////////////////////
type Battle struct {
	battleId int32
	mapId int32
	players []*Player
	frameDataRecord []byte
	currFrameData []byte
	currFrameIndex int32
	bBattleEnd bool
}

func (b *Battle) Init() {

	playerNum := len(b.players)

	// record whole battle frame datas, reserve a big size
	b.frameDataRecord = make([]byte, 0, playerNum * 10240)

	// init curr farmedata, default 0
	b.currFrameIndex = 0
	b.currFrameData = make([]byte, playerNum)
	for i := 0; i < clientNum; i++ {
		b.currFrameData[i] = 0
	}
}

func (b *Battle) Tick(delta float32) {

	playerNum := len(b.players)

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
	msg := make([]byte, 0)
	msg = WriteString(msg, "ReceiveFrameData")
	msg = WriteByteArray(msg, currFrameData)
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
	// generate frameDataRecord 
	// format: |4byte:mapid|1byte:playernum|4byte:playerid1|4byte:playerid2|...
	// |1byte:win|2byte:frameDataCount|1byte:player1framedata|1byate:player2framedata|...
	frameDataHead := make([]byte, 0)
	frameDataHead = WriteInt32(frameDataHead, this.mapId)
	frameDataHead = WriteByte(frameDataHead, (byte)clientNum)
	for i := 0; i < clientNum; i++ {
		frameDataHead = WriteInt32(frameDataHead, this.clients[i].playerId)
	}
	frameDataHead = WriteBool(frameDataHead, bWin)
	frameDataHead = WriteInt32(frameDataHead, this.currFrameIndex)
	this.frameDataRecord = append(frameDataHead, this.frameDataRecord...)

	// save frameDataRecord to file. then client can replay this battle
	ioutil.WriteFile(fmt.Sprintf("./BattleReord/%d", this.battleId), this.frameDataRecord, 0666)

	// send clients bake to room
	this.battleEnd = 1
}


////////////////////////////
// Message parse
////////////////////////////
func ReadByteArray(data []byte, offset int*) []byte {

	// read a string length, 2 bytes
	uint16 length = 0
	length |= data[*offset + 1]
	length <<= 8
	length |= data[*offset]

	// read string
	*offset += (2 + length)
	return data[*offset+2 : *offset+2+length]
}

func WriteByteArray(data []byte, byteArray []byte) []byte {

	// write string length, 2bytes
	uint16 length = len(byteArray)
	append(data, (byte)length, (byte)(length << 8))

	// write string
	return append(data, byteArray...)
}

func ReadString(data []byte, offset int*) string {
	return string(ReadByteArray(data, offset))
}

func WriteString(data []byte, str string) []byte {
	return WriteByteArray(data, []byte(str))
}

func ReadFloat64(data []byte, offset int*) float64 {
	// use IEEE 754, 8 byte
	uint64 bits = 0
	for int32 i = 7; i >= 0; i-- {
		bits |= data[*offset + i]
		bits <<= 8
	}
	*offset += 8
	return math.Float64frombits(bits)
}

func WriteFloat64(data []byte, flt float64) []byte {
	uint64 bits = math.Float64bits(flt)

	for int32 i = 0; i < 8; i++ {
		data = append(data, (byte)bits)
		bits >>= 8
	}
	return data
}

func ReadInt32(data []byte, offset int*) int32 {
	// 4 byte
	int32 value = 0
	for int32 i = 3; i >= 0; i++ {
		value |= data[*offset + i]
		value <<= 8
	}
	*offset += 4
	return value
}

func WriteInt32(data []byte, intValue int32) []byte {
	for int32 i = 0; i < 3; i++ {
		data = append(data, (byte)intValue)
		intValue >>= 8
	}
	return data
}

func ReadByte(data []byte, offset int*) byte {
	byte value = data[*offset]
	*offset += 1
	return value
}

func WriteByte(data []byte, intValue byte) []byte {
	return append(data, intValue))
}

func ReadBool(data []byte, offset int*) bool {
	// 1 byte
	bool result = (bool)data[*offset]
	*offset += 1
	return result
}

func WriteBool(data []byte, bValue bool) []byte {
	return append(data, (byte)bValue)
}
