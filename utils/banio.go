package utils

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/banbox/banexg/utils"
	"github.com/sasha-s/go-deadlock"
	"go.uber.org/zap"
	"io"
	"math/rand"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

type ConnCB = func(string, []byte)

type IBanConn interface {
	WriteMsg(msg *IOMsg) *errs.Error
	Write(data []byte, locked bool) *errs.Error
	ReadMsg() (*IOMsgRaw, *errs.Error)
	Subscribe(tags ...string)
	UnSubscribe(tags ...string)
	GetRemote() string
	IsClosed() bool
	HasTag(tag string) bool
	RunForever() *errs.Error
}

type BanConn struct {
	Conn        net.Conn          // Original socket connection 原始的socket连接
	Tags        map[string]bool   // Message subscription list 消息订阅列表
	Remote      string            // Remote Name 远端名称
	Listens     map[string]ConnCB // Message processing function 消息处理函数
	RefreshMS   int64             // Connection ready timestamp 连接就绪的时间戳
	Ready       bool
	IsReading   bool
	lockConnect deadlock.Mutex
	lockWrite   deadlock.Mutex
	lockTag     deadlock.Mutex
	heartBeatMs int64               // Timestamp of the latest received ping/pong
	DoConnect   func(conn *BanConn) // Reconnect function, no attempt to reconnect provided 重新连接函数，未提供不尝试重新连接
	ReInitConn  func()              // Initialize callback function after successful reconnection 重新连接成功后初始化回调函数
	IsServer    bool                // Flag to indicate if this is a server connection
	CloseFlag   bool                // Flag to indicate connection should be closed
	OnClose     func()              // Callback when connection is closed
}

type IOMsg struct {
	Action string      `json:"action"`
	Data   interface{} `json:"data"`
}

type IOMsgRaw struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}

var (
	tipRetryTimes     = make(map[string]int64)
	tipRetryTimesLock deadlock.Mutex
)

func (c *BanConn) GetRemote() string {
	return c.Remote
}
func (c *BanConn) IsClosed() bool {
	return c.Conn == nil || !c.Ready || c.CloseFlag
}
func (c *BanConn) HasTag(tag string) bool {
	c.lockTag.Lock()
	_, ok := c.Tags[tag]
	c.lockTag.Unlock()
	return ok
}

func (c *BanConn) WriteMsg(msg *IOMsg) *errs.Error {
	if c.Conn == nil || c.CloseFlag {
		return errs.NewMsg(errs.CodeIOWriteFail, "write fail as disconnected")
	}
	raw, err_ := utils.Marshal(*msg)
	if err_ != nil {
		return errs.New(core.ErrMarshalFail, err_)
	}
	compressed, err := compress(raw)
	if err != nil {
		return err
	}
	return c.Write(compressed, false)
}

func (c *BanConn) Write(data []byte, locked bool) *errs.Error {
	if c.Conn == nil || c.CloseFlag {
		return errs.NewMsg(errs.CodeIOWriteFail, "write fail as disconnected")
	}
	if !locked {
		c.lockWrite.Lock()
		defer c.lockWrite.Unlock()
	}
	dataLen := uint32(len(data))
	lenBt := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBt, dataLen)
	if c.Conn != nil {
		_, err_ := c.Conn.Write(lenBt)
		if err_ != nil {
			c.Ready = false
			errCode, errType := getErrType(err_)
			if c.DoConnect != nil && errCode == core.ErrNetConnect && !c.IsServer {
				log.Warn("write fail, wait 3s and retry", zap.String("type", errType))
				c.connect()
				return c.Write(data, true)
			}
			if c.IsServer {
				c.CloseFlag = true
			}
			return errs.New(errCode, err_)
		}
		if c.Conn != nil {
			_, err_ = c.Conn.Write(data)
			if err_ != nil {
				c.Ready = false
				errCode, _ := getErrType(err_)
				if c.IsServer {
					c.CloseFlag = true
				}
				return errs.New(errCode, err_)
			}
			return nil
		}
	}
	return errs.NewMsg(errs.CodeIOWriteFail, "write fail as disconnected")
}

func (c *BanConn) ReadMsg() (*IOMsgRaw, *errs.Error) {
	compressed, err := c.Read()
	if err != nil {
		return nil, err
	}
	data, err := deCompress(compressed)
	if err != nil {
		return nil, err
	}
	var msg IOMsgRaw
	err_ := utils.Unmarshal(data, &msg, utils.JsonNumDefault)
	if err_ != nil {
		return nil, errs.New(errs.CodeUnmarshalFail, err_)
	}
	return &msg, nil
}

func (c *BanConn) Read() ([]byte, *errs.Error) {
	if c.Conn == nil {
		return nil, errs.NewMsg(core.ErrRunTime, "BanConn Read nil, connection already closed")
	}
	lenBuf := make([]byte, 4)
	_, err_ := c.Conn.Read(lenBuf)
	if err_ != nil {
		errCode, errType := getErrType(err_)
		if c.DoConnect != nil && errCode == core.ErrNetConnect {
			log.Warn("read fail, wait 3s and retry", zap.String("type", errType))
			c.connect()
			return c.Read()
		}
		return nil, errs.New(errCode, err_)
	}
	dataLen := binary.LittleEndian.Uint32(lenBuf)
	buf := make([]byte, dataLen)
	_, err_ = c.Conn.Read(buf)
	if err_ != nil {
		return nil, errs.New(core.ErrNetReadFail, err_)
	}
	return buf, nil
}

func (c *BanConn) Subscribe(tags ...string) {
	c.lockTag.Lock()
	for _, tag := range tags {
		c.Tags[tag] = true
	}
	c.lockTag.Unlock()
}
func (c *BanConn) UnSubscribe(tags ...string) {
	c.lockTag.Lock()
	for _, tag := range tags {
		delete(c.Tags, tag)
	}
	c.lockTag.Unlock()
}

/*
RunForever
Monitor the information sent by the connection and process it.
According to the action of the message:
Call the corresponding member function for processing; Directly input msd_data
Or find the corresponding processing function from listeners. If there is an exact match, pass msd_data. Otherwise, pass action and msd_data
Both the server-side and client-side will call this method
监听连接发送的信息并处理。
根据消息的action：

	调用对应成员函数处理；直接传入msg_data
	或从listens中找对应的处理函数，如果精确匹配，传入msg_data，否则传入action, msg_data

服务器端和客户端都会调用此方法
*/
func (c *BanConn) RunForever() *errs.Error {
	if !core.LiveMode {
		return errs.NewMsg(errs.CodeRunTime, "BanConn is unavailable in mode %s", core.RunMode)
	}
	defer func() {
		c.Ready = false
		c.IsReading = false
		c.CloseFlag = true
		if c.Conn != nil {
			err_ := c.Conn.Close()
			if err_ != nil {
				log.Error("close conn fail", zap.String("remote", c.Remote), zap.Error(err_))
			}
			c.Conn = nil
		}
		if c.OnClose != nil {
			c.OnClose()
		}
	}()
	c.IsReading = true
	for {
		if c.CloseFlag {
			return errs.NewMsg(core.ErrRunTime, "connection closed by flag")
		}
		msg, err := c.ReadMsg()
		if err != nil {
			if err.Code == core.ErrDeCompressFail {
				// 无效消息，解压缩失败，忽略
				log.Error("invalid banIO msg, deCompress fail", zap.Error(err))
				continue
			}
			return err
		}
		isMatch := false
		for prefix, handle := range c.Listens {
			if strings.HasPrefix(msg.Action, prefix) {
				isMatch = true
				handle(msg.Action, msg.Data)
				break
			}
		}
		if !isMatch {
			log.Info("unhandle msg", zap.String("action", msg.Action))
		}
	}
}

/*
connect
A function used for reconnecting.
用于重新连接的函数。
*/
func (c *BanConn) connect() {
	c.lockConnect.Lock()
	defer c.lockConnect.Unlock()
	if c.Ready && btime.TimeMS()-c.RefreshMS < 2000 {
		// 连接已经刷新，跳过本次重试
		return
	}
	c.Ready = false
	if c.Conn != nil {
		_ = c.Conn.Close()
		c.Conn = nil
	}
	core.Sleep(time.Second * 3)
	c.DoConnect(c)
	c.RefreshMS = btime.TimeMS()
	if c.Conn != nil {
		if c.ReInitConn != nil {
			c.ReInitConn()
		}
		c.Ready = true
		log.Info("reconnect ok", zap.String("remote", c.Remote))
	}
}

func (c *BanConn) LoopPing(intvSecs int) {
	id := 0
	failNum := 0
	addrField := zap.String("addr", c.Remote)
	for {
		core.Sleep(time.Duration(intvSecs) * time.Second)
		if c.CloseFlag {
			log.Info("exit ping loop as close flag set", addrField)
			break
		}
		if !c.IsReading {
			continue
		}
		timeouts := float64(btime.UTCStamp()-c.heartBeatMs) / 1000 / float64(intvSecs)
		if id > 1 && timeouts > 2.2 {
			log.Error("close conn as ping timeout", addrField, zap.Int64("last", c.heartBeatMs))
			c.CloseFlag = true
			break
		}
		id += 1
		err := c.WriteMsg(&IOMsg{Action: "ping", Data: id})
		if err != nil {
			failNum += 1
			if failNum >= 2 {
				// 连续两次失败退出
				log.Error("close conn as ping fail", addrField, zap.String("err", err.Short()))
				c.CloseFlag = true
				break
			} else {
				log.Warn("write ping fail", addrField, zap.Error(err))
			}
		} else {
			failNum = 0
		}
	}
	if c.Conn != nil {
		err_ := c.Conn.Close()
		if err_ != nil {
			log.Warn("close ban conn error", addrField, zap.Error(err_))
		}
		c.Conn = nil
	}
}

func (c *BanConn) initListens() {
	c.Listens["subscribe"] = makeArrStrHandle(func(arr []string) {
		c.Subscribe(arr...)
	})
	c.Listens["unsubscribe"] = makeArrStrHandle(func(arr []string) {
		c.UnSubscribe(arr...)
	})
	c.Listens["ping"] = func(s string, i []byte) {
		var val int64
		err_ := utils.Unmarshal(i, &val, utils.JsonNumDefault)
		if err_ != nil {
			log.Warn("got bad ping", zap.ByteString("data", i))
			return
		}
		err := c.WriteMsg(&IOMsg{Action: "pong", Data: val + 1})
		if err != nil {
			log.Warn("write pong fail", zap.Int64("v", val), zap.Error(err))
			if c.IsServer {
				c.CloseFlag = true
			}
		} else {
			c.heartBeatMs = btime.UTCStamp()
			log.Debug("receive ping", zap.String("from", c.Remote), zap.Int64("v", val))
		}
	}
	c.Listens["pong"] = func(s string, i []byte) {
		c.heartBeatMs = btime.UTCStamp()
		log.Debug("receive pong", zap.String("from", c.Remote))
	}
}

func makeArrStrHandle(cb func(arr []string)) func(s string, data []byte) {
	return func(s string, data []byte) {
		var tags = make([]string, 0, 8)
		err := utils.Unmarshal(data, &tags, utils.JsonNumDefault)
		if err != nil {
			log.Error("receive invalid data", zap.String("n", s), zap.String("raw", string(data)),
				zap.Error(err))
			return
		}
		if len(tags) > 0 {
			cb(tags)
		}
	}
}

func compress(data []byte) ([]byte, *errs.Error) {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	_, err_ := w.Write(data)
	if err_ != nil {
		return nil, errs.New(core.ErrCompressFail, err_)
	}
	err_ = w.Close()
	if err_ != nil {
		return nil, errs.New(core.ErrCompressFail, err_)
	}
	return b.Bytes(), nil
}

func deCompress(compressed []byte) ([]byte, *errs.Error) {
	var result bytes.Buffer
	b := bytes.NewReader(compressed)

	// Create zlib decompressor
	// 创建 zlib 解压缩器
	r, err := zlib.NewReader(b)
	if err != nil {
		return nil, errs.New(core.ErrDeCompressFail, err)
	}
	defer r.Close()

	// Copy the decompressed data to the result
	// 将解压后的数据复制到 result 中
	_, err = io.Copy(&result, r)
	if err != nil {
		return nil, errs.New(core.ErrIOReadFail, err)
	}

	return result.Bytes(), nil
}

func getErrType(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return core.ErrNetTimeout, "op_timeout"
		} else if opErr.Temporary() {
			return core.ErrNetTemporary, "op_temporary"
		} else if opErr.Op == "dial" || opErr.Op == "read" || opErr.Op == "write" {
			return core.ErrNetConnect, "op_conn_dial"
		} else {
			return core.ErrNetUnknown, "op_err"
		}
	}
	var callErr *syscall.Errno
	if errors.As(err, &callErr) {
		if errors.Is(callErr, syscall.ECONNRESET) {
			return core.ErrNetConnect, "call_reset"
		} else if errors.Is(callErr, syscall.EPIPE) {
			return core.ErrNetConnect, "call_pipe"
		} else {
			return core.ErrNetUnknown, "call_err"
		}
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return core.ErrNetTimeout, "dead_exceeded"
	}
	if errors.Is(err, io.EOF) {
		return core.ErrNetConnect, "io_eof"
	} else if errors.Is(err, io.ErrClosedPipe) {
		return core.ErrNetConnect, "pipe_closed"
	} else {
		return core.ErrNetUnknown, "net_fail"
	}
}

type ServerIO struct {
	Addr      string
	Name      string
	Conns     []IBanConn
	Data      map[string]string // Cache data available for remote access 缓存的数据，可供远程端访问
	DataExp   map[string]int64  // Cache data expiration timestamp, 13 bits 缓存数据的过期时间戳，13位
	InitConn  func(*BanConn)
	lockConns deadlock.RWMutex
	stopFlag  bool
}

var (
	banServer *ServerIO
)

func NewBanServer(addr, name string) *ServerIO {
	var server ServerIO
	server.Addr = addr
	server.Name = name
	server.Data = map[string]string{}
	banServer = &server
	return &server
}

func (s *ServerIO) RunForever() *errs.Error {
	ln, err_ := net.Listen("tcp", s.Addr)
	if err_ != nil {
		return errs.New(core.ErrNetConnect, err_)
	}
	defer ln.Close()
	log.Info("banio started", zap.String("name", s.Name), zap.String("addr", s.Addr))
	
	// Start connection cleaner goroutine
	go s.cleanDeadConnections()
	
	// Start server heartbeat checker
	go s.checkHeartbeats()
	
	// Start server-side heartbeat sender
	go s.sendHeartbeats()
	
	for !s.stopFlag {
		conn_, err_ := ln.Accept()
		if err_ != nil {
			if s.stopFlag {
				return nil
			}
			return errs.New(core.ErrNetConnect, err_)
		}
		conn := s.WrapConn(conn_)
		log.Info("receive client", zap.String("remote", conn.GetRemote()))
		
		s.lockConns.Lock()
		s.Conns = append(s.Conns, conn)
		s.lockConns.Unlock()
		
		go func() {
			err := conn.RunForever()
			if err != nil {
				log.Warn("read client fail", zap.String("remote", conn.GetRemote()),
					zap.String("err", err.Message()))
			}
		}()
	}
	return nil
}

type KeyValExpire struct {
	Key        string
	Val        string
	ExpireSecs int `json:"expireSecs"`
}

type IOKeyVal struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

func (s *ServerIO) SetVal(args *KeyValExpire) {
	if args.Val == "" {
		// 删除值
		delete(s.Data, args.Key)
		return
	}
	s.Data[args.Key] = args.Val
	if args.ExpireSecs > 0 {
		s.DataExp[args.Key] = btime.TimeMS() + int64(args.ExpireSecs*1000)
	}
}

func (s *ServerIO) GetVal(key string) string {
	val, ok := s.Data[key]
	if !ok {
		return ""
	}
	if exp, ok := s.DataExp[key]; ok {
		if btime.TimeMS() >= exp {
			delete(s.Data, key)
			delete(s.DataExp, key)
			return ""
		}
	}
	return val
}

func (s *ServerIO) Broadcast(msg *IOMsg) *errs.Error {
	s.lockConns.RLock()
	curConns := make([]IBanConn, 0)
	for _, conn := range s.Conns {
		if !conn.IsClosed() && conn.HasTag(msg.Action) {
			curConns = append(curConns, conn)
		}
	}
	s.lockConns.RUnlock()
	
	if len(curConns) == 0 {
		return nil
	}
	raw, err_ := utils.Marshal(*msg)
	if err_ != nil {
		return errs.New(core.ErrMarshalFail, err_)
	}
	compressed, err := compress(raw)
	if err != nil {
		return err
	}
	for _, conn := range curConns {
		go func(c IBanConn) {
			err = c.Write(compressed, false)
			if err != nil {
				log.Warn("broadcast fail", zap.String("remote", c.GetRemote()),
					zap.String("tag", msg.Action), zap.Error(err))
			}
		}(conn)
	}
	return nil
}

// cleanDeadConnections periodically removes closed connections from the connection pool
func (s *ServerIO) cleanDeadConnections() {
	for !s.stopFlag {
		core.Sleep(time.Second * 10)
		
		s.lockConns.Lock()
		activeConns := make([]IBanConn, 0, len(s.Conns))
		for _, conn := range s.Conns {
			if !conn.IsClosed() {
				activeConns = append(activeConns, conn)
			} else {
				log.Info("removing dead connection", zap.String("remote", conn.GetRemote()))
			}
		}
		if len(activeConns) != len(s.Conns) {
			log.Info("cleaned connections", zap.Int("before", len(s.Conns)), 
				zap.Int("after", len(activeConns)))
			s.Conns = activeConns
		}
		s.lockConns.Unlock()
	}
}

// checkHeartbeats monitors server-side connections and closes inactive ones
func (s *ServerIO) checkHeartbeats() {
	for !s.stopFlag {
		core.Sleep(time.Second * 30)
		
		s.lockConns.RLock()
		conns := make([]IBanConn, len(s.Conns))
		copy(conns, s.Conns)
		s.lockConns.RUnlock()
		
		nowMs := btime.UTCStamp()
		for _, conn := range conns {
			if banConn, ok := conn.(*BanConn); ok {
				// Check if connection is inactive for more than 90 seconds
				if banConn.IsServer && !banConn.CloseFlag && 
					nowMs - banConn.heartBeatMs > 90000 {
					log.Warn("closing inactive connection", 
						zap.String("remote", banConn.Remote),
						zap.Int64("lastHB", banConn.heartBeatMs))
					banConn.CloseFlag = true
				}
			}
		}
	}
}

// sendHeartbeats sends periodic heartbeats from server to all active clients
func (s *ServerIO) sendHeartbeats() {
	id := 0
	for !s.stopFlag {
		core.Sleep(time.Second * 20)
		
		s.lockConns.RLock()
		conns := make([]IBanConn, len(s.Conns))
		copy(conns, s.Conns)
		s.lockConns.RUnlock()
		
		id++
		for _, conn := range conns {
			if !conn.IsClosed() {
				if banConn, ok := conn.(*BanConn); ok && banConn.IsServer {
					go func(c *BanConn, pingId int) {
						err := c.WriteMsg(&IOMsg{Action: "ping", Data: pingId})
						if err != nil {
							log.Debug("server ping fail", zap.String("remote", c.Remote), 
								zap.Error(err))
							c.CloseFlag = true
						}
					}(banConn, id)
				}
			}
		}
	}
}

func (s *ServerIO) WrapConn(conn net.Conn) *BanConn {
	res := &BanConn{
		Conn:        conn,
		Tags:        map[string]bool{},
		Listens:     map[string]ConnCB{},
		RefreshMS:   btime.TimeMS(),
		Ready:       true,
		Remote:      conn.RemoteAddr().String(),
		IsServer:    true,
		heartBeatMs: btime.UTCStamp(),
	}
	res.Listens["onGetVal"] = func(action string, data []byte) {
		var key string
		err_ := utils.Unmarshal(data, &key, utils.JsonNumDefault)
		if err_ != nil {
			log.Error("unmarshal fail onGetVal", zap.String("raw", string(data)), zap.Error(err_))
			return
		}
		val := s.GetVal(key)
		err := res.WriteMsg(&IOMsg{Action: "onGetValRes", Data: &IOKeyVal{
			Key: key,
			Val: val,
		}})
		if err != nil {
			log.Error("write val res fail", zap.Error(err))
		}
	}
	res.Listens["onSetVal"] = func(action string, data []byte) {
		var args KeyValExpire
		err := utils.Unmarshal(data, &args, utils.JsonNumDefault)
		if err != nil {
			log.Error("unmarshal fail onSetVal", zap.String("raw", string(data)), zap.Error(err))
			return
		}
		s.SetVal(&args)
	}
	res.initListens()
	if s.InitConn != nil {
		s.InitConn(res)
	}
	return res
}

type ClientIO struct {
	BanConn
	Addr  string
	waits map[string]chan string
}

func NewClientIO(addr string) (*ClientIO, *errs.Error) {
	conn, err_ := net.Dial("tcp", addr)
	if err_ != nil {
		return nil, errs.New(core.ErrNetConnect, err_)
	}
	res := &ClientIO{
		Addr: addr,
		BanConn: BanConn{
			Conn:        conn,
			Tags:        map[string]bool{},
			Remote:      conn.RemoteAddr().String(),
			Listens:     map[string]ConnCB{},
			RefreshMS:   btime.TimeMS(),
			Ready:       true,
			heartBeatMs: btime.UTCStamp(),
			IsServer:    false,
		},
		waits: map[string]chan string{},
	}
	res.Listens["onGetValRes"] = func(_ string, data []byte) {
		var val IOKeyVal
		err := utils.Unmarshal(data, &val, utils.JsonNumDefault)
		if err != nil {
			log.Error("onGetValRes unmarshal fail", zap.String("raw", string(data)), zap.Error(err))
		} else {
			out, ok := res.waits[val.Key]
			if !ok {
				return
			}
			out <- val.Val
		}
	}
	res.initListens()
	// This is only responsible for connection, no initialization required, leave it to connect for initialization
	// 这里只负责连接，无需初始化，交给connect初始化
	res.DoConnect = func(c *BanConn) {
		retryCount := 0
		for !c.CloseFlag {
			cn, err_ := net.Dial("tcp", addr)
			if err_ != nil {
				retryCount++
				curMS := btime.TimeMS()
				tipRetryTimesLock.Lock()
				nextMS, _ := tipRetryTimes[addr]
				if curMS > nextMS {
					tipRetryTimes[addr] = curMS + 10000
					log.Error("connect fail, sleep 10s and retry..", 
						zap.String("addr", addr), 
						zap.Int("retry", retryCount))
				}
				tipRetryTimesLock.Unlock()
				core.Sleep(time.Second * 10)
				continue
			}
			c.Conn = cn
			c.Remote = cn.RemoteAddr().String()
			c.heartBeatMs = btime.UTCStamp()
			retryCount = 0
			return
		}
	}
	banClient = res
	return res, nil
}

const (
	readTimeout = 120
)

func (c *ClientIO) GetVal(key string, timeout int) (string, *errs.Error) {
	err := c.WriteMsg(&IOMsg{
		Action: "onGetVal",
		Data:   key,
	})
	if err != nil {
		return "", err
	}
	if timeout == 0 {
		timeout = readTimeout
	}
	out := make(chan string)
	c.waits[key] = out
	var res string
	select {
	case res = <-out:
	case <-time.After(time.Second * time.Duration(timeout)):
		close(out)
		delete(c.waits, key)
	}
	return res, nil
}

func (c *ClientIO) SetVal(args *KeyValExpire) *errs.Error {
	return c.WriteMsg(&IOMsg{
		Action: "onSetVal",
		Data:   *args,
	})
}

var (
	banClient *ClientIO
)

func HasBanConn() bool {
	return banClient != nil || banServer != nil
}

func GetServerData(key string) (string, *errs.Error) {
	if banServer != nil {
		data := banServer.GetVal(key)
		return data, nil
	}
	if banClient == nil {
		return "", errs.NewMsg(core.ErrRunTime, "banClient not load")
	}
	return banClient.GetVal(key, 0)
}

func SetServerData(args *KeyValExpire) *errs.Error {
	if banServer != nil {
		banServer.SetVal(args)
		return nil
	}
	if banClient == nil {
		return errs.NewMsg(core.ErrRunTime, "banClient not load")
	}
	return banClient.SetVal(args)
}

func GetNetLock(key string, timeout int) (int32, *errs.Error) {
	lockKey := "lock_" + key
	val, err := GetServerData(lockKey)
	if err != nil {
		return 0, err
	}
	lockVal := rand.Int31()
	lockStr := fmt.Sprintf("%v", lockVal)
	if val == "" {
		err = SetServerData(&KeyValExpire{Key: lockKey, Val: lockStr})
		return lockVal, err
	}
	if timeout == 0 {
		timeout = 30
	}
	stopAt := btime.Time() + float64(timeout)
	for btime.Time() < stopAt {
		core.Sleep(time.Microsecond * 10)
		val, err = GetServerData(lockKey)
		if err != nil {
			return 0, err
		}
		if val == "" {
			err = SetServerData(&KeyValExpire{Key: lockKey, Val: lockStr})
			return lockVal, err
		}
	}
	return 0, errs.NewMsg(core.ErrTimeout, "GetNetLock for %s", key)
}

func DelNetLock(key string, lockVal int32) *errs.Error {
	lockKey := "lock_" + key
	val, err := GetServerData(lockKey)
	if err != nil {
		return err
	}
	lockStr := fmt.Sprintf("%v", lockVal)
	if val == lockStr {
		return SetServerData(&KeyValExpire{Key: lockKey, Val: ""})
	}
	log.Info("del lock fail", zap.String("val", val), zap.Int32("exp", lockVal))
	return nil
}
