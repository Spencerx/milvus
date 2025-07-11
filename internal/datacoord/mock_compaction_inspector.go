// Code generated by mockery v2.53.3. DO NOT EDIT.

package datacoord

import (
	context "context"

	datapb "github.com/milvus-io/milvus/pkg/v2/proto/datapb"
	mock "github.com/stretchr/testify/mock"
)

// MockCompactionInspector is an autogenerated mock type for the CompactionInspector type
type MockCompactionInspector struct {
	mock.Mock
}

type MockCompactionInspector_Expecter struct {
	mock *mock.Mock
}

func (_m *MockCompactionInspector) EXPECT() *MockCompactionInspector_Expecter {
	return &MockCompactionInspector_Expecter{mock: &_m.Mock}
}

// enqueueCompaction provides a mock function with given fields: task
func (_m *MockCompactionInspector) enqueueCompaction(task *datapb.CompactionTask) error {
	ret := _m.Called(task)

	if len(ret) == 0 {
		panic("no return value specified for enqueueCompaction")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(*datapb.CompactionTask) error); ok {
		r0 = rf(task)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// MockCompactionInspector_enqueueCompaction_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'enqueueCompaction'
type MockCompactionInspector_enqueueCompaction_Call struct {
	*mock.Call
}

// enqueueCompaction is a helper method to define mock.On call
//   - task *datapb.CompactionTask
func (_e *MockCompactionInspector_Expecter) enqueueCompaction(task interface{}) *MockCompactionInspector_enqueueCompaction_Call {
	return &MockCompactionInspector_enqueueCompaction_Call{Call: _e.mock.On("enqueueCompaction", task)}
}

func (_c *MockCompactionInspector_enqueueCompaction_Call) Run(run func(task *datapb.CompactionTask)) *MockCompactionInspector_enqueueCompaction_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*datapb.CompactionTask))
	})
	return _c
}

func (_c *MockCompactionInspector_enqueueCompaction_Call) Return(_a0 error) *MockCompactionInspector_enqueueCompaction_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockCompactionInspector_enqueueCompaction_Call) RunAndReturn(run func(*datapb.CompactionTask) error) *MockCompactionInspector_enqueueCompaction_Call {
	_c.Call.Return(run)
	return _c
}

// getCompactionInfo provides a mock function with given fields: ctx, signalID
func (_m *MockCompactionInspector) getCompactionInfo(ctx context.Context, signalID int64) *compactionInfo {
	ret := _m.Called(ctx, signalID)

	if len(ret) == 0 {
		panic("no return value specified for getCompactionInfo")
	}

	var r0 *compactionInfo
	if rf, ok := ret.Get(0).(func(context.Context, int64) *compactionInfo); ok {
		r0 = rf(ctx, signalID)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*compactionInfo)
		}
	}

	return r0
}

// MockCompactionInspector_getCompactionInfo_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'getCompactionInfo'
type MockCompactionInspector_getCompactionInfo_Call struct {
	*mock.Call
}

// getCompactionInfo is a helper method to define mock.On call
//   - ctx context.Context
//   - signalID int64
func (_e *MockCompactionInspector_Expecter) getCompactionInfo(ctx interface{}, signalID interface{}) *MockCompactionInspector_getCompactionInfo_Call {
	return &MockCompactionInspector_getCompactionInfo_Call{Call: _e.mock.On("getCompactionInfo", ctx, signalID)}
}

func (_c *MockCompactionInspector_getCompactionInfo_Call) Run(run func(ctx context.Context, signalID int64)) *MockCompactionInspector_getCompactionInfo_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(int64))
	})
	return _c
}

func (_c *MockCompactionInspector_getCompactionInfo_Call) Return(_a0 *compactionInfo) *MockCompactionInspector_getCompactionInfo_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockCompactionInspector_getCompactionInfo_Call) RunAndReturn(run func(context.Context, int64) *compactionInfo) *MockCompactionInspector_getCompactionInfo_Call {
	_c.Call.Return(run)
	return _c
}

// getCompactionTasksNum provides a mock function with given fields: filters
func (_m *MockCompactionInspector) getCompactionTasksNum(filters ...compactionTaskFilter) int {
	_va := make([]interface{}, len(filters))
	for _i := range filters {
		_va[_i] = filters[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	if len(ret) == 0 {
		panic("no return value specified for getCompactionTasksNum")
	}

	var r0 int
	if rf, ok := ret.Get(0).(func(...compactionTaskFilter) int); ok {
		r0 = rf(filters...)
	} else {
		r0 = ret.Get(0).(int)
	}

	return r0
}

// MockCompactionInspector_getCompactionTasksNum_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'getCompactionTasksNum'
type MockCompactionInspector_getCompactionTasksNum_Call struct {
	*mock.Call
}

// getCompactionTasksNum is a helper method to define mock.On call
//   - filters ...compactionTaskFilter
func (_e *MockCompactionInspector_Expecter) getCompactionTasksNum(filters ...interface{}) *MockCompactionInspector_getCompactionTasksNum_Call {
	return &MockCompactionInspector_getCompactionTasksNum_Call{Call: _e.mock.On("getCompactionTasksNum",
		append([]interface{}{}, filters...)...)}
}

func (_c *MockCompactionInspector_getCompactionTasksNum_Call) Run(run func(filters ...compactionTaskFilter)) *MockCompactionInspector_getCompactionTasksNum_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]compactionTaskFilter, len(args)-0)
		for i, a := range args[0:] {
			if a != nil {
				variadicArgs[i] = a.(compactionTaskFilter)
			}
		}
		run(variadicArgs...)
	})
	return _c
}

func (_c *MockCompactionInspector_getCompactionTasksNum_Call) Return(_a0 int) *MockCompactionInspector_getCompactionTasksNum_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockCompactionInspector_getCompactionTasksNum_Call) RunAndReturn(run func(...compactionTaskFilter) int) *MockCompactionInspector_getCompactionTasksNum_Call {
	_c.Call.Return(run)
	return _c
}

// getCompactionTasksNumBySignalID provides a mock function with given fields: signalID
func (_m *MockCompactionInspector) getCompactionTasksNumBySignalID(signalID int64) int {
	ret := _m.Called(signalID)

	if len(ret) == 0 {
		panic("no return value specified for getCompactionTasksNumBySignalID")
	}

	var r0 int
	if rf, ok := ret.Get(0).(func(int64) int); ok {
		r0 = rf(signalID)
	} else {
		r0 = ret.Get(0).(int)
	}

	return r0
}

// MockCompactionInspector_getCompactionTasksNumBySignalID_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'getCompactionTasksNumBySignalID'
type MockCompactionInspector_getCompactionTasksNumBySignalID_Call struct {
	*mock.Call
}

// getCompactionTasksNumBySignalID is a helper method to define mock.On call
//   - signalID int64
func (_e *MockCompactionInspector_Expecter) getCompactionTasksNumBySignalID(signalID interface{}) *MockCompactionInspector_getCompactionTasksNumBySignalID_Call {
	return &MockCompactionInspector_getCompactionTasksNumBySignalID_Call{Call: _e.mock.On("getCompactionTasksNumBySignalID", signalID)}
}

func (_c *MockCompactionInspector_getCompactionTasksNumBySignalID_Call) Run(run func(signalID int64)) *MockCompactionInspector_getCompactionTasksNumBySignalID_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(int64))
	})
	return _c
}

func (_c *MockCompactionInspector_getCompactionTasksNumBySignalID_Call) Return(_a0 int) *MockCompactionInspector_getCompactionTasksNumBySignalID_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockCompactionInspector_getCompactionTasksNumBySignalID_Call) RunAndReturn(run func(int64) int) *MockCompactionInspector_getCompactionTasksNumBySignalID_Call {
	_c.Call.Return(run)
	return _c
}

// isFull provides a mock function with no fields
func (_m *MockCompactionInspector) isFull() bool {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for isFull")
	}

	var r0 bool
	if rf, ok := ret.Get(0).(func() bool); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// MockCompactionInspector_isFull_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'isFull'
type MockCompactionInspector_isFull_Call struct {
	*mock.Call
}

// isFull is a helper method to define mock.On call
func (_e *MockCompactionInspector_Expecter) isFull() *MockCompactionInspector_isFull_Call {
	return &MockCompactionInspector_isFull_Call{Call: _e.mock.On("isFull")}
}

func (_c *MockCompactionInspector_isFull_Call) Run(run func()) *MockCompactionInspector_isFull_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockCompactionInspector_isFull_Call) Return(_a0 bool) *MockCompactionInspector_isFull_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockCompactionInspector_isFull_Call) RunAndReturn(run func() bool) *MockCompactionInspector_isFull_Call {
	_c.Call.Return(run)
	return _c
}

// removeTasksByChannel provides a mock function with given fields: channel
func (_m *MockCompactionInspector) removeTasksByChannel(channel string) {
	_m.Called(channel)
}

// MockCompactionInspector_removeTasksByChannel_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'removeTasksByChannel'
type MockCompactionInspector_removeTasksByChannel_Call struct {
	*mock.Call
}

// removeTasksByChannel is a helper method to define mock.On call
//   - channel string
func (_e *MockCompactionInspector_Expecter) removeTasksByChannel(channel interface{}) *MockCompactionInspector_removeTasksByChannel_Call {
	return &MockCompactionInspector_removeTasksByChannel_Call{Call: _e.mock.On("removeTasksByChannel", channel)}
}

func (_c *MockCompactionInspector_removeTasksByChannel_Call) Run(run func(channel string)) *MockCompactionInspector_removeTasksByChannel_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string))
	})
	return _c
}

func (_c *MockCompactionInspector_removeTasksByChannel_Call) Return() *MockCompactionInspector_removeTasksByChannel_Call {
	_c.Call.Return()
	return _c
}

func (_c *MockCompactionInspector_removeTasksByChannel_Call) RunAndReturn(run func(string)) *MockCompactionInspector_removeTasksByChannel_Call {
	_c.Run(run)
	return _c
}

// start provides a mock function with no fields
func (_m *MockCompactionInspector) start() {
	_m.Called()
}

// MockCompactionInspector_start_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'start'
type MockCompactionInspector_start_Call struct {
	*mock.Call
}

// start is a helper method to define mock.On call
func (_e *MockCompactionInspector_Expecter) start() *MockCompactionInspector_start_Call {
	return &MockCompactionInspector_start_Call{Call: _e.mock.On("start")}
}

func (_c *MockCompactionInspector_start_Call) Run(run func()) *MockCompactionInspector_start_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockCompactionInspector_start_Call) Return() *MockCompactionInspector_start_Call {
	_c.Call.Return()
	return _c
}

func (_c *MockCompactionInspector_start_Call) RunAndReturn(run func()) *MockCompactionInspector_start_Call {
	_c.Run(run)
	return _c
}

// stop provides a mock function with no fields
func (_m *MockCompactionInspector) stop() {
	_m.Called()
}

// MockCompactionInspector_stop_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'stop'
type MockCompactionInspector_stop_Call struct {
	*mock.Call
}

// stop is a helper method to define mock.On call
func (_e *MockCompactionInspector_Expecter) stop() *MockCompactionInspector_stop_Call {
	return &MockCompactionInspector_stop_Call{Call: _e.mock.On("stop")}
}

func (_c *MockCompactionInspector_stop_Call) Run(run func()) *MockCompactionInspector_stop_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockCompactionInspector_stop_Call) Return() *MockCompactionInspector_stop_Call {
	_c.Call.Return()
	return _c
}

func (_c *MockCompactionInspector_stop_Call) RunAndReturn(run func()) *MockCompactionInspector_stop_Call {
	_c.Run(run)
	return _c
}

// NewMockCompactionInspector creates a new instance of MockCompactionInspector. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockCompactionInspector(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockCompactionInspector {
	mock := &MockCompactionInspector{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
