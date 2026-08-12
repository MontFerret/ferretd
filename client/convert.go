package client

import (
	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
)

func toProtoAPIVersion(value APIVersion) *daemonv1.ApiVersion {
	return &daemonv1.ApiVersion{Major: value.Major, Minor: value.Minor}
}

func fromProtoAPIVersion(value *daemonv1.ApiVersion) APIVersion {
	if value == nil {
		return APIVersion{}
	}

	return APIVersion{Major: value.Major, Minor: value.Minor}
}

func fromProtoWorkspace(value *workspacev1.Workspace) (Workspace, error) {
	if value == nil || value.Id == nil {
		return Workspace{}, errIncompleteWorkspace
	}

	return Workspace{ID: value.Id.Value, Root: value.Root}, nil
}
