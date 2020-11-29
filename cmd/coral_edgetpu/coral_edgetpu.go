package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	edgetpumanager "github.com/CyberAgent/sia-ake-accelerators/pkg/edgetpu/coral"
	"github.com/golang/glog"
)

const (
	pluginSocketPrefix = "coral-edgetpu"
	devDirectory       = "/dev"
)

var (
	pluginMountPath = flag.String("plugin-directory", "/device-plugin", "The directory path to create plugin socket")
)

func main() {
	flag.Parse()
	glog.Infoln("Coral EdgeTPU plugin started")
	//cem := edgetpumanager.NewCoralEdgeTPUManager(devDirectory, fmt.Sprintf("%s-%d.sock", pluginSocketPrefix, time.Now().Unix()))
	cem := edgetpumanager.NewCoralEdgeTPUManager(devDirectory, fmt.Sprintf("%s.sock", pluginSocketPrefix))

	for {
		err := cem.Start()
		if err == nil {
			break
		}
		// Use non-default level to avoid log spam.
		glog.V(3).Infof("Failed to initialize device-plugin: %v", err)
		time.Sleep(5 * time.Second)
	}

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
		<-sigs
		cem.Stop()
	}()

	cem.Serve(*pluginMountPath)
}
