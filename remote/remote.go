/*
 * This module is responsible for recieving and running remote WP CLI requests and streaming the logs back to the requesting client.
 */

package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/creack/pty"
	"github.com/howeyc/fsnotify"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/net/websocket"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
)

const (
	shutdownErrorCode = 4001 // WebSocket close code when a shutdown signal is detected
)

var nonUTF8Replacement = []byte(string(unicode.ReplacementChar))

// Holds info related to a specific remote CLI that is running.
type wpCLIProcess struct {
	GUID          string
	Cmd           *exec.Cmd
	Tty           *os.File
	Running       bool
	LogFileName   string
	BytesLogged   int64
	BytesStreamed map[string]int64
	padlock       *sync.Mutex
}

var (
	gGUIDLength = 36
	gGUIDttys   map[string]*wpCLIProcess
	padlock     *sync.Mutex
	guidRegex   *regexp.Regexp

	blackListed1stLevel = []string{"admin", "cli", "config", "core", "db", "dist-archive",
		"eval-file", "eval", "find", "i18n", "scaffold", "server", "package", "profile"}

	blackListed2ndLevel = map[string][]string{
		"media":    {"regenerate"},
		"theme":    {"install", "update", "delete"},
		"plugin":   {"install", "update", "delete"},
		"language": {"install", "update", "delete"},
		"vip":      {"support-user"},
	}
)

type config struct {
	remoteToken   string
	useWebsockets bool
	wpCLIPath     string
	wpPath        string
}

var remoteConfig config

// Setup configures the module (not super ideal, but this module needs some reworking to make it better)
func Setup(remoteToken string, useWebsockets bool, wpCLIPath string, wpPath string) {
	remoteConfig = config{
		remoteToken:   remoteToken,
		useWebsockets: useWebsockets,
		wpCLIPath:     wpCLIPath,
		wpPath:        wpPath,
	}
}

// ListenForConnections is the entrypoint. Listens for, and processes, the remote requests.
func ListenForConnections() {
	gGUIDttys = make(map[string]*wpCLIProcess)
	padlock = &sync.Mutex{}

	guidRegex = regexp.MustCompile("^[a-fA-F0-9\\-]+$")
	if nil == guidRegex {
		log.Println("Failed to compile the Guid regex")
		return
	}

	listenAddr := "0.0.0.0:22122"

	if remoteConfig.useWebsockets {
		s := &http.Server{
			Addr: listenAddr,
			ConnContext: func(ctx context.Context, c net.Conn) context.Context {
				if tcpConn, ok := c.(*net.TCPConn); ok && tcpConn != nil {
					tcpConn.SetKeepAlivePeriod(30 * time.Second)
					tcpConn.SetKeepAlive(true)
					tcpConn.SetReadBuffer(8192)
				}
				return ctx
			},
			Handler: websocket.Handler(func(wsConn *websocket.Conn) {
				log.Printf("websocket connection from %s\n", wsConn.RemoteAddr().String())
				authConn(wsConn)
			}),
		}
		log.Printf("Listening for websocket protocol on %q...", listenAddr)
		wsErr := s.ListenAndServe()
		log.Printf("Websocket listener stopped: %v", wsErr)
		return
	}

	addr, err := net.ResolveTCPAddr("tcp4", listenAddr)
	if err != nil {
		log.Printf("error resolving listen address: %s\n", err.Error())
		return
	}

	listener, err := net.ListenTCP("tcp4", addr)
	if err != nil {
		log.Printf("error listening on %s: %s\n", addr.String(), err.Error())
		return
	}
	defer listener.Close()

	for {
		log.Println("listening...")
		conn, err := listener.AcceptTCP()
		log.Printf("connection from %s\n", conn.RemoteAddr().String())
		if err != nil {
			log.Printf("error accepting connection: %s\n", err.Error())
			continue
		}
		go authConn(conn)
	}
}

func authConn(conn net.Conn) {
	var rows, cols uint16
	var offset int64
	var token, GUID, cmd string
	var read int
	var err error
	var data []byte
	buf := make([]byte, 65535)

	log.Println("waiting for auth data")

	conn.SetReadDeadline(time.Now().Add(time.Duration(5000 * time.Millisecond.Nanoseconds())))
	bufReader := bufio.NewReader(conn)

	for {
		read, err = bufReader.Read(buf)

		if nil != err && !strings.Contains(err.Error(), "i/o timeout") {
			conn.Write([]byte("error during handshaking\n"))
			log.Printf("error handshaking: %s\n", err.Error())
			conn.Close()
			return
		}

		if 0 != read {
			if nil == data {
				data = make([]byte, read)
				copy(data, buf[:read])
			} else {
				data = append(data, buf[:read]...)
			}
		} else if 0 == bufReader.Buffered() {
			break
		}

		conn.SetReadDeadline(time.Now().Add(time.Duration(200 * time.Millisecond.Nanoseconds())))
	}
	buf = nil

	size := len(data)
	log.Printf("size of handshake %d\n", size)

	// This is the minimum size to determine the protocol type
	if size < len(remoteConfig.remoteToken)+gGUIDLength {
		conn.Write([]byte("Error negotiating handshake"))
		log.Println("error negotiating the handshake")
		conn.Close()
		return
	}

	newlineChars := 1
	if 1 < size && 0xd == (data[size-2 : size-1])[0] {
		newlineChars = 2
	}

	// Determine if the packet structure is the new version or not
	if ';' != data[len(remoteConfig.remoteToken)] {
		token, GUID, rows, cols, offset, cmd, err = authenticateProtocolHeader2(data[:size-newlineChars])
	} else {
		token, GUID, rows, cols, cmd, err = authenticateProtocolHeader1(string(data[:size-newlineChars]))
	}
	data = nil

	if nil != err {
		conn.Write([]byte(err.Error()))
		log.Println(err.Error())
		conn.Close()
		return
	}

	if token != remoteConfig.remoteToken {
		conn.Write([]byte("invalid auth handshake"))
		log.Printf("error incorrect handshake string")
		conn.Close()
		return
	}

	log.Println("handshake complete!")

	conn.SetReadDeadline(time.Time{})
	if tcpConn, ok := conn.(*net.TCPConn); ok && tcpConn != nil {
		tcpConn.SetKeepAlivePeriod(time.Duration(30 * time.Second.Nanoseconds()))
		tcpConn.SetKeepAlive(true)
	}

	padlock.Lock()
	wpCLIProcess, found := gGUIDttys[GUID]
	padlock.Unlock()

	if found && wpCLIProcess.Running {
		if "vip-go-retrieve-remote-logs" == cmd {
			conn.Write([]byte(fmt.Sprintf("Not sending the logs because the WP-CLI command with GUID %s is still running", GUID)))
			conn.Close()
			return
		}

		// Reattach to the running WP-CLi command
		attachWpCliCmdRemote(conn, wpCLIProcess, GUID, uint16(rows), uint16(cols), int64(offset))
		return
	}

	// The GUID is not currently running
	wpCliCmd, err := validateCommand(cmd)
	if nil != err {
		log.Println(err.Error())
		conn.Write([]byte(err.Error()))
		conn.Close()
		return
	}

	if "vip-go-retrieve-remote-logs" == wpCliCmd {
		streamLogs(conn, GUID)
		return
	}

	err = runWpCliCmdRemote(conn, GUID, uint16(rows), uint16(cols), wpCliCmd)
	if nil != err {
		log.Println(err.Error())
	}
}

func authenticateProtocolHeader1(dataString string) (string, string, uint16, uint16, string, error) {
	var token, guid string
	var rows, cols uint64
	var err error

	elems := strings.Split(dataString, ";")
	if 5 > len(elems) {
		return "", "", 0, 0, "", errors.New("error handshake format incorrect")
	}

	token = elems[0]
	if len(token) != len(remoteConfig.remoteToken) {
		return "", "", 0, 0, "", fmt.Errorf("error incorrect handshake reply size: %d != %d", len(remoteConfig.remoteToken), len(elems[0]))
	}

	guid = elems[1]
	if !guidRegex.Match([]byte(guid)) {
		return "", "", 0, 0, "", errors.New("error incorrect GUID format")
	}

	rows, err = strconv.ParseUint(elems[2], 10, 16)
	if nil != err {
		return "", "", 0, 0, "", fmt.Errorf("error incorrect console rows setting: %s", err.Error())
	}

	cols, err = strconv.ParseUint(elems[3], 10, 16)
	if nil != err {
		return "", "", 0, 0, "", fmt.Errorf("error incorrect console columns setting: %s", err.Error())
	}

	return token, guid, uint16(rows), uint16(cols), strings.Join(elems[4:], ";"), nil
}

func authenticateProtocolHeader2(data []byte) (string, string, uint16, uint16, int64, string, error) {
	var token, guid string
	var rows, cols uint64
	var offset uint64
	var err error

	if len(data) < len(remoteConfig.remoteToken)+gGUIDLength+4+4+8 {
		return "", "", 0, 0, 0, "", errors.New("error negotiating the v2 protocol handshake")
	}

	token = string(data[:len(remoteConfig.remoteToken)])
	guid = string(data[len(remoteConfig.remoteToken) : len(remoteConfig.remoteToken)+gGUIDLength])

	if !guidRegex.Match([]byte(guid)) {
		return "", "", 0, 0, 0, "", errors.New("error incorrect GUID format")
	}

	rows, err = strconv.ParseUint(string(data[len(remoteConfig.remoteToken)+gGUIDLength:len(remoteConfig.remoteToken)+gGUIDLength+4]), 10, 16)
	if nil != err {
		return "", "", 0, 0, 0, "", fmt.Errorf("error incorrect console rows setting: %s", err.Error())
	}

	cols, err = strconv.ParseUint(string(data[len(remoteConfig.remoteToken)+gGUIDLength+4:len(remoteConfig.remoteToken)+gGUIDLength+4+4]), 10, 16)
	if nil != err {
		return "", "", 0, 0, 0, "", fmt.Errorf("error incorrect console columns setting: %s", err.Error())
	}

	offset = binary.LittleEndian.Uint64(data[len(remoteConfig.remoteToken)+gGUIDLength+4+4 : len(remoteConfig.remoteToken)+gGUIDLength+4+4+8])

	return token, guid, uint16(rows), uint16(cols), int64(offset), string(data[len(remoteConfig.remoteToken)+gGUIDLength+4+4+8:]), nil
}

func validateCommand(calledCmd string) (string, error) {
	if 0 == len(strings.TrimSpace(calledCmd)) {
		return "", errors.New("No WP CLI command specified")
	}

	cmdParts := strings.Fields(strings.TrimSpace(calledCmd))
	if 0 == len(cmdParts) {
		return "", errors.New("WP CLI command not sent")
	}

	for _, command := range blackListed1stLevel {
		if strings.ToLower(strings.TrimSpace(cmdParts[0])) == command {
			return "", fmt.Errorf("WP CLI command '%s' is not permitted", command)
		}
	}

	if 1 == len(cmdParts) {
		return strings.TrimSpace(cmdParts[0]), nil
	}

	for command, blacklistedMap := range blackListed2ndLevel {
		for _, subCommand := range blacklistedMap {
			if strings.ToLower(strings.TrimSpace(cmdParts[0])) == command &&
				strings.ToLower(strings.TrimSpace(cmdParts[1])) == subCommand {
				return "", fmt.Errorf("WP CLI command '%s %s' is not permitted", command, subCommand)
			}
		}
	}

	return strings.Join(cmdParts, " "), nil
}

func getCleanWpCliArgumentArray(wpCliCmdString string) ([]string, error) {
	rawArgs := tokenizeString(wpCliCmdString)
	cleanArgs := make([]string, 0)
	openQuote := false
	arg := ""

	for _, rawArg := range rawArgs {
		if idx := strings.Index(rawArg, "\""); -1 != idx {
			if idx != strings.LastIndexAny(rawArg, "\"") {
				cleanArgs = append(cleanArgs, rawArg)
			} else if openQuote {
				arg = fmt.Sprintf("%s %s", arg, rawArg)
				cleanArgs = append(cleanArgs, arg)
				arg = ""
				openQuote = false
			} else {
				arg = rawArg
				openQuote = true
			}
		} else {
			if openQuote {
				arg = fmt.Sprintf("%s %s", arg, rawArg)
			} else {
				cleanArgs = append(cleanArgs, rawArg)
			}
		}
	}

	if openQuote {
		return make([]string, 0), errors.New(fmt.Sprintf("WP CLI command is invalid: %s\n", wpCliCmdString))
	}

	// Remove quotes from the args
	for i := range cleanArgs {
		if !isJSONObject(cleanArgs[i]) { //don't alter JSON arguments
			cleanArgs[i] = strings.ReplaceAll(cleanArgs[i], "\"", "")
		}
	}

	return cleanArgs, nil
}

func connWriteUTF8(conn net.Conn, data []byte) (int, int, error) {
	if conn == nil {
		return 0, 0, errors.New("invalid connection")
	}

	utf8data := bytes.ToValidUTF8(data, nonUTF8Replacement)

	n, err := conn.Write(utf8data)

	return len(data), n, err
}

func processShutdown(conn net.Conn, wpcli *wpCLIProcess) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(sigChan)

	sig := <-sigChan
	log.Printf("remote: caught termination signal %v, starting shutdown. GUID: %s\n", sig, wpcli.GUID)

	wpcli.padlock.Lock()

	wsConn, ok := conn.(*websocket.Conn)
	if ok {
		wsConn.WriteClose(shutdownErrorCode)
	}

	conn.Close()

	wpcli.Cmd.Process.Kill()

	wpcli.padlock.Unlock()
}

func processTCPConnectionData(conn net.Conn, wpcli *wpCLIProcess) {
	data := make([]byte, 8192)
	var size, written int
	var err error
	if tcpConn, ok := conn.(*net.TCPConn); ok && tcpConn != nil {
		tcpConn.SetReadBuffer(8192)
	}
	for {
		size, err = conn.Read(data)

		if nil != err {
			if io.EOF == err {
				log.Printf("client connection closed. GUID: %s\n", wpcli.GUID)
			} else if !strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("error reading from the client connection: %s. GUID: %s\n", err.Error(), wpcli.GUID)
			}
			break
		}

		if 0 == size {
			log.Println("ignoring data of length 0")
			continue
		}

		if 1 == size && 0x3 == data[0] {
			log.Printf("Ctrl-C received. GUID: %s\n", wpcli.GUID)
			wpcli.padlock.Lock()
			// If this is the only process, then we can stop the command
			if 1 == len(wpcli.BytesStreamed) {
				wpcli.Cmd.Process.Kill()
				log.Printf("terminating the WP-CLI. GUID: %s\n", wpcli.GUID)
			}
			wpcli.padlock.Unlock()
			break
		}

		if 4 < len(data) && "\xc2\x9b8;" == string(data[:4]) && 't' == data[size-1:][0] {
			cmdParts := strings.Split(string(data[4:size]), ";")

			rows, err := strconv.ParseUint(cmdParts[0], 10, 16)
			if nil != err {
				log.Printf("error reading rows resize data from the WP CLI client: %s\n", err.Error())
				continue
			}
			cols, err := strconv.ParseUint(cmdParts[1][:len(cmdParts[1])-1], 10, 16)
			if nil != err {
				log.Printf("error reading columns resize data from the WP CLI client: %s\n", err.Error())
				continue
			}

			wpcli.padlock.Lock()
			err = pty.Setsize(wpcli.Tty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
			wpcli.padlock.Unlock()

			if nil != err {
				log.Printf("error performing window resize: %s\n", err.Error())
			} else {
				log.Printf("set new window size: %dx%d\n", rows, cols)
			}
			continue
		}

		wpcli.padlock.Lock()
		written, err = wpcli.Tty.Write(data[:size])
		wpcli.padlock.Unlock()

		if nil != err {
			if io.EOF != err {
				log.Printf("error writing to the WP CLI tty: %s\n", err.Error())
				break
			}
		}
		if written != size {
			log.Println("error writing to the WP CLI tty: not enough data written")
			break
		}
	}
}

func attachWpCliCmdRemote(conn net.Conn, wpcli *wpCLIProcess, GUID string, rows uint16, cols uint16, offset int64) error {
	var wg sync.WaitGroup

	log.Printf("resuming %s - rows: %d, cols: %d, offset: %d\n", GUID, rows, cols, offset)

	var err error
	remoteAddress := conn.RemoteAddr().String()
	connectionActive := true

	wpcli.padlock.Lock()
	if -1 == offset || offset > wpcli.BytesLogged {
		offset = wpcli.BytesLogged
	}
	wpcli.BytesStreamed[remoteAddress] = offset

	// Only set the window size if this client is the only one connected, otherwise the
	// original window size is maintained for the other client that is connected.
	if 1 == len(wpcli.BytesStreamed) {
		err = pty.Setsize(wpcli.Tty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
		if nil != err {
			log.Printf("attachWpCliCmdRemote: error performing window resize: %s\n", err.Error())
		} else {
			log.Printf("attachWpCliCmdRemote: set new window size: %dx%d\n", rows, cols)
		}
	}
	wpcli.padlock.Unlock()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		conn.Write([]byte("unable to reattach to the WP CLI processs"))
		conn.Close()
		return fmt.Errorf("attachWpCliCmdRemote: error reattaching to the WP CLI process: %s", err.Error())
	}

	err = watcher.Watch(wpcli.LogFileName)
	if err != nil {
		log.Printf("attachWpCliCmdRemote: error watching the logfile: %s", err.Error())
		conn.Write([]byte("unable to open the remote process log"))
		conn.Close()
		watcher.Close()
		return fmt.Errorf("attachWpCliCmdRemote: error watching the logfile: %s", err.Error())
	}

	wg.Add(1)
	go func() {
		var written, read int
		var buf []byte = make([]byte, 8192)

		readFile, err := os.OpenFile(wpcli.LogFileName, os.O_RDONLY, os.ModeCharDevice)
		if nil != err {
			log.Printf("attachWpCliCmdRemote: error opening the read file for the catchup stream: %s\n", err.Error())
			conn.Close()
			return
		}

		log.Printf("Seeking %s to %d for the catchup stream", wpcli.LogFileName, offset)
		readFile.Seek(offset, 0)

	Catchup_Loop:
		for {
			if !connectionActive {
				log.Println("attachWpCliCmdRemote: client connection is closed, exiting this catchup loop")
				break Catchup_Loop
			}

			read, err = readFile.Read(buf)
			if nil != err {
				if io.EOF != err {
					log.Printf("attachWpCliCmdRemote: error reading file for the catchup stream: %s\n", err.Error())
				}
				break Catchup_Loop
			}

			if 0 == read {
				log.Printf("attachWpCliCmdRemote: error reading file for the stream: %s\n", err.Error())
				break Catchup_Loop
			}


			written, _, err = connWriteUTF8(conn, buf[:read])
			if nil != err {
				log.Printf("attachWpCliCmdRemote catchup: error writing to client connection: %s\n", err.Error())
				readFile.Close()
				return
			}

			wpcli.padlock.Lock()
			wpcli.BytesStreamed[remoteAddress] = wpcli.BytesStreamed[remoteAddress] + int64(written)

			if wpcli.BytesStreamed[remoteAddress] == wpcli.BytesLogged {
				wpcli.padlock.Unlock()
				break Catchup_Loop
			} else {
				wpcli.padlock.Unlock()
			}
		}
		log.Printf("attachWpCliCmdRemote: Catchup_Loop finished ")

		// Used to monitor when the connection is disconnected or the CLI command finishes
		ticker := time.Tick(time.Duration(500 * time.Millisecond.Nanoseconds()))

	Watcher_Loop:
		for {
			select {
			case <-ticker:
				if !connectionActive {
					log.Println("attachWpCliCmdRemote: ticker: client connection is closed, exiting this watcher loop")
					break Watcher_Loop
				}

				if !wpcli.Running && wpcli.BytesStreamed[remoteAddress] >= wpcli.BytesLogged {
					log.Println("attachWpCliCmdRemote: WP CLI command finished and all data has been written, exiting this watcher loop")
					break Watcher_Loop
				}
			case ev := <-watcher.Event:
				if ev.IsDelete() {
					break Watcher_Loop
				}
				if !ev.IsModify() {
					continue
				}

				read, err = readFile.Read(buf)
				if 0 == read {
					continue
				}

				if !connectionActive || conn == nil {
					log.Println("attachWpCliCmdRemote: client connection is closed, exiting this watcher loop")
					break Watcher_Loop
				}

				written, _, err = connWriteUTF8(conn, buf[:read])
				if nil != err {
					log.Printf("attachWpCliCmdRemote: error writing to client connection: %s\n", err.Error())
					break Watcher_Loop
				}

				wpcli.padlock.Lock()
				wpcli.BytesStreamed[remoteAddress] += int64(written)
				wpcli.padlock.Unlock()

			case err := <-watcher.Error:
				log.Printf("attachWpCliCmdRemote: error scanning the logfile: %s", err.Error())
				break Watcher_Loop
			}
		}

		log.Printf("attachWpCliCmdRemote: Watcher_Loop finished ")

		log.Println("attachWpCliCmdRemote: closing watcher and readfile")
		watcher.Close()
		readFile.Close()

		log.Println("attachWpCliCmdRemote: closing connection at the end of the file read")

		wg.Done()
	}()

	go processShutdown(conn, wpcli)

	go func() {
		processTCPConnectionData(conn, wpcli)
		if conn != nil {
			conn.Close()
			conn = nil
			connectionActive = false
		}
	}()

	wg.Wait()

	if conn != nil {
		conn.Close()
	}

	log.Printf("attachWpCliCmdRemote: cleaning out %s\n", remoteAddress)

	return nil
}

func runWpCliCmdRemote(conn net.Conn, GUID string, rows uint16, cols uint16, wpCliCmdString string) error {
	cmdArgs := make([]string, 0)
	cmdArgs = append(cmdArgs, strings.Fields("--path="+remoteConfig.wpPath)...)

	cleanArgs, err := getCleanWpCliArgumentArray(wpCliCmdString)
	if nil != err {
		conn.Write([]byte("WP CLI command is invalid"))
		conn.Close()
		return errors.New(err.Error())
	}
	log.Printf("LOG CLI Arguments (%d elements): %s", len(cleanArgs), strings.Join(cleanArgs, ", "))

	cmdArgs = append(cmdArgs, cleanArgs...)

	cmd := exec.Command(remoteConfig.wpCLIPath, cmdArgs...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	log.Printf("launching %s - rows: %d, cols: %d, args: %s\n", GUID, rows, cols, strings.Join(cmdArgs, " "))

	logFileName := fmt.Sprintf("/tmp/wp-cli-%s", GUID)

	if _, err := os.Stat(logFileName); nil == err {
		log.Printf("runWpCliCmdRemote: Removing existing GUID logfile %s", logFileName)
		os.Remove(logFileName)
	}

	log.Printf("Creating the logfile %s", logFileName)
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE|os.O_SYNC, 0666)
	if nil != err {
		conn.Write([]byte("unable to launch the remote WP CLI process: " + err.Error()))
		conn.Close()
		return fmt.Errorf("runWpCliCmdRemote: error creating the WP CLI log file: %s", err.Error())
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		conn.Write([]byte("unable to launch the remote WP CLI process: " + err.Error()))
		logFile.Close()
		conn.Close()
		return fmt.Errorf("runWpCliCmdRemote: error launching the WP CLI log file watcher: %s", err.Error())
	}

	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if nil != err {
		conn.Write([]byte("unable to launch the remote WP CLI process."))
		logFile.Close()
		conn.Close()
		return fmt.Errorf("runWpCliCmdRemote: error setting the WP CLI tty window size: %s", err.Error())
	}
	defer func() { _ = tty.Close() }()

	remoteAddress := conn.RemoteAddr().String()

	padlock.Lock()
	wpcli := &wpCLIProcess{}
	wpcli.GUID = GUID
	wpcli.Cmd = cmd
	wpcli.BytesLogged = 0
	wpcli.BytesStreamed = make(map[string]int64)
	wpcli.BytesStreamed[remoteAddress] = 0
	wpcli.Tty = tty
	wpcli.LogFileName = logFileName
	wpcli.Running = true
	wpcli.padlock = &sync.Mutex{}
	gGUIDttys[GUID] = wpcli
	padlock.Unlock()

	prevState, err := terminal.MakeRaw(int(tty.Fd()))
	if nil != err {
		conn.Write([]byte("unable to initialize the remote WP CLI process."))
		conn.Close()
		logFile.Close()
		return fmt.Errorf("runWpCliCmdRemote: error initializing the WP CLI process: %s", err.Error())
	}
	defer func() { _ = terminal.Restore(int(tty.Fd()), prevState) }()

	readFile, err := os.OpenFile(logFileName, os.O_RDONLY, os.ModeCharDevice)
	if nil != err {
		conn.Close()
		logFile.Close()
		return fmt.Errorf("runWpCliCmdRemote: error opening the read file for the stream: %s", err.Error())
	}

	// logfile -> connection
	go func() {
		var written, read int
		var buf []byte = make([]byte, 8192)

		// Used to monitor when the connection is disconnected or the CLI command finishes
		ticker := time.Tick(time.Duration(500 * time.Millisecond.Nanoseconds()))

	Exit_Loop:
		for {
			select {
			case <-ticker:
				if nil == conn {
					log.Println("runWpCliCmdRemote ticker: client connection is closed, exiting this watcher loop" )
					break Exit_Loop
				}

				if (!wpcli.Running && wpcli.BytesStreamed[remoteAddress] >= wpcli.BytesLogged) {
					log.Println("runWpCliCmdRemote: WP CLI command finished and all data has been written, exiting this watcher loop")
					break Exit_Loop
				}

				// Command finished but there are remaining logs to be sent
				if !wpcli.Running {
					for {
						read, err = readFile.Read(buf)

						if io.EOF == err {
							log.Println("runWpCliCmdRemote: No more remaining logs")

							break Exit_Loop
						}

						written, _, err = connWriteUTF8(conn, buf[:read])

						wpcli.padlock.Lock()
						wpcli.BytesStreamed[remoteAddress] += int64(written)
						wpcli.padlock.Unlock()
					}
				}

			case ev := <-watcher.Event:
				if ev.IsDelete() {
					break Exit_Loop
				}
				if !ev.IsModify() {
					continue
				}

				read, err = readFile.Read(buf)
				if nil != err {
					if io.EOF != err {
						log.Printf("runWpCliCmdRemote: error reading the log file: %s\n", err.Error())
						break Exit_Loop
					}
					continue
				}
				if 0 == read {
					continue
				}

				if nil == conn {
					log.Println("runWpCliCmdRemote: client connection is closed, exiting this watcher loop")
					break Exit_Loop
				}

				written, _, err = connWriteUTF8(conn, buf[:read])

				wpcli.padlock.Lock()
				wpcli.BytesStreamed[remoteAddress] += int64(written)
				wpcli.padlock.Unlock()

				if nil != err {
					log.Printf("runWpCliCmdRemote: error writing to client connection: %s\n", err.Error())
					break Exit_Loop
				}

			case err := <-watcher.Error:
				log.Printf("runWpCliCmdRemote: error scanning the logfile %s: %s", logFileName, err.Error())
				break Exit_Loop
			}
		}

		log.Println("runWpCliCmdRemote: closing watcher and read file")
		watcher.Close()
		readFile.Close()
	}()

	err = watcher.Watch(logFileName)
	if err != nil {
		conn.Close()
		logFile.Close()
		readFile.Close()
		return err
	}

	// tty output -> logfile
	go func() {
		var written, read int
		var err error
		var buf []byte = make([]byte, 8192)

		for {
			padlock.Lock()
			running := wpcli.Running
			padlock.Unlock()

			if ! running {
				log.Printf("runWpCliCmdRemote: command already finished. Stop reading WP CLI tty output")
				break
			}

			if _, err = tty.Stat(); nil != err {
				log.Printf("runWpCliCmdRemote: tty closed. Command already finished. Stop reading WP CLI tty output")
				// This is because the command has been terminated
				break
			}

			read, err = tty.Read(buf)
			if nil != err {
				if io.EOF != err {
					log.Printf("runWpCliCmdRemote: error reading WP CLI tty output: %s\n", err.Error())
				}
				break
			}

			if 0 == read {
				continue
			}

			atomic.AddInt64(&wpcli.BytesLogged, int64(read))

			written, err = logFile.Write(buf[:read])
			if nil != err {
				log.Printf("runWpCliCmdRemote: error writing to logfle: %s\n", err.Error())
				break
			}
			if written != read {
				log.Printf("runWpCliCmdRemote: error writing to logfile, read %d and only wrote %d\n", read, written)
				break
			}
		}

		log.Println("closing logfile")
		logFile.Sync()
		logFile.Close()
	}()

	go processShutdown(conn, wpcli)

	go func() {
		processTCPConnectionData(conn, wpcli)
		if conn != nil {
			conn.Close()
			conn = nil
		}
	}()

	log.Printf("runWpCliCmdRemote: waiting command to finish")
	state, err := cmd.Process.Wait()
	if nil != err {
		log.Printf("runWpCliCmdRemote: error from the wp command: %s\n", err.Error())
	}

	log.Printf("runWpCliCmdRemote: comand finished: %s\n", GUID)

	if wpcli.Running {
		log.Println("runWpCliCmdRemote: marking the WP-CLI as finished")
		wpcli.padlock.Lock()
		wpcli.Running = false
		wpcli.padlock.Unlock()
	} else {
		log.Println("runWpCliCmdRemote: WP-CLI already finished running")
	}

	if !state.Exited() {
		log.Println("runWpCliCmdRemote: terminating the wp command")
		if nil != conn {
			conn.Write([]byte("Command has been terminated\n"))
		}
		cmd.Process.Kill()
	}

	usage := state.SysUsage().(*syscall.Rusage)
	log.Printf("GUID %s : max rss: %0.0f KB : user time %0.2f sec : sys time %0.2f sec",
		GUID,
		float64(usage.Maxrss)/1024,
		float64(usage.Utime.Sec)+float64(usage.Utime.Usec)/1e6,
		float64(usage.Stime.Sec)+float64(usage.Stime.Usec)/1e6)

	for {
		if (!wpcli.Running && wpcli.BytesStreamed[remoteAddress] >= wpcli.BytesLogged) || nil == conn {
			break
		}
		log.Printf("runWpCliCmdRemote: waiting for remaining bytes to be sent to a client: at %d - have %d\n", wpcli.BytesStreamed[remoteAddress], wpcli.BytesLogged)
		time.Sleep(time.Duration(200 * time.Millisecond.Nanoseconds()))
	}

	if nil != conn {
		log.Println("runWpCliCmdRemote: closing the connection at the end")
		conn.Close()
	}

	wpcli.padlock.Lock()
	log.Printf("cleaning out %s\n", remoteAddress)
	if 0 == len(wpcli.BytesStreamed) {
		log.Printf("cleaning out %s\n", GUID)
		wpcli.Running = false
		wpcli.padlock.Unlock()
		wpcli.padlock = nil
		padlock.Lock()
		delete(gGUIDttys, GUID)
		padlock.Unlock()
	} else {
		wpcli.padlock.Unlock()
	}

	return nil
}

func streamLogs(conn net.Conn, GUID string) {
	var err error
	var logFileName string

	log.Printf("preparing to send the log file for GUID %s\n", GUID)

	logFileName = fmt.Sprintf("/tmp/wp-cli-%s", GUID)

	if _, err := os.Stat(logFileName); nil != err {
		conn.Write([]byte(fmt.Sprintf("The WP CLI log file for GUID %s does not exist\n", GUID)))
		log.Printf("The logfile %s does not exist\n", logFileName)
		conn.Close()
		return
	}

	logFile, err := os.OpenFile(logFileName, os.O_RDONLY|os.O_SYNC, 0666)
	if nil != err {
		conn.Write([]byte("error reading the WP CLI log file\n"))
		log.Printf("error reading the WP CLI log file: %s\n", err.Error())
		conn.Close()
		return
	}

	var buf []byte = make([]byte, 8192)
	var read int
	for {
		read, err = logFile.Read(buf)
		if io.EOF == err {
			break
		}
		conn.Write(buf[:read])
	}
	conn.Close()
	logFile.Close()
	log.Printf("log file for GUID %s sent\n", GUID)
}

/*
Splits a string into an array based on whitespace except when that whitepace is inside double qoutes or escaped quotes
*/
func tokenizeString(rawString string) []string {
	quoted := false
	var prevRune rune
	tokenized := strings.FieldsFunc(rawString, func(r rune) bool {
		//Tokenizing on double quotes EXCEPT when preceded by the escape char
		if r == '"' && prevRune != '\\' {
			quoted = !quoted
		}
		prevRune = r
		return !quoted && r == ' '
	})
	out := strings.Join(tokenized, ", ")
	log.Printf("LOG: %s", out)
	return tokenized
}

func isJSON(str string) bool {
	return json.Valid([]byte(str))
}

func isJSONObject(str string) bool {
	trimmedStr := strings.TrimSpace(str)
	if !strings.HasPrefix(trimmedStr, "{") || !strings.HasSuffix(trimmedStr, "}") {
		return false
	}
	return isJSON(str)
}
