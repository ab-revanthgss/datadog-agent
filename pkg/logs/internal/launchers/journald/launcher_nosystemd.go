// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build !systemd
// +build !systemd

package journald

import (
	"github.com/DataDog/datadog-agent/pkg/logs/auditor"
	"github.com/DataDog/datadog-agent/pkg/logs/internal/launchers"
	"github.com/DataDog/datadog-agent/pkg/logs/internal/tailers"
	"github.com/DataDog/datadog-agent/pkg/logs/pipeline"
)

// Launcher is not supported on no systemd environment.
type Launcher struct{}

// NewLauncher returns a new Launcher
func NewLauncher() *Launcher {
	return &Launcher{}
}

// Start does nothing
func (l *Launcher) Start(sources launchers.SourceProvider, pipelineProvider pipeline.Provider, registry auditor.Registry, tracker *tailers.TailerTracker) {
}

// Stop does nothing
func (l *Launcher) Stop() {}
