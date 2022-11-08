

package main

import (
    "strings"
    "strconv"
    "math"
    "bufio"
    "os"
    "crypto/md5"
    "fmt"
    "flag"
    "net/url"
    "github.com/gorilla/websocket"
)

func main() {

    // dial
    addr := flag.String("addr", "127.0.0.1:30000", "http server address")
    u := url.URL{Scheme:"ws", Host:*addr, Path:"/"}
    conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
    if err != nil {
        fmt.Println(err)
        return
    }
    fmt.Println("connect success")

    // read msg
    go func() {
        
        for {
            _ , msg, err := conn.ReadMessage()
            if err != nil {
                fmt.Println(err)
                return
            }

            offset := 0
            name := ReadString(msg, &offset)

            if name == "LoginRespone" {
                success := ReadBool(msg, &offset)
                fmt.Println(name, success)
                continue
            }

            if name == "UploadMapRespone" {
                success := ReadBool(msg, &offset)
                failReason := ""
                if !success {
                    failReason = ReadString(msg, &offset)
                }
                fmt.Println(name, success, failReason)
                continue
            }

            if name == "UpdateRoomState" {
                roomId := ReadUInt32(msg, &offset)
                mapId := ReadUInt32(msg, &offset)
                playerNum := ReadUInt32(msg, &offset)
                playerIds := make([]uint32, 0)
                for i := 0; i < int(playerNum); i++ {
                    playerIds = append(playerIds, ReadUInt32(msg, &offset))
                }
                fmt.Println(name, roomId, mapId, playerNum, playerIds)
                continue
            }

            if name == "JoinRoomRespone" {
                success := ReadBool(msg, &offset)
                failReason := ""
                if !success {
                    failReason = ReadString(msg, &offset)
                }
                fmt.Println(name, success, failReason)
                continue
            }

            if name == "EnterBattle" {
                battleId := ReadUInt32(msg, &offset)
                roomId := ReadUInt32(msg, &offset)
                playerNum := ReadUInt32(msg, &offset)
                playerIds := make([]uint32, 0)
                for i := 0; i < int(playerNum); i++ {
                    playerIds = append(playerIds, ReadUInt32(msg, &offset))
                }
                fmt.Println(name, battleId, roomId, playerNum, playerIds)
                continue
            }

            if name == "BroadcastFrameData" {
                frameData := ReadByteArray(msg, &offset)
                fmt.Println(name, frameData)
                continue
            }

            if name == "BattleEnd" {
                bWin := ReadBool(msg, &offset)
                fmt.Println(name, bWin)
                continue
            }
        }
    }()

    // read cmommand line input, simulate send msg
    for {
        reader := bufio.NewReader(os.Stdin)
        input, err := reader.ReadString('\n')
        if err != nil {
            fmt.Println("An error occured while reading input. Please try again", err)
            return
        }

        input = input[:len(input)-1]
        inputs := strings.Split(input, " ")
        if len(inputs) == 0 {
            inputs = append(inputs, input)
        }


        if inputs[0] == "Login" {
            msg := make([]byte, 0, 32)
            msg = WriteString(msg, "Login")
            playerId, _ := strconv.Atoi(inputs[1])
            msg = WriteUInt32(msg, uint32(playerId))
            msg = WriteString(msg, inputs[2])
            err := conn.WriteMessage(websocket.TextMessage, msg)
            if err != nil {
                fmt.Println(err)
            }
            continue
        }

        if inputs[0] == "UploadMapHead" {
            fileData := []byte("aaaabbbbccccdddd")
            fileMd5Array := md5.Sum(fileData)
            fileMd5 := make([]byte, 16)
            copy(fileMd5, fileMd5Array[:])

            msg := make([]byte, 0, 32)
            msg = WriteString(msg, "UploadMapHead")
            msg = WriteUInt32(msg, uint32(len(fileData)))
            msg = WriteByteArray(msg, fileMd5)
            msg = WriteByteArray(msg, fileData)

            err := conn.WriteMessage(websocket.TextMessage, msg)
            if err != nil {
                fmt.Println(err)
            }
            continue
        }

        if inputs[0] == "CreateRoom" {
            msg := make([]byte, 0, 32)
            msg = WriteString(msg, "CreateRoom")
            msg = WriteUInt32(msg, uint32(6))

            err := conn.WriteMessage(websocket.TextMessage, msg)
            if err != nil {
                fmt.Println(err)
            }
            continue
        }

        if inputs[0] == "JoinRoom" {

            msg := make([]byte, 0, 32)
            msg = WriteString(msg, inputs[0])
            roomId, _ := strconv.Atoi(inputs[1])
            msg = WriteUInt32(msg, uint32(roomId))

            err := conn.WriteMessage(websocket.TextMessage, msg)
            if err != nil {
                fmt.Println(err)
            }
            continue
        }

        if inputs[0] == "ExistRoom" {
            msg := make([]byte, 0, 32)
            msg = WriteString(msg, inputs[0])

            err := conn.WriteMessage(websocket.TextMessage, msg)
            if err != nil {
                fmt.Println(err)
            }
            continue
        }

        if inputs[0] == "StartBattle" {
            msg := make([]byte, 0, 32)
            msg = WriteString(msg, inputs[0])

            err := conn.WriteMessage(websocket.TextMessage, msg)
            if err != nil {
                fmt.Println(err)
            }
            continue
        }
    }
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
