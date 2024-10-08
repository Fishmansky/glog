package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type Glog interface {
	Run()
}

type GlogConnectedClient struct {
	c    net.Conn
	name string
	dir  string
}

type GlogServerConfig struct {
	name      string
	mode      string
	addr      string
	logFiles  map[string]string `toml:"logfiles"`
	debug     bool
	serverLog *os.File
}

type GlogServer struct {
	GlogServerConfig
}

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
}

type NullWriter struct{}

func (NullWriter) Write([]byte) (int, error) { return 0, nil }

func SetConfigDefaults() {
	viper.SetDefault("logdir", "/var/log/glog")
	viper.SetDefault("debuglog", "glog.debug")
	viper.AddConfigPath("/etc/glog")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
}
func ReadGlogServerConfig() GlogServerConfig {
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
	return GlogServerConfig{
		name:  name,
		mode:  mode,
		addr:  addr,
		debug: debug,
	}
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
func NewGlogServer() *GlogServer {
	conf := ReadGlogServerConfig()
	logdir := viper.GetString("logdir")
	debuglog := viper.GetString("debuglog")
	f, err := os.OpenFile(logdir+"/"+debuglog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	conf.serverLog = f
	var logLevel = new(slog.LevelVar)
	logLevel.Set(slog.LevelInfo)
	logger := slog.NewTextHandler(f, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(logger))
	if conf.debug {
		//slog.SetLogLoggerLevel(slog.LevelDebug)
		logLevel.Set(slog.LevelDebug)
	}
	return &GlogServer{
		GlogServerConfig: conf,
	}

}

func ValidateNewClient(c net.Conn) (*GlogConnectedClient, error) {
	buf := make([]byte, 1024)
	n, err := c.Read(buf)
	if err != nil {
		return nil, err
	}
	str := string(buf[:n])
	splitted := strings.Split(str, ":")
	if splitted[0] != "new-client" {
		return nil, fmt.Errorf("New client connection request malformed.\n")
	}
	slog.Debug("New connection request from client", "client", splitted[1])
	if _, err := c.Write([]byte(fmt.Sprintf("confirm-client:%s", splitted[1]))); err != nil {
		return nil, err
	}
	slog.Debug("Connection confirmation sent to client", "client", splitted[1])
	n, err = c.Read(buf)
	if err != nil {
		return nil, err
	}
	str = string(buf[:n])
	splitted = strings.Split(str, ":")
	if splitted[0] != "confirmed-client" {
		return nil, fmt.Errorf("Confirmed client connection request malformed.\n")
	}
	slog.Debug("Connection confirmation received from client", "client", splitted[1])
	if _, err := c.Write([]byte(fmt.Sprintf("ok-client:%s", splitted[1]))); err != nil {
		return nil, err
	}
	slog.Debug("Connection acknowledgement sent to client", "client", splitted[1])
	return &GlogConnectedClient{c: c, name: splitted[1]}, nil
}

func (g *GlogServer) ProcessReceivedData(ctx context.Context, gc *GlogConnectedClient) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// receive data
			buf := make([]byte, 1024)
			n, err := gc.c.Read(buf)
			if err != nil {
				if err == io.EOF {
					slog.Info("Client disconnected", "client", gc.name)
					return
				}
				slog.Error(err.Error())
				os.Exit(1)
			}
			i := 0
			for i < len(buf) {
				if buf[i] == 58 {
					i++
					break
				}
				i++
			}
			logFileName := string(buf[:i-1])
			data := buf[i:n]
			logPath := gc.dir + "/" + logFileName
			// save data
			f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				slog.Error("Error opening log file", "file", logPath, "error", err)
				os.Exit(1)
			}
			if _, err := f.Write(data); err != nil {
				slog.Error("Error writing log file", "file", logPath, "error", err)
				os.Exit(1)
			}
			if err := f.Close(); err != nil {
				slog.Error("Error closing log file", "file", logPath, "error", err)
				os.Exit(1)
			}
			slog.Debug("File updated", "file", logPath)
		}
	}
}

func (g *GlogServer) HandleConnection(ctx context.Context, c net.Conn) {
	// set connection with new client
	gc, err := ValidateNewClient(c)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	// prepare client log directory
	ld := viper.GetString("logdir")
	gc.dir = ld + "/" + gc.name
	_, err = os.Stat(gc.dir)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.Mkdir(gc.dir, 0755)
			if err != nil {
				slog.Error("Error creating log directory for client", "client", gc.name, "directory", gc.dir, "error", err)
				os.Exit(1)
			}
		} else {
			slog.Error(err.Error())
			os.Exit(1)
		}
	}
	// start processing received data
	g.ProcessReceivedData(ctx, gc)
}

func (g *GlogServer) Run() {
	l, err := net.Listen("tcp", g.addr)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	defer l.Close()
	defer g.serverLog.Close()
	slog.Info("Server started", "Listening address", g.addr)
	ctx := context.TODO()

	for {
		conn, err := l.Accept()
		if err != nil {
			slog.Error(err.Error())
			os.Exit(1)
		}
		go func() {
			g.HandleConnection(ctx, conn)
		}()
	}
}

func NewGlogClient() *GlogClient {
	conf := ReadGlogClientConfig()
	logdir := viper.GetString("logdir")
	debuglog := viper.GetString("debuglog")
	f, err := os.OpenFile(logdir+"/"+debuglog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

func (g *GlogClient) GetLogsChan() <-chan []byte {
	logschan := make(chan []byte)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	for logFile := range g.logFiles {
		go func(l string) {
			defer watcher.Close()
			src, err := os.Open(g.logFiles[l])
			if err != nil {
				slog.Error("Error opening file to read", "file", g.logFiles[l], "error", err)
				os.Exit(1)
			}
			defer src.Close()
			err = watcher.Add(g.logFiles[l])
			if err != nil {
				slog.Error("Error adding file to watcher", "file", g.logFiles[l], "error", err)
				os.Exit(1)
			}
			slog.Debug("Watching file", "file", g.logFiles[l])
			_, err = src.Seek(0, os.SEEK_END)
			if err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
			reader := bufio.NewReader(src)
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						return
					}
					if event.Op == fsnotify.Write {
						slog.Debug("Watched file changed", "file", g.logFiles[l])
						var newData string
						for {
							line, err := reader.ReadString('\n')
							if err != nil {
								break
							}
							newData += line
						}
						logschan <- []byte(l + ":" + newData)
					}
				case err, ok := <-watcher.Errors:
					if !ok {
						return
					}
					slog.Error(err.Error())
				}
			}
		}(logFile)
	}
	return logschan
}
func (g *GlogClient) ConnectToServer(c net.Conn) {
	// send connection request
	data := []byte(fmt.Sprintf("new-client:%s", g.name))
	if _, err := c.Write(data); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	// read first response
	buf := make([]byte, 1024)
	n, err := c.Read(buf)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	str := string(buf[:n])
	if str != "confirm-client:"+g.name {
		slog.Error("Server confirmation request malformed!")
		os.Exit(1)
	}
	// send confirmation request
	data = []byte(fmt.Sprintf("confirmed-client:%s", g.name))
	if _, err := c.Write(data); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	// read second response
	buf = make([]byte, 1024)
	n, err = c.Read(buf)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	str = string(buf[:n])
	if str != "ok-client:"+g.name {
		slog.Error("Server settle request malformed!")
		os.Exit(1)
	}
	slog.Info("Client connected", "Server address", g.addr)
}

func (g *GlogClient) Run() {
	// connect to server
	var d net.Dialer
	ctxInit, cancelInit := context.WithTimeout(context.Background(), time.Second*15)
	d.KeepAlive = 0 // default interval is used
	defer cancelInit()

	conn, err := d.DialContext(ctxInit, "tcp", g.addr)
	if err != nil {
		slog.Error("Failed to connect to glog server: %v", "errormsg", err)
		os.Exit(1)
	}
	defer conn.Close()

	g.ConnectToServer(conn)

	logsChan := g.GetLogsChan()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			g.clientLog.Close()
			slog.Info("Glog finished!")
			return
		case d := <-logsChan:
			slog.Debug("Sending log to server...")
			if _, err := conn.Write(d); err != nil {
				slog.Error(err.Error())
				os.Exit(1)
			}
			slog.Debug("Log sent!")
		}
	}
}

func NewGlog() Glog {
	SetConfigDefaults()
	viper.ReadInConfig()
	mode := viper.GetString("mode")
	if mode == "" {
		slog.Error("mode variable not set in config file!")
		os.Exit(1)
	}
	if mode == "server" {
		return NewGlogServer()
	} else if mode == "client" {
		return NewGlogClient()
	}
	return nil
}

func main() {
	g := NewGlog()
	g.Run()
}
