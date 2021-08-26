// Code generated by counterfeiter. DO NOT EDIT.
package controllersfakes

import (
	"context"
	"sync"

	"code.cloudfoundry.org/cf-k8s-controllers/controllers"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type FakeCFAppClient struct {
	GetStub        func(context.Context, types.NamespacedName, client.Object) error
	getMutex       sync.RWMutex
	getArgsForCall []struct {
		arg1 context.Context
		arg2 types.NamespacedName
		arg3 client.Object
	}
	getReturns struct {
		result1 error
	}
	getReturnsOnCall map[int]struct {
		result1 error
	}
	StatusStub        func() client.StatusWriter
	statusMutex       sync.RWMutex
	statusArgsForCall []struct {
	}
	statusReturns struct {
		result1 client.StatusWriter
	}
	statusReturnsOnCall map[int]struct {
		result1 client.StatusWriter
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeCFAppClient) Get(arg1 context.Context, arg2 types.NamespacedName, arg3 client.Object) error {
	fake.getMutex.Lock()
	ret, specificReturn := fake.getReturnsOnCall[len(fake.getArgsForCall)]
	fake.getArgsForCall = append(fake.getArgsForCall, struct {
		arg1 context.Context
		arg2 types.NamespacedName
		arg3 client.Object
	}{arg1, arg2, arg3})
	stub := fake.GetStub
	fakeReturns := fake.getReturns
	fake.recordInvocation("Get", []interface{}{arg1, arg2, arg3})
	fake.getMutex.Unlock()
	if stub != nil {
		return stub(arg1, arg2, arg3)
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeCFAppClient) GetCallCount() int {
	fake.getMutex.RLock()
	defer fake.getMutex.RUnlock()
	return len(fake.getArgsForCall)
}

func (fake *FakeCFAppClient) GetCalls(stub func(context.Context, types.NamespacedName, client.Object) error) {
	fake.getMutex.Lock()
	defer fake.getMutex.Unlock()
	fake.GetStub = stub
}

func (fake *FakeCFAppClient) GetArgsForCall(i int) (context.Context, types.NamespacedName, client.Object) {
	fake.getMutex.RLock()
	defer fake.getMutex.RUnlock()
	argsForCall := fake.getArgsForCall[i]
	return argsForCall.arg1, argsForCall.arg2, argsForCall.arg3
}

func (fake *FakeCFAppClient) GetReturns(result1 error) {
	fake.getMutex.Lock()
	defer fake.getMutex.Unlock()
	fake.GetStub = nil
	fake.getReturns = struct {
		result1 error
	}{result1}
}

func (fake *FakeCFAppClient) GetReturnsOnCall(i int, result1 error) {
	fake.getMutex.Lock()
	defer fake.getMutex.Unlock()
	fake.GetStub = nil
	if fake.getReturnsOnCall == nil {
		fake.getReturnsOnCall = make(map[int]struct {
			result1 error
		})
	}
	fake.getReturnsOnCall[i] = struct {
		result1 error
	}{result1}
}

func (fake *FakeCFAppClient) Status() client.StatusWriter {
	fake.statusMutex.Lock()
	ret, specificReturn := fake.statusReturnsOnCall[len(fake.statusArgsForCall)]
	fake.statusArgsForCall = append(fake.statusArgsForCall, struct {
	}{})
	stub := fake.StatusStub
	fakeReturns := fake.statusReturns
	fake.recordInvocation("Status", []interface{}{})
	fake.statusMutex.Unlock()
	if stub != nil {
		return stub()
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeCFAppClient) StatusCallCount() int {
	fake.statusMutex.RLock()
	defer fake.statusMutex.RUnlock()
	return len(fake.statusArgsForCall)
}

func (fake *FakeCFAppClient) StatusCalls(stub func() client.StatusWriter) {
	fake.statusMutex.Lock()
	defer fake.statusMutex.Unlock()
	fake.StatusStub = stub
}

func (fake *FakeCFAppClient) StatusReturns(result1 client.StatusWriter) {
	fake.statusMutex.Lock()
	defer fake.statusMutex.Unlock()
	fake.StatusStub = nil
	fake.statusReturns = struct {
		result1 client.StatusWriter
	}{result1}
}

func (fake *FakeCFAppClient) StatusReturnsOnCall(i int, result1 client.StatusWriter) {
	fake.statusMutex.Lock()
	defer fake.statusMutex.Unlock()
	fake.StatusStub = nil
	if fake.statusReturnsOnCall == nil {
		fake.statusReturnsOnCall = make(map[int]struct {
			result1 client.StatusWriter
		})
	}
	fake.statusReturnsOnCall[i] = struct {
		result1 client.StatusWriter
	}{result1}
}

func (fake *FakeCFAppClient) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.getMutex.RLock()
	defer fake.getMutex.RUnlock()
	fake.statusMutex.RLock()
	defer fake.statusMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeCFAppClient) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ controllers.CFAppClient = new(FakeCFAppClient)