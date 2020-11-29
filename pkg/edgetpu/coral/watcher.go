package coral

import (
	"github.com/golang/glog"
	"golang.org/x/net/context"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	watcherapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

type watcherServiceV1 struct {
	cem *coralEdgeTPUManager
}

func (s *watcherServiceV1) GetInfo(ctx context.Context, req *watcherapi.InfoRequest) (*watcherapi.PluginInfo, error) {
	glog.Info("GetInfo() called by kubelet")
	return &watcherapi.PluginInfo{
		Type:              watcherapi.DevicePlugin,
		Name:              resourceName,
		Endpoint:          s.cem.endpoint,
		SupportedVersions: []string{pluginapi.Version},
	}, nil
}

func (s *watcherServiceV1) NotifyRegistrationStatus(ctx context.Context, status *watcherapi.RegistrationStatus) (*watcherapi.RegistrationStatusResponse, error) {
	glog.Info("NotifyRegistrationStatus() called by kubelet")
	if s.cem.registrationStatus != nil {
		s.cem.registrationStatus <- *status
	}
	glog.Infof("Registration status: %v", status)
	if !status.PluginRegistered {
		glog.Error("Registration failed: ", status.Error)
	}
	return &watcherapi.RegistrationStatusResponse{}, nil
}
