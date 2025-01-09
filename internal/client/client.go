package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type GlogClientConfig struct {
	name      string
	mode      string
	addr      string
	logFiles  map[string]string `toml:"logfiles"`
	debug     bool
	clientLog *os.File
}

type GlogClient struct {
	GlogClientConfig
	conn    net.Conn
	logsMap map[string]uint8
}

func ReadGlogClientConfig() GlogClientConfig {
	name := viper.GetString("name")
	if name == "" {
		log.Fatal("name variable not set in config file!")
	}
	mode := viper.GetString("mode")
	if mode == "" {
		log.Fatal("mode variable not set in config file!")
	}
	addr := viper.GetString("addr")
	if addr == "" {
		log.Fatal("addr variable not set in config file!")
	}
	debug := viper.GetBool("debug")
	if mode == "" {
		log.Fatal("debug variable not set in config file!")
	}
	logFiles := viper.GetStringMapString("logfiles")
	if mode == "" {
		log.Fatal("debug variable not set in config file!")
	}
	return GlogClientConfig{
		name:     name,
		mode:     mode,
		addr:     addr,
		debug:    debug,
		logFiles: logFiles,
	}
}

func New() *GlogClient {
	conf := ReadGlogClientConfig()
	logdir := viper.GetString("logdir")
	mainlog := viper.GetString("mainlog")
	f, err := os.OpenFile(logdir+"/"+mainlog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	conf.clientLog = f
	var logLevel = new(slog.LevelVar)
	logLevel.Set(slog.LevelInfo)
	logger := slog.NewTextHandler(f, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(logger))
	if conf.debug {
		logLevel.Set(slog.LevelDebug)
	}
	return &GlogClient{
		GlogClientConfig: conf,
	}

}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func WatchFile(w *fsnotify.Watcher, src *os.File, filename string) *bufio.Reader {
	err := w.Add(filename)
	if err != nil {
		slog.Error("Error adding file to watcher", "file", filename, "error", err)
		os.Exit(1)
	}
	slog.Debug("Watching file", "file", filename)
	_, err = src.Seek(0, io.SeekEnd)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	return bufio.NewReader(src)
}

func StreamData(reader *bufio.Reader, logID uint8, c chan []byte) {
	var newData string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		newData += line
	}
	slog.Debug("new data", "data", newData)
	c <- []byte(fmt.Sprintf("!:%d:%s", logID, newData))
}

func (g *GlogClient) GetLogsChan() <-chan []byte {
	logschan := make(chan []byte)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	for l := range g.logFiles {
		go func(logName string) {
			logID := g.logsMap[logName]
			defer watcher.Close()
			src, err := os.Open(g.logFiles[logName])
			if err != nil {
				slog.Error("Error opening file to read", "file", g.logFiles[logName], "error", err)
				os.Exit(1)
			}
			defer src.Close()
			reader := WatchFile(watcher, src, g.logFiles[logName])
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						return
					}
					if event.Op == fsnotify.Remove || event.Op == fsnotify.Rename {
						tries := 0
						slog.Debug("Watched file disapeared - trying to rewatch", "file", g.logFiles[logName])
						if !fileExists(g.logFiles[logName]) {
							if tries > 50 {
								slog.Error("Files rewatching error - 50 retries exceeded")
								return
							}
							time.Sleep(time.Millisecond * 50)
							tries++
						}
						src, err := os.Open(g.logFiles[logName])
						if err != nil {
							slog.Error("Error opening file to read", "file", g.logFiles[logName], "error", err)
							os.Exit(1)
						}
						defer src.Close()
						reader = WatchFile(watcher, src, g.logFiles[logName])
					}
					if event.Op == fsnotify.Write {
						slog.Debug("Watched file changed", "file", g.logFiles[logName])
						StreamData(reader, logID, logschan)
					}
				case err, ok := <-watcher.Errors:
					if !ok {
						return
					}
					slog.Error(err.Error())
				}
			}
		}(l)
	}
	return logschan
}
func (g *GlogClient) ServerHandshake() {
	m := make(map[string]uint8)
	// send connection request
	data := []byte(fmt.Sprintf("new-client:%s", g.name))
	if _, err := g.conn.Write(data); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	// read first response
	buf := make([]byte, 1024)
	n, err := g.conn.Read(buf)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	str := string(buf[:n])
	if str != "confirm-client:"+g.name {
		slog.Error("Server confirmation request malformed!")
		os.Exit(1)
	}
	// send each log name to server to receive id assigned to this number
	for k := range g.logFiles {
		data := []byte(fmt.Sprintf("new-log:%s", k))
		if _, err := g.conn.Write(data); err != nil {
			slog.Error(err.Error())
			os.Exit(1)
		}
		// read response with log id
		buf := make([]byte, 1024)
		n, err := g.conn.Read(buf)
		if err != nil {
			slog.Error(err.Error())
			os.Exit(1)
		}
		response := strings.Split(string(buf[:n]), ":")
		cmd := response[0]
		if cmd != "new-log-id" {
			slog.Error("Server confirmation request malformed!")
			os.Exit(1)
		}
		idStr := response[1]
		id, err := strconv.Atoi(idStr)
		if err != nil {
			slog.Error("Log id conversion error", "msg", err.Error())
			os.Exit(1)
		}
		m[k] = uint8(id)
	}
	g.logsMap = m
	slog.Debug("Log files map created", "map", g.logsMap)
	// send confirmation request
	data = []byte(fmt.Sprintf("confirmed-client:%s", g.name))
	if _, err := g.conn.Write(data); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	// read second response
	buf = make([]byte, 1024)
	n, err = g.conn.Read(buf)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	str = string(buf[:n])
	if str != "ok-client:"+g.name {
		slog.Error("Server settle request malformed!")
		os.Exit(1)
	}
	slog.Info("Connection with server established", "Server address", g.addr)
}

func (g *GlogClient) ConnectToServer() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	var d net.Dialer
	d.KeepAlive = 0 // default interval is used
	var err error
	g.conn, err = d.DialContext(ctx, "tcp", g.addr)
	if err != nil {
		slog.Error("Failed to connect to glog server", "error", err)
		os.Exit(1)
	}
	g.ServerHandshake()
}

func (g *GlogClient) StreamLogs(ctx context.Context) {
	logsChan := g.GetLogsChan()
	for {
		select {
		case <-ctx.Done():
			g.clientLog.Close()
			g.conn.Close()
			slog.Info("Glog finished!")
			return
		case d := <-logsChan:
			slog.Debug("Sending log to server...")
			if _, err := g.conn.Write(d); err != nil {
				slog.Error("Error sending log to server", "error", err)
				os.Exit(1)
			}
			slog.Debug("Log sent!")
		}
	}
}

func (g *GlogClient) Run() {
	g.ConnectToServer()
	slog.Info("Client started")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)
	defer cancel()
	go g.StreamLogs(ctx)
	<-ctx.Done()
	slog.Info("Termination signal received - client gracefully shutting down...")
}
