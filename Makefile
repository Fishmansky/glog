build:
	go build -o bin/glog internal/main.go
help:
	@echo "++ GLOG ++"
	@echo "Available options:"
	@echo ""
	@echo "make build - build glog"
	
.DEFAULT_GOAL:= help
