package client

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
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
	conn net.Conn
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
					if event.Op == fsnotify.Remove || event.Op == fsnotify.Rename {
						tries := 0
						slog.Debug("Watched file disapeared - trying to rewatch", "file", g.logFiles[l])
						if !fileExists(g.logFiles[l]) {
							if tries > 50 {
								slog.Error("Files rewatching error - 50 retries exceeded")
								return
							}
							time.Sleep(time.Millisecond * 50)
							tries++
						}
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
						reader = bufio.NewReader(src)
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
func (g *GlogClient) ServerHandshake() {
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
	g.ServerHandshake()

	slog.Info("Client started")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)
	defer cancel()
	go g.StreamLogs(ctx)
	<-ctx.Done()
	slog.Info("Termination signal received - client gracefully shutting down...")
}
