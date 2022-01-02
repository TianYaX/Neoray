package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"
)

const (
	DEFAULT_ADDRESS     = "localhost:17717"
	DEFAULT_TIMEOUT     = time.Second / 2
	DEFAULT_BUFFER_SIZE = 1024
)

type IpcMessageType int

type IpcFuncCall struct {
	MsgType    IpcMessageType
	MacAddress uint64
	Args       []interface{}
}

const (
	IPC_MSG_TYPE_OK IpcMessageType = iota
	IPC_MSG_TYPE_CLOSE_CONN
	IPC_MSG_TYPE_OPEN_FILE
	IPC_MSG_TYPE_GOTO_LINE
	IPC_MSG_TYPE_GOTO_COLUMN
)

func (msgType IpcMessageType) String() string {
	switch msgType {
	case IPC_MSG_TYPE_OK:
		return "OK"
	case IPC_MSG_TYPE_CLOSE_CONN:
		return "CLOSE"
	case IPC_MSG_TYPE_OPEN_FILE:
		return "OPEN_FILE"
	case IPC_MSG_TYPE_GOTO_LINE:
		return "GOTO_LINE"
	case IPC_MSG_TYPE_GOTO_COLUMN:
		return "GOTO_COLUMN"
	default:
		panic("Invalid message type.")
	}
}

func getMacAddress() uint64 {
	interfaces, err := net.Interfaces()
	if err != nil {
		return 0
	}
	for _, i := range interfaces {
		if i.Flags&net.FlagUp != 0 && bytes.Compare(i.HardwareAddr, nil) != 0 {
			// Skip locally administered addresses
			if i.HardwareAddr[0]&2 == 2 {
				continue
			}
			var mac uint64
			for j, b := range i.HardwareAddr {
				if j >= 8 {
					break
				}
				mac <<= 8
				mac += uint64(b)
			}
			return mac
		}
	}
	return 0
}

type IpcClient struct {
	conn net.Conn
	mac  uint64
}

func CreateClient() (*IpcClient, error) {
	defer measure_execution_time()()
	// NOTE: Timeout parameter may not be enough for tcp connection, but speeds up startup
	conn, err := net.DialTimeout("tcp", DEFAULT_ADDRESS, DEFAULT_TIMEOUT)
	if err != nil {
		return nil, err
	}
	client := IpcClient{
		conn: conn,
		mac:  getMacAddress(),
	}
	return &client, nil
}

func (client *IpcClient) Call(msgType IpcMessageType, args ...interface{}) bool {
	defer measure_execution_time()()
	logMessage(LEVEL_DEBUG, TYPE_NETWORK, "Sending signal:", msgType)
	// Encode function
	jsonData, err := json.Marshal(IpcFuncCall{
		MsgType:    msgType,
		MacAddress: client.mac,
		Args:       args,
	})
	if err != nil {
		logMessage(LEVEL_WARN, TYPE_NETWORK, "Failed to encode function call:", err)
		return false
	}
	_, err = client.conn.Write(jsonData)
	if err != nil {
		logMessage(LEVEL_WARN, TYPE_NETWORK, "Failed to send signal:", err)
		return false
	}
	// Read response from server
	resp := make([]byte, DEFAULT_BUFFER_SIZE)
	n, err := client.conn.Read(resp)
	if err != nil {
		logMessage(LEVEL_WARN, TYPE_NETWORK, "Failed to read response:", err)
		return false
	}
	resp = resp[:n]
	// Decode response
	var funcCall IpcFuncCall
	err = json.Unmarshal(resp, &funcCall)
	if err != nil {
		logMessage(LEVEL_WARN, TYPE_NETWORK, "Failed to decode response:", err)
		return false
	}
	// Check mac address
	// NOTE: Actually we don't need to check for mac address in client because
	// client already sent command to execute but anyway, it seems more secure
	if funcCall.MacAddress != client.mac {
		logMessage(LEVEL_WARN, TYPE_NETWORK, "Signal rejected: Connected server is not running on same machine.")
		return false
	}
	// First client sends close call to server, if server accepts, it resends
	// close call to client and closes its connection. After server closes, client
	// receives a close call and closes itself.
	if funcCall.MsgType == IPC_MSG_TYPE_CLOSE_CONN {
		logMessage(LEVEL_TRACE, TYPE_NETWORK, "Disconnected from server.")
		client.conn.Close()
		return true
	} else if funcCall.MsgType != IPC_MSG_TYPE_OK {
		// Server always has to send OK. if we are not receive any ok this means there is a
		// problem in connection
		logMessage(LEVEL_TRACE, TYPE_NETWORK, "Client sent non OK response:", funcCall.MsgType)
		return false
	}
	return true
}

func (client *IpcClient) Close() {
	client.Call(IPC_MSG_TYPE_CLOSE_CONN)
	logMessage(LEVEL_TRACE, TYPE_NETWORK, "Client closed.")
}

// Server is a listener, not sends messages but processes incoming messages from clients
type IpcServer struct {
	listener       net.Listener
	mac            uint64
	callsAvailable AtomicBool
	callsMutex     sync.Mutex
	calls          []IpcFuncCall
}

// Create a server and process incoming signals.
func CreateServer() (*IpcServer, error) {
	defer measure_execution_time()()
	listener, err := net.Listen("tcp", DEFAULT_ADDRESS)
	if err != nil {
		return nil, err
	}
	server := IpcServer{
		listener: listener,
		mac:      getMacAddress(),
	}
	go server.mainLoop()
	return &server, nil
}

func (server *IpcServer) mainLoop() {
	// Encode ok message because we always use it
	encodedOK, err := json.Marshal(IpcFuncCall{MsgType: IPC_MSG_TYPE_OK, MacAddress: server.mac})
	if err != nil {
		logMessage(LEVEL_ERROR, TYPE_NETWORK, "Failed to encode OK:", err)
		return
	}
	// Encode CLOSE message because we always use it
	encodedCLOSE, err := json.Marshal(IpcFuncCall{MsgType: IPC_MSG_TYPE_CLOSE_CONN, MacAddress: server.mac})
	if err != nil {
		logMessage(LEVEL_ERROR, TYPE_NETWORK, "Failed to encode CLOSE:", err)
		return
	}
	for {
		conn, err := server.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				logMessage(LEVEL_TRACE, TYPE_NETWORK, "Server closed.")
			} else {
				logMessage(LEVEL_ERROR, TYPE_NETWORK, "Server closed because of errors:", err)
			}
			return
		}
		logMessage(LEVEL_TRACE, TYPE_NETWORK, "New client connected:", conn.RemoteAddr())
		// handle connection concurrently
		go func() {
			defer conn.Close()
			for {
				data := make([]byte, DEFAULT_BUFFER_SIZE)
				n, err := conn.Read(data)
				if err != nil {
					logMessage(LEVEL_WARN, TYPE_NETWORK, "Failed to read client data:", err)
					continue
				}
				data = data[:n]
				// decode data
				var funcCall IpcFuncCall
				err = json.Unmarshal(data, &funcCall)
				if err != nil {
					logMessage(LEVEL_WARN, TYPE_NETWORK, "Failed to decode client data:", err)
					continue
				}
				// check mac address
				if funcCall.MacAddress != server.mac {
					logMessage(LEVEL_WARN, TYPE_NETWORK, "Signal Rejected: Connected client is not running on same machine.")
					break
				}
				switch funcCall.MsgType {
				case IPC_MSG_TYPE_CLOSE_CONN:
					logMessage(LEVEL_TRACE, TYPE_NETWORK, "Client", conn.RemoteAddr(), "disconnected.")
					_, err = conn.Write(encodedCLOSE)
					if err != nil {
						logMessage(LEVEL_WARN, TYPE_NETWORK, "Failed to send response to client.")
						break
					}
					return
				default:
					server.appendNewCall(funcCall)
					_, err = conn.Write(encodedOK)
					if err != nil {
						logMessage(LEVEL_WARN, TYPE_NETWORK, "Failed to send response to client.")
					}
					break
				}
			}
		}()
	}
}

func (server *IpcServer) appendNewCall(call IpcFuncCall) {
	server.callsMutex.Lock()
	defer server.callsMutex.Unlock()
	server.calls = append(server.calls, call)
	server.callsAvailable.Set(true)
}

func (server *IpcServer) update() {
	if server.callsAvailable.Get() {
		server.callsMutex.Lock()
		defer server.callsMutex.Unlock()
		for _, call := range server.calls {
			// bool, for JSON booleans
			// float64, for JSON numbers
			// string, for JSON strings
			// []interface{}, for JSON arrays
			// map[string]interface{}, for JSON objects
			// nil for JSON null
			switch call.MsgType {
			case IPC_MSG_TYPE_OPEN_FILE:
				path := call.Args[0].(string)
				singleton.nvim.openFile(path)
				break
			case IPC_MSG_TYPE_GOTO_LINE:
				line := int(call.Args[0].(float64))
				singleton.nvim.gotoLine(line)
				break
			case IPC_MSG_TYPE_GOTO_COLUMN:
				column := int(call.Args[0].(float64))
				singleton.nvim.gotoColumn(column)
				break
			default:
				logMessage(LEVEL_WARN, TYPE_NETWORK, "Server received invalid signal:", call)
				break
			}
		}
		server.calls = server.calls[0:0]
		server.callsAvailable.Set(false)
		singleton.window.raise()
	}
}

func (server *IpcServer) Close() {
	server.listener.Close()
}
