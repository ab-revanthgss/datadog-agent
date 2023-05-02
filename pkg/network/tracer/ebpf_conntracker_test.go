// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux_bpf
// +build linux_bpf

package tracer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DataDog/datadog-agent/pkg/network/tracer/offsetguess"
)

func TestEbpfConntrackerLoadTriggersOffsetGuessing(t *testing.T) {
	require.Empty(t, offsetguess.TracerOffsets.Constants)
	require.NoError(t, offsetguess.TracerOffsets.Err)

	cfg := testConfig()
	cfg.EnableRuntimeCompiler = false
	_, err := NewEBPFConntracker(cfg, nil)
	assert.NoError(t, err)
	assert.NotEmpty(t, offsetguess.TracerOffsets)
	assert.NoError(t, offsetguess.TracerOffsets.Err)
}
