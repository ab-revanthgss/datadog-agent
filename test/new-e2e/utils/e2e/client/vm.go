// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package client

import (
	"testing"

	commonvm "github.com/DataDog/test-infra-definitions/common/vm"
)

var _ clientService[commonvm.ClientData] = (*VM)(nil)

// A client VM that is connected to a VM defined in test-infra-definition.
type VM struct {
	*UpResultDeserializer[commonvm.ClientData]
	*vmClient
}

// Create a new instance of VM
func NewVM(infraVM commonvm.VM) *VM {
	vm := &VM{}
	vm.UpResultDeserializer = NewUpResultDeserializer[commonvm.ClientData](infraVM, vm)
	return vm
}

//lint:ignore U1000 Ignore unused function as this function is call using reflection
func (vm *VM) initService(t *testing.T, data *commonvm.ClientData) error {
	var err error
	vm.vmClient, err = newVMClient(t, "", &data.Connection)
	return err
}
