build:
	go build -o bin/glog cmd/main.go
help:
	@echo "++ GLOG ++"
	@echo "Available options:"
	@echo ""
	@echo "make build - build glog"
	
.DEFAULT_GOAL:= help
