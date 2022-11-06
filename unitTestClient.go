

package main

import (
    "math"
    "bufio"
    "os"
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
            fmt.Println("name:", name)
            if name == "LoginRespone" {
                success := ReadBool(msg, &offset)
                fmt.Println("args:%t", success)
                continue
            }
        }
    }()

    // read cmommand line input
    for {
        reader := bufio.NewReader(os.Stdin)
        input, err := reader.ReadString('\n')
        if err != nil {
            fmt.Println("An error occured while reading input. Please try again", err)
            return
        }

        if input == "Login\n" {
            msg := make([]byte, 0, 32)
            msg = WriteString(msg, "Login")
            msg = WriteUInt32(msg, uint32(11111))
            msg = WriteString(msg, "jasonszheng")
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
