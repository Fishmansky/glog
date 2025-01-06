package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/viper"
)

type GlogServer struct {
	GlogServerConfig
	listener net.Listener
}

type GlogServerConfig struct {
	name      string
	mode      string
	addr      string
	logFiles  map[string]string `toml:"logfiles"`
	debug     bool
	serverLog *os.File
}

type GlogConnectedClient struct {
	c    net.Conn
	name string
	dir  string
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
func New() *GlogServer {
	conf := ReadGlogServerConfig()
	logdir := viper.GetString("logdir")
	mainlog := viper.GetString("mainlog")
	f, err := os.OpenFile(logdir+"/"+mainlog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

func (g *GlogServer) PrepareClientDir(gc *GlogConnectedClient) error {
	gc.dir = viper.GetString("logdir") + "/" + gc.name
	return os.MkdirAll(gc.dir, 0755)
}

func (g *GlogServer) HandleConnection(ctx context.Context, c net.Conn) {
	// set connection with new client
	gc, err := ValidateNewClient(c)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	// prepare client log directory
	err = g.PrepareClientDir(gc)
	if err != nil {
		slog.Error("Error creating log directory for client", "client", gc.name, "directory", gc.dir, "error", err)
		os.Exit(1)
	}
	// start processing received data
	g.ProcessReceivedData(ctx, gc)
}

func (g *GlogServer) ListenForConnections(ctx context.Context) {
	for {
		conn, err := g.listener.Accept()
		if err != nil {
			slog.Error(err.Error())
			os.Exit(1)
		}
		go g.HandleConnection(ctx, conn)
	}
}

func (g *GlogServer) Run() {
	var err error
	g.listener, err = net.Listen("tcp", g.addr)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	defer g.listener.Close()
	defer g.serverLog.Close()
	slog.Info("Server started", "Listening address", g.addr)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)
	defer cancel()
	go g.ListenForConnections(ctx)
	<-ctx.Done()
	slog.Info("Termination signal received - server gracefully shutting down now.")
}
