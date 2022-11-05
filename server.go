package main

import (
    "math"
    "fmt"
    "sync"
    "net/http"
    "io/ioutil"
    "time"
    "crypto/md5"
    "hash"
    "github.com/gorilla/websocket"
    _"github.com/go-sql-driver/mysql"
    "github.com/jmoiron/sqlx"
)

////////////////////////////
// main
////////////////////////////
func main() {
    GetServerInstance().Start()
}

////////////////////////////
// server
////////////////////////////
type Server struct {
    lobby *Lobby
    rooms map[uint32]*Room
    battles map[uint32]*Battle
    maxMapId uint32
    maxRoomId uint32
    maxBattleId uint32

    playersNoLogin []*Player
    newPlayerChan chan *Player

    upgrader websocket.Upgrader

    db *sqlx.DB
}

var serverInstance *Server
func GetServerInstance() *Server {
    if serverInstance == nil {
        serverInstance = &Server{}
        serverInstance.Init()
    }
    return serverInstance
}

func (s *Server)Init() {

    s.lobby = &Lobby{}
    s.lobby.Init()
    s.rooms = make(map[uint32]*Room)
    s.battles = make(map[uint32]*Battle)
    s.maxMapId = 0
    s.maxRoomId = 0
    s.maxBattleId = 0

    s.playersNoLogin = make([]*Player, 0, 128)
    s.newPlayerChan = make(chan *Player, 32)

    s.upgrader = websocket.Upgrader {
        ReadBufferSize: 1024,
        WriteBufferSize: 1024,
    }
}

func (s *Server)Start() {

    // connect sql
    //TODO:close connect on program terminate, where is the callback? ask json?
    var (
        userName string = "root"
        password string = "super789987"
        ip string = "127.0.0.1"
        port int = 3306
        dbName string = "YouBuildIPlay"
        charset string = "utf8"
    )
    dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s", userName, password, ip, port, dbName, charset)
    db, err := sqlx.Open("mysql", dsn)
    if err != nil {
        fmt.Println("connect mysql fail:", err.Error())
        s.db = nil
        return
    } else {
        fmt.Println("connect mysql success")
        s.db = db
    }

    // init global vars
    globalTable := &GlobalTable{}
    err = s.db.Get(globalTable, "select * from Global")
    if err != nil {
        fmt.Println("db connection is damaged!", err)
        return
    }
    s.maxMapId = globalTable.MaxMapId
    s.maxBattleId = globalTable.MaxBattleId

    // main tick
    go s.TickCoroutine()

    // listening connection
    http.HandleFunc("/", s.HandleNewConnection)
    fmt.Println("server started, listening 127.0.0.1:30000...")
    http.ListenAndServe("127.0.0.1:30000", nil)
}

func (s *Server)HandleNewConnection(w http.ResponseWriter, r *http.Request) {
    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        fmt.Println("upgrade error", err)
        return
    }

    player := &Player{conn: conn}
    player.Init()
    s.newPlayerChan <- player
}

func (s *Server)TickCoroutine() {

    // here all the time are Millisecond 
    firstTick := true
    var tickInterval int64 = 100
    lastTickTime := time.Now().UnixMilli()

    for {
        // delta time
        tickStartTime := time.Now().UnixMilli()
        deltaTime := tickStartTime - lastTickTime
        lastTickTime = tickStartTime
        if firstTick {
            deltaTime = tickInterval
            firstTick = false
        }

        // tick
        deltaTimeSecond := float32(deltaTime) * 0.001
        s.Tick(deltaTimeSecond)
        s.lobby.Tick(deltaTimeSecond)
        for _, v := range s.rooms {
            v.Tick(deltaTimeSecond)
        }
        for _, v := range s.battles {
            v.Tick(deltaTimeSecond)
        }

        // sleepTime + codeCostTime == tickInterval
        tickEndTime := time.Now().UnixMilli()
        sleepTime := tickInterval - (tickEndTime - tickStartTime)
        if sleepTime > 0 {
            time.Sleep(time.Duration(sleepTime) * time.Millisecond)
        }
    }
}

func (s *Server)Tick(deltaTime float32) {

    // consume newPlayerChan
    for {
        bNoNewPlayer := false
        select {
            case newPlayer := <-s.newPlayerChan:
                s.playersNoLogin = append(s.playersNoLogin, newPlayer)
            default:
                bNoNewPlayer = true
        }
        if bNoNewPlayer {
            break
        }
    }

    // handle player login
    for i := 0; i < len(s.playersNoLogin); i++ {
        player := s.playersNoLogin[i]
        for {
            bNoNewMsg := false
            var msg []byte
            select {
                case msg = <-player.readMsgs:
                default:
                    bNoNewMsg = true
            }
            if bNoNewMsg {
                break
            }

            offset := 0
            name := ReadString(msg, &offset)
            if name == "Login" {
                fmt.Println(msg, len(msg))
                // remove from server and put to lobby
                player.playerId = ReadUInt32(msg, &offset)
                player.playerName = ReadString(msg, &offset)
                s.lobby.players[player.playerId] = player
                s.playersNoLogin = append(s.playersNoLogin[:i], s.playersNoLogin[i+1:]...)
                i--
                fmt.Println("player login", player.playerId, player.playerName)
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
    playerName string

    readMsgs chan []byte
    sendMutex sync.Mutex

    conn* websocket.Conn
    disconnect bool

    roomId uint32
    battleId uint32

    // mapdata is upload by many part(msg), so need cache, after upload all part, combine them and save to file
    // after combine them, we check the md5
    mapPartDataCache []byte
    mapExpectSize uint32
    mapReceiveSize uint32
    mapExpectMd5 []byte
    mapReceiveMd5 hash.Hash
}

func (p *Player) Init() {

    fmt.Println("come a new connection")

    // start read goroutine
    p.readMsgs = make(chan []byte, 32)
    go p.ReadMsgCoroutine()
    p.disconnect = false

    // 0 means not in room or battle
    p.roomId = 0
    p.battleId = 0

    // map receive cache
    p.mapPartDataCache = make([]byte, 0)
    p.mapExpectSize = 0
    p.mapReceiveSize = 0
    p.mapExpectMd5 = make([]byte, 0, 16)
}

func (p *Player) ReadMsgCoroutine() {
    for {
        _ , msg, err := p.conn.ReadMessage()
        if err != nil {
            fmt.Println("player disconnect", p.playerId, err)
            p.disconnect = true
            //TODO kick off lobby/room/battle
            return
        }
        fmt.Printf("receive from:%s message: %s\n", p.conn.RemoteAddr(), string(msg))
        p.readMsgs <- msg
    }
}

func (p *Player) SendMsg(msg []byte) {
    p.sendMutex.Lock()
    p.conn.WriteMessage(websocket.TextMessage, msg)
    p.sendMutex.Unlock()
}

func (p *Player) OnReceiveMapPartData(mapPartData []byte) {
    s := GetServerInstance()

    // append part data
    p.mapReceiveSize += uint32(len(mapPartData))
    p.mapReceiveMd5.Write(mapPartData)
    p.mapPartDataCache = append(p.mapPartDataCache, mapPartData...)

    // receive end, check md5 and save to file
    if p.mapReceiveSize >= p.mapExpectSize {

        // check file md5
        checkMd5Success := true
        receiveMd5 := p.mapReceiveMd5.Sum(nil) 
        for i := 0; i < 16; i++ {
            if receiveMd5[i] != p.mapExpectMd5[i] {
                checkMd5Success = false
                break
            }
        }

        // check end, send upload result
        sendMsg := make([]byte, 0, 32)
        sendMsg = WriteString(sendMsg, "UploadMapRespone")
        sendMsg = WriteBool(sendMsg, checkMd5Success)
        if !checkMd5Success {
            sendMsg = WriteString(sendMsg, "Md5CheckFail")
        }
        p.SendMsg(sendMsg)

        if checkMd5Success {

            // save map to file
            // client should zip the file
            s.maxMapId++
            newMapId := s.maxMapId
            ioutil.WriteFile(fmt.Sprintf("./maps/%d", newMapId), p.mapPartDataCache, 0666)

            // insert a record to db
            _, err := s.db.Exec("insert into Map values(?,?,?,?,?)", newMapId, p.playerId, p.mapReceiveSize, string(receiveMd5), 0)
            if err != nil {
                fmt.Println(err)
            }

            // update max mapId to db
            _, err1 := s.db.Exec("update Global set maxMapId = ?", newMapId)
            if err1 != nil {
                fmt.Println(err1)
            }
        }

        // receive end, reset to refuse next body upload
        p.mapExpectSize = 0
    }
}

// file size too big will cause server lag, so create a thread to do this
func (p *Player) SendFileCoroutine(filePath string, msgName string) {

    // file not exist?
    fileData, err := ioutil.ReadFile(filePath)
    if err != nil {
        responeMsg := make([]byte, 0, 32)
        responeMsg = WriteString(responeMsg, fmt.Sprintf("%sHead", msgName))
        responeMsg = WriteBool(responeMsg, false)
        responeMsg = WriteString(responeMsg, "RequestFileMissing??")
        p.SendMsg(responeMsg)
        return
    }

    fileSize := len(fileData)
    fileMd5Array := md5.Sum(fileData)
    fileMd5 := make([]byte, 16)
    copy(fileMd5, fileMd5Array[:])

    // Send file content
    bHead := true
    sendSize := 0
    for {

        responeMsg := make([]byte, 0, 1024)
        remainSendSize := 1024

        if bHead {
            responeMsg = WriteString(responeMsg, fmt.Sprintf("%sHead", msgName))
            responeMsg = WriteBool(responeMsg, true)
            responeMsg = WriteUInt32(responeMsg, uint32(fileSize))
            responeMsg = WriteByteArray(responeMsg, fileMd5)
            remainSendSize = 1024 - len(responeMsg)
        } else {
            responeMsg = WriteString(responeMsg, fmt.Sprintf("%sBody", msgName))
        }

        remainSendSize -= 2 // 2 byte respone to the size of part data
        sendEnd := false
        if remainSendSize + sendSize >= fileSize {
            remainSendSize = fileSize - sendSize
            sendEnd = true
        }

        // send file part
        WriteByteArray(responeMsg, fileData[sendSize : (sendSize+remainSendSize)])
        p.SendMsg(responeMsg)
        sendSize += remainSendSize

        // send end
        if sendEnd {
            break
        }

        // sleep to avoid lap
        time.Sleep(10 * time.Millisecond)
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

    s := GetServerInstance()
    for _, player := range l.players {

        for {
            bNoNewMsg := false
            var msg []byte
            select {
                case msg = <-player.readMsgs:
                default:
                    bNoNewMsg = true
            }
            if bNoNewMsg {
                break
            }

            offset := 0
            name := ReadString(msg, &offset)
            if name == "CreateRoom" {

                // crate new room
                mapId := ReadUInt32(msg, &offset)
                s.maxRoomId++
                newRoom := &Room{s.maxRoomId, mapId, nil}
                newRoom.Init()

                // enter room
                player.roomId = newRoom.roomId
                newRoom.players = append(newRoom.players, player)
                s.rooms[newRoom.roomId] = newRoom

                // notify player enter room
                // fmt: EnterRoom|playerNum|playerId1|playerId2|...
                msg := make([]byte, 0, 32)
                msg = WriteString(msg, "EnterRoom")
                msg = WriteUInt32(msg, newRoom.roomId)
                msg = WriteUInt32(msg, newRoom.mapId)
                msg = WriteUInt32(msg, 1)
                msg = WriteUInt32(msg, player.playerId)
                player.SendMsg(msg)

                continue
            }

            if name == "JoinRoom" {
                roomId := ReadUInt32(msg, &offset)
                room, exist := s.rooms[roomId]
                if exist {
                    // player enter room
                    player.roomId = roomId
                    room.players = append(room.players, player)

                    // notify all player that a new menber come in
                    // fmt: EnterRoom|roomId|mapId|playerNum|playerId1|playerId2|...
                    msg := make([]byte, 0, 32)
                    msg = WriteString(msg, "EnterRoom")
                    msg = WriteUInt32(msg, room.roomId)
                    msg = WriteUInt32(msg, room.mapId)
                    msg = WriteUInt32(msg, uint32(len(room.players)))
                    for i := 0; i < len(room.players); i++ {
                        WriteUInt32(msg, room.players[i].playerId)
                    }
                    for i := 0; i < len(room.players); i++ {
                        room.players[i].SendMsg(msg)
                    }
                }
                continue
            }

            if name == "GetPlayersInfo" {
                responeMsg := make([]byte, 0, 32)

                // collect all request player infos
                requestPlayerNum := ReadUInt32(msg, &offset)
                var responePlayerNum uint32 = 0
                for i := 0; i < int(requestPlayerNum); i++ {
                    playerId := ReadUInt32(msg, &offset)
                    findPlayer, exist := l.players[playerId]
                    if exist {
                        WriteUInt32(responeMsg, findPlayer.playerId)
                        WriteUInt32(responeMsg, findPlayer.battleId)
                        WriteUInt32(responeMsg, findPlayer.roomId)
                        responePlayerNum++
                    }
                }

                // append head, and send
                responeMsgHead := make([]byte, 0, 32)
                responeMsgHead = WriteString(responeMsgHead, "ResponePlayersInfo")
                responeMsgHead = WriteUInt32(responeMsgHead, responePlayerNum)
                responeMsg = append(responeMsgHead, responeMsg...)
                player.SendMsg(responeMsg)

                continue
            }

            if name == "UploadMapHead" {

                player.mapExpectSize = ReadUInt32(msg, &offset)
                // a gaint file?? 10MB ?? not allow
                if player.mapExpectSize >= 1024 * 1024 * 10 {
                    player.mapExpectSize = 0
                    sendMsg := make([]byte, 0, 32)
                    sendMsg = WriteString(sendMsg, "UploadMapRespone")
                    sendMsg = WriteBool(sendMsg, false)
                    sendMsg = WriteString(sendMsg, "SizeTooBig")
                    player.SendMsg(sendMsg)
                    continue
                }

                // head, reset player all map caches
                player.mapExpectMd5 = ReadByteArray(msg, &offset)
                player.mapReceiveSize = 0
                player.mapReceiveMd5 = md5.New()
                player.mapPartDataCache = make([]byte, 0, player.mapExpectSize)

                // handle map part data
                mapPartData := ReadByteArray(msg, &offset)
                player.OnReceiveMapPartData(mapPartData)

                continue
            }

            if name == "UploadMapBody" {

                // maybe client not send UploadMapHead first, or size too big, refuse by UploadMapHead
                if player.mapExpectSize == 0 {
                    continue
                }

                // handle map part data
                mapPartData := ReadByteArray(msg, &offset)
                player.OnReceiveMapPartData(mapPartData)

                continue
            }

            if name == "DownloadMap" {

                mapId := ReadUInt32(msg, &offset)
                filePath := fmt.Sprintf("./maps/%d", mapId)
                go player.SendFileCoroutine(filePath, "DownloadMapRespone")
                continue
            }

            if name == "DownloadBattleReplay" {
                battleId := ReadUInt32(msg, &offset)
                filePath := fmt.Sprintf("./battleReplay/%d", battleId)
                go player.SendFileCoroutine(filePath, "DownloadBattleReplayRespone")
                continue
            }
        }
    }
}



////////////////////////////
// multi client can enter one room, chat or ready to start a battle, come from lobby
////////////////////////////
type Room struct {
    roomId uint32
    mapId uint32
    players []*Player // first player is the captain
}

func (r *Room) Init() {
    if r.players == nil {
        r.players = make([]*Player, 0, 4)
    }

}

func (r *Room) Tick(delta float32) {

    playerNum := len(r.players)
    s := GetServerInstance()
    for i := 0; i < playerNum; i++ {
        player := r.players[i]

        for {
            bNoNewMsg := false
            var msg []byte
            select {
                case msg = <-player.readMsgs:
                default:
                    bNoNewMsg = true
            }
            if bNoNewMsg {
                break
            }

            offset := 0
            name := ReadString(msg, &offset)

            // captain only msg
            if i == 0 {
                if name == "StartBattle" {
                    // creat new battle
                    s.maxBattleId++
                    newBattle := &Battle{s.maxBattleId, r.mapId, r.players, nil, nil, 0, 0}
                    newBattle.Init()

                    // dismiss room, players enter battle, and start a new battle
                    delete(s.rooms, r.roomId)
                    s.battles[newBattle.battleId] = newBattle
                    for j := 0; j < playerNum; j++ {
                        player := r.players[j]
                        player.roomId = 0
                        player.battleId = newBattle.battleId
                    }

                    // notify players enter a battle
                    // msg format: battleId|mapId|playerNum|playerid1|playerid2|...
                    msg := make([]byte, 0, 32)
                    msg = WriteString(msg, "EnterBattle")
                    msg = WriteUInt32(msg, newBattle.battleId)
                    msg = WriteUInt32(msg, r.mapId)
                    msg = WriteUInt32(msg, uint32(playerNum))
                    for j := 0; j < playerNum; j++ {
                        WriteUInt32(msg, r.players[j].playerId)
                    }
                    for j := 0; j < playerNum; j++ {
                        r.players[j].SendMsg(msg)
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
    replayData []byte
    currFrameData []byte
    currFrameIndex uint32
    tickCount uint32
}

func (b *Battle) Init() {

    playerNum := len(b.players)

    // record whole battle frame datas, reserve a big size
    b.replayData = make([]byte, 0, playerNum * 10240)

    // init curr farmedata, default 0
    b.currFrameIndex = 0
    b.currFrameData = make([]byte, playerNum)
    for i := 0; i < playerNum; i++ {
        b.currFrameData[i] = 0
    }
    b.tickCount = 0

    // update max battleId to db
    s := GetServerInstance()
    _, err := s.db.Exec("update Global set maxBattleId = ?", b.battleId)
    if err != nil {
        fmt.Println(err)
    }

    // insert a battle record to db
    // ugly code, but sql not support save a array...
    playTime := time.Now().Format("2006-01-02 15:04:05")
    var playerId0 uint32 = 0
    var playerId1 uint32 = 0
    var playerId2 uint32 = 0
    var playerId3 uint32 = 0
    if playerNum > 0 {
        playerId0 = b.players[0].playerId
    }
    if playerNum > 1 {
        playerId1 = b.players[1].playerId
    }
    if playerNum > 2 {
        playerId2 = b.players[2].playerId
    }
    if playerNum > 3 {
        playerId3 = b.players[3].playerId
    }
    _, err = s.db.Exec("insert into Battle values(?,?,?,?,?,?,?)", b.battleId, b.mapId, playTime, playerId0, playerId1, playerId2, playerId3)
    if err != nil {
        fmt.Println(err)
    }
}

func (b *Battle) Tick(delta float32) {

    // max battle time 30min, why client not upload EndBattle?? force stop it
    b.tickCount++
    if b.tickCount > 18000 {
        b.BattleEnd(false)
        return
    }

    playerNum := len(b.players)

    // pase client message
    bReceiveEnd := false
    bWin := false
    for i := 0; i < playerNum; i++ {
        player := b.players[i]

        for {
            bNoNewMsg := false
            var msg []byte
            select {
                case msg = <-player.readMsgs:
                default:
                    bNoNewMsg = true
            }
            if bNoNewMsg {
                break
            }

            offset := 0
            name := ReadString(msg, &offset)

            // collect all client frame data
            if name == "UploadFrameData" {
                // frameData can ref client 8 button press status
                frameData := ReadByte(msg, &offset)
                b.currFrameData[i] = frameData
                continue
            }
            if name == "EndBattle" {
                bReceiveEnd = true
                bWin = ReadBool(msg, &offset)
                continue
            }
        }
    }

    if bReceiveEnd {
        b.BattleEnd(bWin)
        return
    }

    // broadcast
    msg := make([]byte, 0, 32)
    msg = WriteString(msg, "ResponeFrameData")
    msg = WriteByteArray(msg, b.currFrameData)
    for i := 0; i < playerNum; i++ {
        b.players[i].SendMsg(msg)
    }

    // add to replay data
    b.replayData = append(b.replayData, b.currFrameData...)
    b.currFrameIndex++
}

func (b *Battle)BattleEnd(bWin bool) {

    // generate replayData 
    playerNum := len(b.players)
    replayDataHead := make([]byte, 0, 32)
    replayDataHead = WriteUInt32(replayDataHead, b.mapId)
    replayDataHead = WriteUInt32(replayDataHead, uint32(playerNum))
    for i := 0; i < playerNum; i++ {
        replayDataHead = WriteUInt32(replayDataHead, b.players[i].playerId)
    }
    replayDataHead = WriteBool(replayDataHead, bWin)
    replayDataHead = WriteUInt32(replayDataHead, b.currFrameIndex)
    b.replayData = append(replayDataHead, b.replayData...)

    // save replayData to file. then client can replay this battle
    // TODO: zip the file, they are huge continue frameData are same
    ioutil.WriteFile(fmt.Sprintf("./battleReplay/%d", b.battleId), b.replayData, 0666)

    // create a new room
    s := GetServerInstance()
    s.maxRoomId++
    newRoom := &Room{s.maxRoomId, b.mapId, b.players}
    newRoom.Init()

    // send player back to room, and dismiss battle
    s.rooms[newRoom.roomId] = newRoom
    delete(s.battles, b.battleId)
    for i := 0; i < len(newRoom.players); i++ {
        player := newRoom.players[i]
        player.battleId = 0
        player.roomId = newRoom.roomId
    }

    // motify players enter room
    // fmt: EnterRoom|roomId|mapId|playerNum|playerId1|playerId2|...
    msg := make([]byte, 0, 32)
    msg = WriteString(msg, "EnterRoom")
    msg = WriteUInt32(msg, newRoom.roomId)
    msg = WriteUInt32(msg, newRoom.mapId)
    msg = WriteUInt32(msg, uint32(playerNum))
    for i := 0; i < playerNum; i++ {
        msg = WriteUInt32(msg, newRoom.players[i].playerId)
    }
    for i := 0; i < playerNum; i++ {
        newRoom.players[i].SendMsg(msg)
    }
}

///////////////////////////
// sql table define
// operation helper:
// insert db.Exec("insert into tablename values(?,?)", v1, v2)
// delete db.Exec("delete from tablename where id = ")
// update db.Exec("update tablename set columnname = v where id = ")
// get db.Get(*TableDefine, "select * from tablename where id = ") 
// select db.Select([]TableDefine, "select * from tablename where id = ")
///////////////////////////
type GlobalTable struct {
    MaxMapId uint32 `db:"maxMapId"`
    MaxBattleId uint32 `db:"maxBattleId"`
}

type PlayerTable struct {
    PlayerId uint32 `db:"playerId"`
}

type MapTable struct {
    MapId uint32 `db:"mapId"`
    OwnerPlayerId uint32 `db:"ownerPlayerId"`
    Size uint32 `db:"size"`
    Md5 string `db:"md5"`
    LikeNum uint32 `db:"likeNum"`
}

type BattleTable struct {
    BattleId uint32 `db:"battleId"` 
    MapId uint32 `db:"mapId"`
    PlayTime string `db:"playTime"`
    PlayerId1 uint32 `db:"playerId1"`
    PlayerId2 uint32 `db:"playerId2"`
    PlayerId3 uint32 `db:"playerId3"`
    PlayerId4 uint32 `db:"playerId4"`
}

////////////////////////////
// Message parse
////////////////////////////
func ReadByteArray(data []byte, offset *int) []byte {

    // read a string length, 2 bytes
    var length uint16 = 0
    length |= uint16(data[*offset + 1])
    length <<= 8
    length |= uint16(data[*offset])
    *offset += 2
    fmt.Println(length)

    // read string
    result := data[*offset : *offset+int(length)]
    *offset += int(length)
    return result
}

func WriteByteArray(data []byte, byteArray []byte) []byte {

    // write string length, 2bytes
    var length uint16 = uint16(len(byteArray))
    data = append(data, byte(length), byte(length >> 8))

    // write string
    return append(data, byteArray...)
}

func ReadString(data []byte, offset *int) string {
    return string(ReadByteArray(data, offset))
}

func WriteString(data []byte, str string) []byte {
    return WriteByteArray(data, []byte(str))
}

func ReadFloat64(data []byte, offset *int) float64 {
    // use IEEE 754, 8 byte
    var bits uint64 = 0
    for i := 7; i >= 0; i-- {
        bits |= uint64(data[*offset + i])
        if i > 0 {
            bits <<= 8
        }
    }
    *offset += 8
    return math.Float64frombits(bits)
}

func WriteFloat64(data []byte, flt float64) []byte {
    var bits uint64 = math.Float64bits(flt)

    for i := 0; i < 8; i++ {
        data = append(data, byte(bits))
        bits >>= 8
    }
    return data
}

func ReadUInt32(data []byte, offset *int) uint32 {
    fmt.Println(*offset)
    // 4 byte
    var value uint32 = 0
    for i := 3; i >= 0; i-- {
        value |= uint32(data[*offset + i])
        if i > 0 {
            value <<= 8
        }
    }
    *offset += 4
    return value
}

func WriteUInt32(data []byte, intValue uint32) []byte {
    for i := 0; i < 4; i++ {
        data = append(data, byte(intValue))
        intValue >>= 8
    }
    return data
}

func ReadByte(data []byte, offset *int) byte {
    value := data[*offset]
    *offset += 1
    return value
}

func WriteByte(data []byte, intValue byte) []byte {
    return append(data, intValue)
}

func ReadBool(data []byte, offset *int) bool {
    // 1 byte
    var result bool = data[*offset] == 1
    *offset += 1
    return result
}

func WriteBool(data []byte, bValue bool) []byte {
    if (bValue) {
        return append(data, byte(1))
    }
    return append(data, byte(0))
}
