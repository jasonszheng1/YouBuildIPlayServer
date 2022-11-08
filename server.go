// author:jasonszheng
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
    defer func() {
        if s.db != nil {
            s.db.Close()
            fmt.Println("mysql close")
        }
    }()

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

    fmt.Println("come a new connection", conn.RemoteAddr())
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

        // remove the player
        if player.conn == nil {
            s.playersNoLogin = append(s.playersNoLogin[:i], s.playersNoLogin[i+1:]...)
            i--
            fmt.Println("remove player from notlogin")
            continue
        }

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

                // remove from server, and put to lobby
                s.playersNoLogin = append(s.playersNoLogin[:i], s.playersNoLogin[i+1:]...)
                i--

                playerId := ReadUInt32(msg, &offset)
                playerName := ReadString(msg, &offset)

                // check the player is already exist in lobby/battle ? replace player with exist player
                existPlayer, exist := s.lobby.players[playerId]
                if exist {
                    existPlayer.conn = player.conn
                    player = existPlayer
                }

                player.Login(playerId, playerName)

                // respone
                responeMsg := make([]byte, 0, 32)
                responeMsg = WriteString(responeMsg, "LoginRespone")
                responeMsg = WriteBool(responeMsg, true)
                player.SendMsg(responeMsg)
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

    // start read goroutine
    p.readMsgs = make(chan []byte, 32)
    go p.ReadMsgCoroutine()

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

    defer func() {
        p.conn.Close()
        p.conn = nil
        fmt.Println(p.playerId, "connection close")
    }()

    for {
        _ , msg, err := p.conn.ReadMessage()
        if err != nil {
            fmt.Println(p.playerId, "Logout")
            return
        }
        p.readMsgs <- msg
    }
}

func (p *Player) SendMsg(msg []byte) {
    if p.conn == nil {
        return
    }
    p.sendMutex.Lock()
    p.conn.WriteMessage(websocket.TextMessage, msg)
    p.sendMutex.Unlock()
}

func (p *Player) Login(inPlayerId uint32, inPlayerName string) {

    fmt.Println(inPlayerId, "login")
    s := GetServerInstance()

    // reconnect to the battle
    if p.battleId > 0 {
        battle, exist := s.battles[p.battleId]
        if exist {
            battle.OnPlayerReconnectToBattle(p)
            return
        }
    }

    p.playerId = inPlayerId
    p.playerName = inPlayerName
    s.lobby.players[p.playerId] = p
    fmt.Println(inPlayerId, "move to lobby")

    var count int
    err := s.db.Get(&count, "select count(playerId) from Player where playerId=?", p.playerId) 
    if err != nil {
        fmt.Println(err)
    }

    // not exist, insert a new record
    if count == 0 {
        fmt.Println(inPlayerId, "not exist in Player db, insert a new record")
        _, err1 := s.db.Exec("insert into Player values(?,?)", inPlayerId, inPlayerName)
        if err1 != nil {
            fmt.Println(err1)
        }
    }
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
            err := ioutil.WriteFile(fmt.Sprintf("./maps/%d", newMapId), p.mapPartDataCache, 0666)
            if err != nil {
                fmt.Println(err)
            }

            // insert a record to db
            _, err1 := s.db.Exec("insert into Map values(?,?,?,?)", newMapId, p.playerId, p.mapReceiveSize, 0)
            if err1 != nil {
                fmt.Println(err1)
            }
            fmt.Println(p.playerId, "server receive a new map", newMapId)

            // update max mapId to db
            _, err2 := s.db.Exec("update Global set maxMapId = ?", newMapId)
            if err2 != nil {
                fmt.Println(err2)
            }
        }

        // receive end, reset to refuse next body upload
        p.mapExpectSize = 0
    }
}

// file size too big will cause server lag, so create a thread to do this
func (p *Player) SendFileCoroutine(filePath string, msgName string) {

    // file not exist?
    // TODO: if muti thread read the same file, is safe??
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

        // player disconnect, stop send
        if p.conn == nil {
            return
        }

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

        // message handle by battle/room
        if player.battleId != 0 || player.roomId != 0 {
            continue
        }

        // handle player disconnect
        if player.conn == nil {
            delete(l.players, player.playerId)
            fmt.Println(player.playerId, "remove from lobby")
            continue
        }

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

                // check mapId is exist
                mapId := ReadUInt32(msg, &offset)
                count := 0
                err := s.db.Get(&count, "select count(mapId) from Map where mapId=?", mapId)
                if err != nil {
                    fmt.Println(err)
                }
                if count == 0 {
                    responeMsg := make([]byte, 0, 32)
                    responeMsg = WriteString(responeMsg, "CreateRoomRespone")
                    responeMsg = WriteBool(responeMsg, false)
                    responeMsg = WriteString(responeMsg, fmt.Sprintf("Request CreateRoom, but param mapId:%d not exist", mapId))
                    player.SendMsg(responeMsg)
                    continue
                }

                // crate new room
                s.maxRoomId++
                newRoom := &Room{s.maxRoomId, mapId, nil}
                newRoom.Init()
                s.rooms[newRoom.roomId] = newRoom

                // player enter room
                player.roomId = newRoom.roomId
                newRoom.players = append(newRoom.players, player)
                fmt.Println(player.playerId, "move to room", newRoom.roomId)

                // notify player enter room
                newRoom.NotifyAllPlayersRoomState()

                continue
            }

            if name == "JoinRoom" {
                roomId := ReadUInt32(msg, &offset)
                room, exist := s.rooms[roomId]

                // not exist
                if !exist {
                    responeMsg := make([]byte, 0, 32)
                    responeMsg = WriteString(responeMsg, "JoinRoomRespone")
                    responeMsg = WriteBool(responeMsg, false)
                    responeMsg = WriteString(responeMsg, fmt.Sprintf("Request roomId:%d not exist", roomId))
                    player.SendMsg(responeMsg)
                    continue
                }

                // player enter room
                player.roomId = roomId
                room.players = append(room.players, player)
                fmt.Println(player.playerId, "move to room", roomId)

                // notify all player that a new menber come in
                room.NotifyAllPlayersRoomState()
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
                responeMsgHead = WriteString(responeMsgHead, "GetPlayersInfoRespone")
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
                filePath := fmt.Sprintf("./battles/%d", battleId)
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

func (r *Room) NotifyAllPlayersRoomState() {

    // fmt: EnterRoom|roomId|mapId|playerNum|playerId1|playerId2|...
    msg := make([]byte, 0, 32)
    msg = WriteString(msg, "UpdateRoomState")
    msg = WriteUInt32(msg, r.roomId)
    msg = WriteUInt32(msg, r.mapId)
    msg = WriteUInt32(msg, uint32(len(r.players)))
    for i := 0; i < len(r.players); i++ {
        msg = WriteUInt32(msg, r.players[i].playerId)
    }
    for i := 0; i < len(r.players); i++ {
        r.players[i].SendMsg(msg)
    }
}

func (r *Room) Tick(delta float32) {

    s := GetServerInstance()
    for i := 0; i < len(r.players); i++ {
        player := r.players[i]

        // handle disconnect
        if player.conn == nil {

            // player remove from room and lobby
            r.players = append(r.players[:i], r.players[i+1:]...)
            delete(s.lobby.players, player.playerId)
            i--

            // dismiss room if empty
            if len(r.players) == 0 {
                delete(s.rooms, r.roomId)
                return
            } else {
                r.NotifyAllPlayersRoomState()
            }
            continue
        }

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
                    for j := 0; j < len(r.players); j++ {
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
                    msg = WriteUInt32(msg, uint32(len(r.players)))
                    for j := 0; j < len(r.players); j++ {
                        msg = WriteUInt32(msg, r.players[j].playerId)
                    }
                    for j := 0; j < len(r.players); j++ {
                        r.players[j].SendMsg(msg)
                        fmt.Println(r.players[j].playerId, "move to battle", newBattle.battleId)
                    }

                    return
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

            if name == "ExistRoom" {
                // player remove from room
                r.players = append(r.players[:i], r.players[i+1:]...)
                i--

                // respone
                responeMsg := make([]byte, 0, 32)
                responeMsg = WriteString(responeMsg, "ExistRoomRespone")
                player.SendMsg(responeMsg)

                player.roomId = 0
                fmt.Println(player.playerId, "move to lobby")

                // dismiss room
                if len(r.players) == 0 {
                    delete(s.rooms, r.roomId)
                    return
                } else {
                    r.NotifyAllPlayersRoomState()
                }
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

    fmt.Println("BattleStart", b.battleId)
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

    // max battle time 18000tick = 30min, why client not upload EndBattle?? force stop it
    b.tickCount++
    if b.tickCount > 100 {
        b.BattleEnd(false)
        return
    }

    playerNum := len(b.players)

    // all players are disconnect? should i end the battle immediatlys?? or maybe allow player to reconnect, but battle will keep 30min if player nerver come back
    bAllPlayersDisconnect := true
    for i := 0; i < playerNum; i++ {
        if b.players[i].conn != nil {
            bAllPlayersDisconnect = false
            break
        }
    }
    if bAllPlayersDisconnect {
        b.BattleEnd(false)
        return
    }

    // client message
    bReceiveEnd := false
    bWin := false
    for i := 0; i < playerNum; i++ {
        player := b.players[i]

        // player disconnect, but we should not remove it from the battle
        // first reason is to keep relayData format valid
        // second reason is we support player reconnect to this battle
        if player.conn == nil {
            // reset frameData to zero, like reset a pressing button
            b.currFrameData[i] = 0
            continue
        }

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
    msg = WriteString(msg, "BroadcastFrameData")
    msg = WriteByteArray(msg, b.currFrameData)
    for i := 0; i < playerNum; i++ {
        b.players[i].SendMsg(msg)
    }

    // add to replay data
    b.replayData = append(b.replayData, b.currFrameData...)
    b.currFrameIndex++
}

func (b *Battle)BattleEnd(bWin bool) {
    fmt.Println("BattleEnd", b.battleId, bWin)
    s := GetServerInstance()

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
    ioutil.WriteFile(fmt.Sprintf("./battles/%d", b.battleId), b.replayData, 0666)

    // dismiss battle
    delete(s.battles, b.battleId)

    // check exist online player num
    onlinePlayers := make([]*Player, 0)
    for i := 0; i < playerNum; i++ {
        if b.players[i].conn == nil {
            // player still exist in lobby, remove it here
            delete(s.lobby.players, b.players[i].playerId)
            fmt.Println(b.players[i].playerId, "offline until battle end, remove from lobby")
        } else {
            onlinePlayers = append(onlinePlayers, b.players[i])
        }
    }
    if len(onlinePlayers) == 0 {
        return
    }

    // send battle result
    for i := 0; i < len(onlinePlayers); i++ {
        responeMsg := make([]byte, 0, 32)
        responeMsg = WriteString(responeMsg, "BattleEnd")
        responeMsg = WriteBool(responeMsg, bWin)
        onlinePlayers[i].SendMsg(responeMsg)
    }

    // create a new room
    s.maxRoomId++
    newRoom := &Room{s.maxRoomId, b.mapId, onlinePlayers}
    newRoom.Init()

    // send players back to room
    s.rooms[newRoom.roomId] = newRoom
    for i := 0; i < len(newRoom.players); i++ {
        player := newRoom.players[i]
        player.battleId = 0
        player.roomId = newRoom.roomId
        fmt.Println(player.playerId, "move to room", newRoom.roomId)
    }
    newRoom.NotifyAllPlayersRoomState()
}

func (b *Battle)OnPlayerReconnectToBattle(p *Player) {

    // send enter battle
    msg := make([]byte, 0, 32)
    msg = WriteString(msg, "EnterBattle")
    msg = WriteUInt32(msg, b.battleId)
    msg = WriteUInt32(msg, b.mapId)
    msg = WriteUInt32(msg, uint32(len(b.players)))
    for j := 0; j < len(b.players); j++ {
        msg = WriteUInt32(msg, b.players[j].playerId)
    }
    p.SendMsg(msg)
    fmt.Println(p.playerId, "reconnect to battle", b.battleId)

    // send relay data, break to many parts, to limit each package not exceed 1024 byte
    totalSize := len(b.replayData)
    sendSize := 0 // the size already send
    bHead := true
    for {
        msg := make([]byte, 0, 1024)
        if bHead {
            msg = WriteString(msg, "ReconnectFrameDatasHead")
            msg = WriteUInt32(msg, uint32(totalSize))
        } else {
            msg = WriteString(msg, "ReconnectFrameDatasBody")
        }

        partSendSize := 1024 - len(msg) - 2

        bEnd := false
        if sendSize + partSendSize > totalSize {
            partSendSize = totalSize - sendSize
            bEnd = true
        }

        msg = WriteByteArray(msg, b.replayData[sendSize : sendSize + partSendSize])
        p.SendMsg(msg)
        sendSize += partSendSize

        if bEnd {
            break
        }
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
    PlayerName string `db:"playerName"`
}

type MapTable struct {
    MapId uint32 `db:"mapId"`
    OwnerPlayerId uint32 `db:"ownerPlayerId"`
    Size uint32 `db:"size"`
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
