package apiAgent

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/shirou/gopsutil/v4/cpu"
)

// Mappings define the local services to be exposed
var mappings = map[string]string{
	// "api": "http://127.0.0.1:7780", // HTTP service
	"alist": "http://127.0.0.1:5244", // WebSite
	// "ws": "ws://127.0.0.1:1882", // WebSocket service
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

// GetHardwareID 生成一个基于CPU信息的唯一ID
func GetHardwareID() (string, error) {
	// 获取CPU信息
	info, err := cpu.Info()
	if err != nil {
		return "", fmt.Errorf("无法获取CPU信息: %v", err)
	}

	if len(info) == 0 {
		return "", fmt.Errorf("未找到CPU信息")
	}

	// 使用CPU的VendorID和ModelName生成哈希
	hash := sha256.New()
	hash.Write([]byte(info[0].VendorID + info[0].ModelName))
	hashSum := hash.Sum(nil)

	// 取哈希值的前8位作为ID
	id := hex.EncodeToString(hashSum)[:8]

	return id, nil
}

// RunClient 启动客户端连接，支持参数传入
func RunClient(serverAddr, clientId string, serverKey string) error {
	hardwareID, err := GetHardwareID()
	if err != nil {
		return fmt.Errorf("无法生成硬件ID: %v", err)
	}
	newClientId := clientId + hardwareID
	// 兼容旧接口，默认明文
	return RunClientWithTLS(serverAddr, newClientId, serverKey, false, false)
}

// RunClientWithTLS 支持 TLS/明文两种模式
func RunClientWithTLS(serverAddr, clientId, serverKey string, enableTLS, tlsInsecure bool) error {
	retryDelay := 5 * time.Second // 重试间隔
	for {
		log.Printf("Attempting to connect to server at %s with clientId %s (TLS: %v)", serverAddr, clientId, enableTLS)
		err := runClient(serverAddr, clientId, serverKey, enableTLS, tlsInsecure)
		if err != nil {
			log.Printf("Client error: %v. Retrying in %v...", err, retryDelay)
			time.Sleep(retryDelay)
		}
	}
}

func runClient(serverAddr, clientId string, serverKey string, enableTLS, tlsInsecure bool) error {
	var conn net.Conn
	var err error
	if enableTLS {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: tlsInsecure,
		}
		conn, err = tls.Dial("tcp", serverAddr, tlsCfg)
	} else {
		conn, err = net.Dial("tcp", serverAddr)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	// 尝试读取服务器的首行响应
	if enableTLS {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		resp, _ := bufio.NewReader(conn).ReadString('\n')
		conn.SetReadDeadline(time.Time{}) // 清除超时
		if strings.HasPrefix(resp, "ERROR:") {
			log.Printf("Server rejected connection: %s", strings.TrimSpace(resp))
			conn.Close()
			return fmt.Errorf("server rejected connection: %s", strings.TrimSpace(resp))
		}
		if strings.HasPrefix(resp, "SUCCESS:") {
			log.Printf("Server accepted connection: %s", strings.TrimSpace(resp))
		}
	} else {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		resp, err := bufio.NewReader(conn).ReadString('\n')
		conn.SetReadDeadline(time.Time{}) // 清除超时
		if err != nil {
			// 读取失败，可能是被服务器直接关闭
			log.Printf("Failed to read server response: %v", err)
			conn.Close()
			// log.Printf("Server closed connection or sent no response (maybe TLS required?)")
			return fmt.Errorf("server closed connection or sent no response (maybe TLS required?)")
		}
		if strings.HasPrefix(resp, "ERROR:") {
			log.Printf("Server rejected connection: %s", strings.TrimSpace(resp))
			conn.Close()
			return fmt.Errorf("server rejected connection: %s", strings.TrimSpace(resp))
		}
		if strings.HasPrefix(resp, "SUCCESS:") {
			log.Printf("Server accepted connection: %s", strings.TrimSpace(resp))
		}
	}

	log.Printf("%s Connected to server.", clientId)

	fmt.Fprintf(conn, "%s\n", serverKey)
	// 发送完 serverKey 后，再读取一行响应
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := bufio.NewReader(conn).ReadString('\n')
	conn.SetReadDeadline(time.Time{}) // 清除超时
	if err != nil {
		log.Printf("Failed to read server response after sending key: %v", err)
		conn.Close()
		return fmt.Errorf("server closed connection or sent no response after key")
	}
	if strings.HasPrefix(resp, "ERROR:") {
		log.Printf("Server rejected connection after serverKey: %s", strings.TrimSpace(resp))
		conn.Close()
		return fmt.Errorf("server rejected connection after serverKey: %s", strings.TrimSpace(resp))
	}
	if strings.HasPrefix(resp, "SUCCESS:") {
		log.Printf("Server Validated serverKey: %s", strings.TrimSpace(resp))
	}

	// 创建 yamux 会话配置
	config := yamux.DefaultConfig()
	config.KeepAliveInterval = 10 * time.Second
	config.ConnectionWriteTimeout = 30 * time.Second
	config.EnableKeepAlive = true
	config.MaxStreamWindowSize = 256 * 1024
	// Setup yamux session
	session, err := yamux.Client(conn, config)
	if err != nil {
		return fmt.Errorf("failed to create yamux session: %w", err)
	}
	defer session.Close()

	// 启动心跳 goroutine
	go func() {
		for {
			if session.IsClosed() {
				log.Println("Heartbeat goroutine exit: session closed")
				return
			}
			stream, err := session.OpenStream()
			if err != nil {
				log.Printf("Heartbeat: failed to open stream: %v", err)
			} else {
				_, werr := stream.Write([]byte("ping"))
				if werr != nil {
					log.Printf("Heartbeat: failed to write ping: %v", werr)
				} else {
					pongBuf := make([]byte, 16)
					var reply string
					for {
						n, rerr := stream.Read(pongBuf)
						if n > 0 {
							reply += string(pongBuf[:n])
						}
						if rerr == io.EOF {
							break
						}
						if rerr != nil {
							log.Printf("Heartbeat: failed to read pong: %v", rerr)
							break
						}
					}
					// log.Printf("Heartbeat: raw reply bytes: %x", reply)
					if strings.Contains(reply, "pong") {
						// log.Printf("Heartbeat: received pong from server")
					} else {
						log.Printf("Heartbeat: unexpected reply: %s", reply)
					}
				}
			}
			stream.Close()
			time.Sleep(5 * time.Second)
		}
	}()

	// Open a stream for registration
	stream, err := session.OpenStream()
	if err != nil {
		return fmt.Errorf("failed to open registration stream: %w", err)
	}

	// Build and send registration message
	regMsg := fmt.Sprintf("register %s", clientId)
	for name, target := range mappings {
		regMsg += fmt.Sprintf(" %s=%s", name, target)
	}
	_, err = stream.Write([]byte(regMsg))
	stream.Close() // Close the stream after sending
	if err != nil {
		return fmt.Errorf("failed to send registration message: %w", err)
	}
	log.Printf("Registered with clientId '%s' and mappings: %v", clientId, mappings)
	//Build and send AccessKey message
	// err = sendAccessKey(session, serverKey)
	// if err != nil {
	// 	return fmt.Errorf("failed to send access key: %w", err)
	// }

	// 持续接收代理请求，只有 session 关闭或网络故障才重连
	for {
		proxyStream, err := session.AcceptStream()
		if err != nil {
			log.Printf("Session closed or network error: %v", err)
			break
		}
		go handleProxyStream(proxyStream)
	}
	return nil
}

// 发送后端API服务的 Accesskey 给wServer程序
func sendAccessKey(session *yamux.Session, accessKey string) error {
	if accessKey == "" {
		return fmt.Errorf("access key is empty")
	}
	stream, err := session.OpenStream()
	if err != nil {
		return fmt.Errorf("failed to open stream for access key: %w", err)
	}
	defer stream.Close()
	_, err = stream.Write([]byte(accessKey + "\n"))
	if err != nil {
		return fmt.Errorf("failed to send access key: %w", err)
	}
	return nil
}

// handleProxyStream 处理http代理请求
func handleProxyStream(stream net.Conn) {
	defer stream.Close()
	reader := bufio.NewReader(stream)

	// Read the request from the server
	req, err := http.ReadRequest(reader)
	if err != nil {
		log.Printf("Failed to read request from stream: %v", err)
		return
	}
	log.Printf("Received request: %s %s", req.Method, req.URL.Path)
	// The path from server is like "/a1/api/v1/sysinfo"
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) < 1 {
		log.Printf("Invalid request path received: %s", req.URL.Path)
		return
	}

	mappingName := parts[0]
	localTarget, ok := mappings[mappingName]

	// 支持 /ws 直接映射到 video 的本地服务，并强制 WebSocket 代理
	if !ok && mappingName == "ws" {
		localTarget, ok = mappings["video"]
	}
	if !ok {
		log.Printf("Received request for unknown mapping: %s", mappingName)
		resp := createErrorResponse(http.StatusNotFound, "Mapping Not Found")
		resp.Write(stream)
		return
	}

	// 检查是否是WebSocket升级请求，或路径中包含 /api/ws 或 /ws
	isWebSocket := (strings.ToLower(req.Header.Get("Upgrade")) == "websocket" &&
		strings.ToLower(req.Header.Get("Connection")) == "upgrade") ||
		strings.HasPrefix(req.URL.Path, "/api/ws") ||
		strings.HasPrefix(req.URL.Path, "/ws")

	// Prepare the request for the local service
	targetURL, err := url.Parse(localTarget)
	if err != nil {
		log.Printf("Invalid local target URL for mapping '%s': %v", mappingName, err)
		resp := createErrorResponse(http.StatusInternalServerError, "Invalid Target Configuration")
		resp.Write(stream)
		return
	}

	// 对于WebSocket，需要特殊处理
	if isWebSocket {
		handleWebSocketProxy(stream, req, targetURL, parts)
		return
	}

	// 标准HTTP请求处理
	req.URL.Scheme = targetURL.Scheme
	req.URL.Host = targetURL.Host
	req.URL.Path = "/" + strings.Join(parts[1:], "/")
	// req.Host 远程请求时的主机名，targetURL.Host 是本地服务的主机名
	// req.Host = targetURL.Host
	req.RequestURI = ""

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Failed to forward request to local target %s: %v", localTarget, err)
		errorResp := createErrorResponse(http.StatusBadGateway, "Local service unavailable")
		errorResp.Write(stream)
		return
	}
	defer resp.Body.Close()

	err = resp.Write(stream)
	if err != nil {
		log.Printf("Failed to write response to stream: %v", err)
	}
}

// handleWebSocketProxy 处理WebSocket代理
func handleWebSocketProxy(clientStream net.Conn, req *http.Request, targetURL *url.URL, parts []string) {
	log.Printf("[WebSocket] Starting WebSocket proxy for: %s", req.URL.Path)

	// 如果目标已经是WebSocket URL，直接使用
	wsURL := *targetURL

	// 如果映射中配置的是 http/https，转换为 ws/wss
	if wsURL.Scheme == "http" {
		wsURL.Scheme = "ws"
	} else if wsURL.Scheme == "https" {
		wsURL.Scheme = "wss"
	}
	// 如果已经是 ws/wss，保持不变

	// 构建完整路径 - 特殊处理 /ws 映射
	if len(parts) > 1 {
		// 如果原始URL已经有路径，保留它，然后添加额外的路径部分
		additionalPath := "/" + strings.Join(parts[1:], "/")
		if wsURL.Path == "" || wsURL.Path == "/" {
			wsURL.Path = additionalPath
		} else {
			wsURL.Path = wsURL.Path + additionalPath
		}
		// 保留查询参数
		if req.URL.RawQuery != "" {
			wsURL.RawQuery = req.URL.RawQuery
		}
		// log.Println("[ws] mapping", parts[0], "to", wsURL.String())
	}

	// 转换为HTTP URL进行连接（WebSocket握手使用HTTP）
	httpURL := wsURL
	if httpURL.Scheme == "ws" {
		httpURL.Scheme = "http"
	} else if httpURL.Scheme == "wss" {
		httpURL.Scheme = "https"
	}

	log.Printf("[WebSocket] Connecting to: %s", httpURL.String())

	// 连接到本地WebSocket服务
	serverConn, err := net.Dial("tcp", httpURL.Host)
	if err != nil {
		log.Printf("[WebSocket] Failed to connect to WebSocket server: %v", err)
		resp := createErrorResponse(http.StatusBadGateway, "WebSocket server unavailable")
		resp.Write(clientStream)
		return
	}
	defer serverConn.Close()

	// 修改请求URL和Host，使用HTTP协议进行握手
	req.URL.Scheme = httpURL.Scheme
	req.URL.Host = httpURL.Host
	req.URL.Path = wsURL.Path
	req.URL.RawQuery = wsURL.RawQuery
	req.RequestURI = ""

	// 自动补全 WebSocket 必要头部
	if strings.ToLower(req.Header.Get("Upgrade")) != "websocket" {
		req.Header.Set("Upgrade", "websocket")
	}
	if !strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade") {
		req.Header.Set("Connection", "Upgrade")
	}
	if req.Header.Get("Sec-WebSocket-Key") == "" {
		// 生成一个随机的 Sec-WebSocket-Key
		key := make([]byte, 16)
		for i := range key {
			key[i] = byte(65 + i) // 简单填充，生产环境可用 crypto/rand
		}
		req.Header.Set("Sec-WebSocket-Key", string(key))
	}
	if req.Header.Get("Sec-WebSocket-Version") == "" {
		req.Header.Set("Sec-WebSocket-Version", "13")
	}

	log.Printf("[WebSocket] Forwarding upgrade request to: %s", req.URL.String())

	// 发送升级请求到本地服务
	err = req.Write(serverConn)
	if err != nil {
		log.Printf("[WebSocket] Failed to write WebSocket upgrade request: %v", err)
		return
	}

	// 读取升级响应
	serverReader := bufio.NewReader(serverConn)
	resp, err := http.ReadResponse(serverReader, req)
	if err != nil {
		log.Printf("[WebSocket] Failed to read WebSocket upgrade response: %v", err)
		return
	}

	log.Printf("[WebSocket] Received upgrade response: Status=%d", resp.StatusCode)

	// 转发升级响应到客户端
	err = resp.Write(clientStream)
	if err != nil {
		log.Printf("[WebSocket] Failed to write WebSocket upgrade response: %v", err)
		return
	}

	// 如果升级成功，开始双向数据转发
	if resp.StatusCode == 101 { // Switching Protocols
		log.Printf("[WebSocket] WebSocket upgrade successful, starting bidirectional forwarding")

		// 双向数据转发
		done := make(chan struct{}, 2)

		// 服务器 -> 客户端流
		go func() {
			defer func() { done <- struct{}{} }()
			_, err := io.Copy(clientStream, serverConn)
			if err != nil {
				log.Printf("[WebSocket] Error copying from server to client: %v", err)
			}
		}()

		// 客户端流 -> 服务器
		go func() {
			defer func() { done <- struct{}{} }()
			_, err := io.Copy(serverConn, clientStream)
			if err != nil {
				log.Printf("[WebSocket] Error copying from client to server: %v", err)
			}
		}()

		// 等待任一方向的转发结束
		<-done
		log.Printf("[WebSocket] WebSocket connection closed")
	} else {
		log.Printf("[WebSocket] WebSocket upgrade failed with status: %d", resp.StatusCode)
	}
}

// createErrorResponse is a helper to build an HTTP response for errors
func createErrorResponse(statusCode int, message string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(bytes.NewBufferString(message)),
	}
}
