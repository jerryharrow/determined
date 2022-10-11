// Code generated by mockery v2.13.1. DO NOT EDIT.

package mocks

import (
	mock "github.com/stretchr/testify/mock"

	model "github.com/determined-ai/determined/master/pkg/model"
	projectv1 "github.com/determined-ai/determined/proto/pkg/projectv1"

	workspacev1 "github.com/determined-ai/determined/proto/pkg/workspacev1"
)

// WorkspaceAuthZ is an autogenerated mock type for the WorkspaceAuthZ type
type WorkspaceAuthZ struct {
	mock.Mock
}

// CanArchiveWorkspace provides a mock function with given fields: curUser, _a1
func (_m *WorkspaceAuthZ) CanArchiveWorkspace(curUser model.User, _a1 *workspacev1.Workspace) error {
	ret := _m.Called(curUser, _a1)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User, *workspacev1.Workspace) error); ok {
		r0 = rf(curUser, _a1)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CanCreateWorkspace provides a mock function with given fields: curUser
func (_m *WorkspaceAuthZ) CanCreateWorkspace(curUser model.User) error {
	ret := _m.Called(curUser)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User) error); ok {
		r0 = rf(curUser)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CanCreateWorkspaceWithAgentUserGroup provides a mock function with given fields: curUser
func (_m *WorkspaceAuthZ) CanCreateWorkspaceWithAgentUserGroup(curUser model.User) error {
	ret := _m.Called(curUser)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User) error); ok {
		r0 = rf(curUser)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CanDeleteWorkspace provides a mock function with given fields: curUser, _a1
func (_m *WorkspaceAuthZ) CanDeleteWorkspace(curUser model.User, _a1 *workspacev1.Workspace) error {
	ret := _m.Called(curUser, _a1)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User, *workspacev1.Workspace) error); ok {
		r0 = rf(curUser, _a1)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CanGetWorkspace provides a mock function with given fields: curUser, _a1
func (_m *WorkspaceAuthZ) CanGetWorkspace(curUser model.User, _a1 *workspacev1.Workspace) (bool, error) {
	ret := _m.Called(curUser, _a1)

	var r0 bool
	if rf, ok := ret.Get(0).(func(model.User, *workspacev1.Workspace) bool); ok {
		r0 = rf(curUser, _a1)
	} else {
		r0 = ret.Get(0).(bool)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(model.User, *workspacev1.Workspace) error); ok {
		r1 = rf(curUser, _a1)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// CanPinWorkspace provides a mock function with given fields: curUser, _a1
func (_m *WorkspaceAuthZ) CanPinWorkspace(curUser model.User, _a1 *workspacev1.Workspace) error {
	ret := _m.Called(curUser, _a1)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User, *workspacev1.Workspace) error); ok {
		r0 = rf(curUser, _a1)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CanSetWorkspacesAgentUserGroup provides a mock function with given fields: curUser, _a1
func (_m *WorkspaceAuthZ) CanSetWorkspacesAgentUserGroup(curUser model.User, _a1 *workspacev1.Workspace) error {
	ret := _m.Called(curUser, _a1)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User, *workspacev1.Workspace) error); ok {
		r0 = rf(curUser, _a1)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CanSetWorkspacesName provides a mock function with given fields: curUser, _a1
func (_m *WorkspaceAuthZ) CanSetWorkspacesName(curUser model.User, _a1 *workspacev1.Workspace) error {
	ret := _m.Called(curUser, _a1)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User, *workspacev1.Workspace) error); ok {
		r0 = rf(curUser, _a1)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CanUnarchiveWorkspace provides a mock function with given fields: curUser, _a1
func (_m *WorkspaceAuthZ) CanUnarchiveWorkspace(curUser model.User, _a1 *workspacev1.Workspace) error {
	ret := _m.Called(curUser, _a1)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User, *workspacev1.Workspace) error); ok {
		r0 = rf(curUser, _a1)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// CanUnpinWorkspace provides a mock function with given fields: curUser, _a1
func (_m *WorkspaceAuthZ) CanUnpinWorkspace(curUser model.User, _a1 *workspacev1.Workspace) error {
	ret := _m.Called(curUser, _a1)

	var r0 error
	if rf, ok := ret.Get(0).(func(model.User, *workspacev1.Workspace) error); ok {
		r0 = rf(curUser, _a1)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// FilterWorkspaceProjects provides a mock function with given fields: curUser, projects
func (_m *WorkspaceAuthZ) FilterWorkspaceProjects(curUser model.User, projects []*projectv1.Project) ([]*projectv1.Project, error) {
	ret := _m.Called(curUser, projects)

	var r0 []*projectv1.Project
	if rf, ok := ret.Get(0).(func(model.User, []*projectv1.Project) []*projectv1.Project); ok {
		r0 = rf(curUser, projects)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]*projectv1.Project)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(model.User, []*projectv1.Project) error); ok {
		r1 = rf(curUser, projects)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// FilterWorkspaces provides a mock function with given fields: curUser, workspaces
func (_m *WorkspaceAuthZ) FilterWorkspaces(curUser model.User, workspaces []*workspacev1.Workspace) ([]*workspacev1.Workspace, error) {
	ret := _m.Called(curUser, workspaces)

	var r0 []*workspacev1.Workspace
	if rf, ok := ret.Get(0).(func(model.User, []*workspacev1.Workspace) []*workspacev1.Workspace); ok {
		r0 = rf(curUser, workspaces)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]*workspacev1.Workspace)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(model.User, []*workspacev1.Workspace) error); ok {
		r1 = rf(curUser, workspaces)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

type mockConstructorTestingTNewWorkspaceAuthZ interface {
	mock.TestingT
	Cleanup(func())
}

// NewWorkspaceAuthZ creates a new instance of WorkspaceAuthZ. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewWorkspaceAuthZ(t mockConstructorTestingTNewWorkspaceAuthZ) *WorkspaceAuthZ {
	mock := &WorkspaceAuthZ{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
