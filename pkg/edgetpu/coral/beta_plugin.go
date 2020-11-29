package coral

import (
	"fmt"
	"net"
	"path"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type pluginServiceV1Beta1 struct {
	cem *coralEdgeTPUManager
}

func (s *pluginServiceV1Beta1) GetDevicePluginOptions(ctx context.Context, e *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

func (s *pluginServiceV1Beta1) ListAndWatch(emtpy *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	glog.Infoln("device-plugin: ListAndWatch start")
	for {
		select {
		case <-s.cem.stop:
			return nil
		case <-s.cem.devicesUpdated:
			resp := new(pluginapi.ListAndWatchResponse)
			for _, dev := range s.cem.devices {
				resp.Devices = append(resp.Devices, &pluginapi.Device{ID: dev.ID, Health: dev.Health})
			}
			glog.Infof("ListAndWatch: send devices %v\n", resp)
			if err := stream.Send(resp); err != nil {
				glog.Errorf("device-plugin: cannot update device states: %v\n", err)
				s.cem.stop <- true
				return err
			}
		}
	}
}

func (s *pluginServiceV1Beta1) Allocate(ctx context.Context, requests *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resps := new(pluginapi.AllocateResponse)
	for _, rqt := range requests.ContainerRequests {
		resp := new(pluginapi.ContainerAllocateResponse)
		// Add all requested devices to Allocate Response
		for _, id := range rqt.DevicesIDs {
			dev, ok := s.cem.devices[id]
			if !ok {
				return nil, fmt.Errorf("invalid allocation request with non-existing device %s", id)
			}
			if dev.Health != pluginapi.Healthy {
				return nil, fmt.Errorf("invalid allocation request with unhealthy device %s", id)
			}
			resp.Devices = append(resp.Devices, &pluginapi.DeviceSpec{
				HostPath:      path.Join(s.cem.devDirectory, id),
				ContainerPath: path.Join(s.cem.devDirectory, id),
				Permissions:   "mrw",
			})
		}
		resps.ContainerResponses = append(resps.ContainerResponses, resp)
	}
	return resps, nil
}

func (s *pluginServiceV1Beta1) PreStartContainer(ctx context.Context, r *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	glog.Errorf("device-plugin: PreStart should NOT be called for AKE Coral EdgeTPU device plugin\n")
	return &pluginapi.PreStartContainerResponse{}, nil
}

func testPluginServiceV1Beta1(unixSocketPath string) (pluginapi.DevicePluginClient, *grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := grpc.DialContext(ctx, unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial device-plugin: %v", err)
	}

	return pluginapi.NewDevicePluginClient(c), c, nil
}
