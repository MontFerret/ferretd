package grpc

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

const (
	apiMajor uint32 = 1
	apiMinor uint32 = 2
)

type daemonService struct {
	daemonv1.UnimplementedDaemonServiceServer
	info     *daemonv1.ServerInfo
	shutdown func()
	once     sync.Once
}

func newDaemonService(version, instanceID string, shutdown func()) (*daemonService, error) {
	if shutdown == nil {
		return nil, errNilShutdown
	}

	result := &daemonService{shutdown: shutdown}
	result.info = &daemonv1.ServerInfo{
		Version:    version,
		InstanceId: instanceID,
		ApiVersion: result.apiVersion(),
	}

	return result, nil
}

func (s *daemonService) GetInfo(
	_ context.Context,
	request *daemonv1.GetInfoRequest,
) (*daemonv1.GetInfoResponse, error) {
	if request == nil || request.ClientApi == nil {
		return nil, status.Error(codes.InvalidArgument, "client API version is required")
	}

	if request.ClientApi.Major != apiMajor {
		result := status.New(codes.FailedPrecondition, "incompatible daemon API major version")
		withDetails, err := result.WithDetails(&daemonv1.ApiCompatibilityError{
			ClientApi: request.ClientApi,
			ServerApi: s.apiVersion(),
		})
		if err != nil {
			return nil, result.Err()
		}

		return nil, withDetails.Err()
	}

	return &daemonv1.GetInfoResponse{
		ServerInfo: s.info,
	}, nil
}

func (s *daemonService) Shutdown(
	context.Context,
	*daemonv1.ShutdownRequest,
) (*daemonv1.ShutdownResponse, error) {
	s.once.Do(s.shutdown)

	return &daemonv1.ShutdownResponse{}, nil
}

func (s *daemonService) apiVersion() *daemonv1.ApiVersion {
	return &daemonv1.ApiVersion{Major: apiMajor, Minor: apiMinor}
}
