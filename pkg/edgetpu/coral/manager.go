package coral

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"google.golang.org/grpc"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	watcherapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

const (
	edgeTPUDeviceRE           = `^edgetpu[0-9]+$`
	apexDeviceRE              = `^apex_[0-9]+$`
	edgeTPUCheckInterval      = 10 * time.Second
	pluginSocketCheckInterval = 1 * time.Second
)

var (
	resourceName = "coral.ai/edgetpu"
)

type coralEdgeTPUManager struct {
	devDirectory string
	devices      map[string]pluginapi.Device
	devicesMutex sync.Mutex
	socket       string

	stop           chan bool
	wg             sync.WaitGroup
	devicesUpdated chan bool

	grpcServer *grpc.Server

	registrationStatus chan watcherapi.RegistrationStatus
	endpoint           string
}

func NewCoralEdgeTPUManager(devDirectory, socket string) *coralEdgeTPUManager {
	return &coralEdgeTPUManager{
		devDirectory:       devDirectory,
		devices:            make(map[string]pluginapi.Device),
		socket:             socket,
		stop:               make(chan bool),
		devicesUpdated:     make(chan bool),
		registrationStatus: make(chan watcherapi.RegistrationStatus),
	}
}

func (cem *coralEdgeTPUManager) discoverEdgeTPUs() (int, error) {
	var devices []string
	foundNew := 0
	tpuRe := regexp.MustCompile(edgeTPUDeviceRE)
	apexRe := regexp.MustCompile(apexDeviceRE)

	glog.V(3).Infof("Searching for devices...")
	files, err := ioutil.ReadDir(cem.devDirectory)
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}

		// Discover PCIe (Gasket/Apex) model.
		if apexRe.MatchString(f.Name()) && f.Mode()&os.ModeSymlink == 0 {
			glog.V(3).Infof("Found Apex device: %q\n", f.Name())
			devices = append(devices, f.Name())
		}

		// Discover USB model.
		if tpuRe.MatchString(f.Name()) && f.Mode()&os.ModeSymlink == os.ModeSymlink {
			path, err := filepath.EvalSymlinks(fmt.Sprintf("%s/%s", cem.devDirectory, f.Name()))
			if err != nil {
				return 0, err
			}
			if !strings.HasPrefix(path, cem.devDirectory) {
				continue
			}
			glog.V(3).Infof("Found USB device: %q (%q)\n", f.Name(), path)
			devices = append(devices, strings.TrimPrefix(path, cem.devDirectory))
		}
	}

	for _, dev := range devices {
		if _, exists := cem.devices[dev]; !exists {
			cem.setDeviceHealth(dev, pluginapi.Unhealthy)
			foundNew += 1
		}
	}

	return foundNew, nil
}

func (cem *coralEdgeTPUManager) updateDeviceHealth(name string) bool {
	return cem.setDeviceHealth(name, cem.getDeviceHealth(name))
}

func (cem *coralEdgeTPUManager) setDeviceHealth(name, health string) (changed bool) {
	cem.devicesMutex.Lock()
	if dev, ok := cem.devices[name]; ok {
		glog.Infof("Updating health for %s: orig=%s, new=%s", name, dev.Health, health)
		changed = dev.Health != health
	}
	cem.devices[name] = pluginapi.Device{ID: name, Health: health}
	cem.devicesMutex.Unlock()

	return
}

func (cem *coralEdgeTPUManager) getDeviceHealth(name string) string {
	if _, err := os.Stat(fmt.Sprintf("%s/%s", cem.devDirectory, name)); err != nil {
		glog.Errorf("Detected unhealthy device: %s", name)
		return pluginapi.Unhealthy
	}

	// TODO: Return Unhealthy if operating the frequency is reduced for thermal issues.
	//       https://coral.ai/docs/m2/get-started/#operating-frequency-and-thermal-settings
	return pluginapi.Healthy
}

func (cem *coralEdgeTPUManager) Start() error {
	if err := cem.cleanup(); err != nil {
		return err
	}

	if _, err := cem.discoverEdgeTPUs(); err != nil {
		return err
	}

	cem.updateDevicesHealth()
	return nil
}

func (cem *coralEdgeTPUManager) updateDevicesHealth() (changed bool) {
	devices := cem.devices // Avoid mutex.

	for id, _ := range devices {
		_changed := cem.updateDeviceHealth(id)
		if _changed {
			changed = true
		}
	}
	return
}

func (cem *coralEdgeTPUManager) Serve(pluginMountPath string) error {
	cem.endpoint = path.Join(pluginMountPath, cem.socket)

	glog.Infof("Starting device-plugin server at %s\n", cem.endpoint)
	lis, err := net.Listen("unix", cem.endpoint)
	if err != nil {
		glog.Fatalf("Failed to create socket: %v", err)
	}
	cem.grpcServer = grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(cem.grpcServer, &pluginServiceV1Beta1{cem: cem})
	watcherapi.RegisterRegistrationServer(cem.grpcServer, &watcherServiceV1{cem: cem})

	cem.wg.Add(1)

	go func() {
		defer cem.wg.Done()
		if err := cem.grpcServer.Serve(lis); err != nil {
			glog.Errorf("Failed to start device-plugin server: %v", err)
		}
	}()

	// Make sure we are ready to handle gRPC connections.
	_, conn, err := testPluginServiceV1Beta1(cem.endpoint)
	if err != nil {
		glog.Errorf("Self connection test failed: %v", err)
		return err
	}
	glog.Info("Self connection test passed.")
	conn.Close()
	glog.Infof("Started device-plugin server.")

	kubeletCheck := time.NewTicker(5 * time.Second)

waitKubelet:
	for {
		select {
		case <-cem.stop:
			glog.Info("Stopping device-plugin server...")
			cem.Stop()
			return nil
		case status := <-cem.registrationStatus:
			// TODO: Check status.PluginRegistered.
			glog.Infof("Notified registration by kubelet: %v", status)
			kubeletCheck.Stop()
			break waitKubelet
		case <-kubeletCheck.C:
			glog.Info("Waiting for kubelet to finish registration.")
		}
	}

	glog.Info("Force-triggered devicesUpdated event for the 1st time.")
	cem.devicesUpdated <- true

	edgeTPUCheck := time.NewTicker(edgeTPUCheckInterval)
	pluginSocketCheck := time.NewTicker(pluginSocketCheckInterval)
	defer edgeTPUCheck.Stop()
	defer pluginSocketCheck.Stop()

	for {
		select {
		case <-cem.stop:
			glog.Info("Stopping device-plugin server...")
			cem.Stop()
			return nil
		case <-pluginSocketCheck.C:
			if _, err := os.Lstat(cem.endpoint); err != nil {
				glog.Errorln("Failed to check existance of socket: %v", err)
				cem.stop <- true
			}
		case <-edgeTPUCheck.C:
			foundNew, err := cem.discoverEdgeTPUs()
			if err != nil {
				glog.Errorln("Failed to discover devices: %v", err)
				cem.stop <- true
			}
			healthChanged := cem.updateDevicesHealth()

			glog.Infof("fn=%d, hc=%t", foundNew, healthChanged)

			if foundNew > 0 || healthChanged {
				cem.devicesUpdated <- true
			}
		}
	}

	return nil
}

func (cem *coralEdgeTPUManager) Stop() error {
	if cem.grpcServer == nil {
		return nil
	}
	cem.grpcServer.Stop()
	cem.wg.Wait()
	cem.grpcServer = nil
	close(cem.stop)

	return cem.cleanup()
}

func (cem *coralEdgeTPUManager) cleanup() error {
	if err := os.Remove(cem.socket); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
