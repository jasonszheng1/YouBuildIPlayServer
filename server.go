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
	rooms map[uint32]*Room
	battles map[uint32]*Battle
	maxRoomId uint32
	maxBattleId uint32

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

	s.lobby = &Lobby{}
	s.lobby.Init()
	s.rooms = make(map[uint32]*Room, 0)
	s.battles = make(map[uint32]*Battle, 0)
	s.maxRoomId = 0
	s.maxBattleId = 0

	s.playersNoLogin = make([]*Player, 0)
	s.newPlayerChan = make(chan Client*, 1024)

	s.upgrader = websocket.Upgrader {
		ReadBufferSize: 1024,
		WriteBufferSize: 1024,
	}
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
	player.Init()
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

	// consume newPlayerChan
	for {
		newPlayer, ok:= <-s.newPlayerChan
		if !ok {
			break
		}
		s.playersNoLogin = append(s.playersNoLogin, newPlayer)
	}

	// handle player login, 
	for i := 0; i < len(s.playersNoLogin); i++ {
		player := s.playersNoLogin[i]
		for {
			msg, ok:= <-player.readMsgs
			if !ok {
				break
			}

			int offset = 0
			string name = ReadString(msg, &offset)

			if name == "Login" {
				// remove from server and put to lobby
				player.playerId = ReadUInt32(msg, &offset)
				s.lobby.players[player.playerId] = player
				s.playersNoLogin = append(s.playersNoLogin[:i], s.playersNoLogin[i+1:]...)
				i--
				break
			}
		}
	}
}

////////////////////////////
// Player, a online player one moment only exist in one space(Lobby, Room, Battle)
// readMsgs consume by the space it belong
////////////////////////////
type Player struct {
	playerId uint32
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
	// online client map
	players map[uint32]*Player
}

func (l *Lobby) Init() {
	l.players = make(map[uint32]*Player)
}

func (l *Lobby) Tick(deltaTime float32) {

	s := GetServerInstace()
	for _, player := range l.players {

		for {
			msg, ok:= <-player.readMsgs
			if !ok {
				break
			}

			int offset = 0
			string name = ReadString(msg, &offset)
			if name == "CreateRoom" {

				// crate new room
				uint32 mapId = ReadUInt32(msg, &offset)
				newRoom := &Room{s.maxRoomId++}
				newRoom.Init()

				// player exist lobby, and enter room
				newRoom.players = append(newRoom.players, player)
				delete(l.players, player.playerId)
				s.rooms[newRoom.roomId] = newRoom

				// notify player enter room
				// fmt: EnterRoom|playerNum|playerId1|playerId2|...
				msg := make([]byte, 0)
				WriteString(msg, "EnterRoom")
				WriteUInt32(msg, newRoom.roomId)
				WriteUInt32(msg, newRoom.mapId)
				WriteUInt32(msg, 1)
				WriteUInt32(msg, player.playerId)
				player.SendMsg(msg)

				continue
			}
			if name == "JoinRoom" {
				uint32 roomId = ReadUInt32(msg, &offset)
				room, exist := s.rooms[roomId]
				if exist {
					// player exist lobby, and enter room
					room.players = append(room.players, player)
					delete(l.players, player.playerId)

					// notify all player that a new menber come in
					// fmt: EnterRoom|roomId|mapId|playerNum|playerId1|playerId2|...
					msg := make([]byte, 0)
					WriteString(msg, "EnterRoom")
					WriteUInt32(msg, room.roomId)
					WriteUInt32(msg, room.mapId)
					WriteUInt32(msg, len(room.players))
					for i := 0; i < len(room.players); i++ {
						WriteUInt32(room.players[i].playerId)
					}
					for i := 0; i < len(room.players); i++ {
						room.players[i].SendMsg(msg)
					}
				}
				continue
			}
			if name == "InvitePlayer" {
				continue
			}
		}

	}
}



////////////////////////////
// multi client can enter one room, chat or ready to start a battle, come from lobby
////////////////////////////
type Room struct {
	mapId uint32
	players []*Player // first player is the captain
}

func (r *Room) Init() {
	r.players = make([]*Player, 0)

}

func (r *Room) Tick(delta float32) {

	s = GetServerInstance()
	for i := 0; i < playerNum; i++ {
		player := b.players[i]

		for {
			msg, ok:= <-player.readMsgs
			if !ok {
				break
			}

			int offset = 0
			string name = ReadString(msg, &offset)

			// captain only msg
			if i == 0 {
				if name == "StartBattle" {
					// creat new battle
					newBattle := &Battle{s.maxBattleId++, r.mapId}
					newBattle.players = r.players
					newBattle.Init()

					// dismiss room, players enter battle, and start a new battle
					delete(s.rooms, r.roomId)
					s.battles[newBattle.battleId] = newBattle

					// notify players enter a battle
					// msg format: battleId|mapId|playerNum|playerid1|playerid2|...
					msg := make([]byte, 0)
					WriteString(msg, "EnterBattle")
					WriteUInt32(msg, newBattle.battleId)
					WriteUInt32(msg, r.mapId)
					WriteUInt32(msg, playerNum)
					for j := 0; j < playerNum; j++ {
						WriteUInt32(msg, r.players[j].playerId)
					}
					for j := 0; j < playerNum; j++ {
						j.SendMsg(msg)
					}

					continue
				}
				if name == "KickPlayer" {
					continue
				}
				if name == "DismissRoom" {
					continue
				}
			}

			if name == "Chat" {
				continue
			}
		}
	}
}


////////////////////////////
// multi client play a level, use frame lock sync, come from room
////////////////////////////
type Battle struct {
	battleId uint32
	mapId uint32
	players []*Player
	frameDataRecord []byte
	currFrameData []byte
	currFrameIndex uint32
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
	for i := 0; i < playerNum; i++ {
		player := b.players[i]

		for {
			msg, ok:= <-player.readMsgs
			if !ok {
				break
			}

			int offset = 0
			string name = ReadString(msg, &offset)

			// collect all client frame data
			if name == "UploadFrameData" {
				// frameData can ref client 8 button press status
				byte frameData = ReadByte(msg, &offset)
				s.currFrameData[i] = frameData
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
	for i := 0; i < playerNum; i++ {
		s.players[i].SendMsg(msg)
	}

	// add to record
	s.frameDataRecord = append(s.frameDataRecord, s.currFrameData)
	s.currFrameIndex += 1

	if bReceiveEnd {
		s.BattleEnd(bWin)
	}
}

func (b Battle*)BattleEnd(bWin bool)
{
	// generate frameDataRecord 
	// format: |4byte:mapid|1byte:playernum|4byte:playerid1|4byte:playerid2|...
	// |1byte:win|2byte:frameDataCount|1byte:player1framedata|1byate:player2framedata|...
	playerNum := len(b.players)
	frameDataHead := make([]byte, 0)
	frameDataHead = WriteUInt32(frameDataHead, this.mapId)
	frameDataHead = WriteByte(frameDataHead, (byte)playerNum)
	for i := 0; i < playerNum; i++ {
		frameDataHead = WriteUInt32(frameDataHead, s.player[i].playerId)
	}
	frameDataHead = WriteBool(frameDataHead, bWin)
	frameDataHead = WriteUInt32(frameDataHead, s.currFrameIndex)
	s.frameDataRecord = append(frameDataHead, s.frameDataRecord...)

	//TODO: should not use battleId as filename, because battleId may reset
	// save frameDataRecord to file. then client can replay this battle
	ioutil.WriteFile(fmt.Sprintf("./BattleReord/%d", this.battleId), this.frameDataRecord, 0666)

	// create a new room
	s := GetServerInstance()
	newRoom := &Room{s.maxRoomId++, b.mapId}
	newRoom.players = b.players
	newRoom.Init()

	// send player back to room, and dismiss battle
	s.rooms[newRoom.roomId] = newRoom
	delete(s.battles, b.battleId)

	// motify players enter room
	// fmt: EnterRoom|roomId|mapId|playerNum|playerId1|playerId2|...
	msg := make([]byte, 0)
	WriteString(msg, "EnterRoom")
	WriteUInt32(msg, newRoom.roomId)
	WriteUInt32(msg, newRoom.mapId)
	WriteUInt32(msg, len(newRoom.players))
	for i := 0; i < len(newRoom.players); i++ {
		WriteUInt32(newRoom.players[i].playerId)
	}
	for i := 0; i < len(newRoom.players); i++ {
		newRoom.players[i].SendMsg(msg)
	}
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

func ReadUInt32(data []byte, offset int*) uint32 {
	// 4 byte
	int32 value = 0
	for int32 i = 3; i >= 0; i++ {
		value |= data[*offset + i]
		value <<= 8
	}
	*offset += 4
	return value
}

func WriteUInt32(data []byte, intValue uint32) []byte {
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
