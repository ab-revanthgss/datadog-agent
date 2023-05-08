// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux_bpf
// +build linux_bpf

package usm

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"go.uber.org/atomic"
	"os"
	"regexp"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/DataDog/gopsutil/process"
	"github.com/twmb/murmur3"
	"golang.org/x/sys/unix"

	ddebpf "github.com/DataDog/datadog-agent/pkg/ebpf"
	"github.com/DataDog/datadog-agent/pkg/network/protocols/http"
	"github.com/DataDog/datadog-agent/pkg/process/monitor"
	"github.com/DataDog/datadog-agent/pkg/process/util"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

func toLibPath(data []byte) http.LibPath {
	return *(*http.LibPath)(unsafe.Pointer(&data[0]))
}

func toBytes(l *http.LibPath) []byte {
	return l.Buf[:l.Len]
}

// pathIdentifier is the unique key (system wide) of a file based on dev/inode
type pathIdentifier struct {
	dev   uint64
	inode uint64
}

func (p *pathIdentifier) String() string {
	return fmt.Sprintf("dev/inode %d.%d/%d", unix.Major(p.dev), unix.Minor(p.dev), p.inode)
}

// Key is a unique (system wide) TLDR Base64(murmur3.Sum64(device, inode))
// It composes based the device (minor, major) and inode of a file
// murmur is a non-crypto hashing
//
//	As multiple containers overlayfs (same inode but could be overwritten with different binary)
//	device would be different
//
// a Base64 string representation is returned and could be used in a file path
func (p *pathIdentifier) Key() string {
	buffer := make([]byte, 16)
	binary.LittleEndian.PutUint64(buffer, p.dev)
	binary.LittleEndian.PutUint64(buffer[8:], p.inode)
	m := murmur3.Sum64(buffer)
	bufferSum := make([]byte, 8)
	binary.LittleEndian.PutUint64(bufferSum, m)
	return base64.StdEncoding.EncodeToString(bufferSum)
}

// path must be an absolute path
func newPathIdentifier(path string) (pi pathIdentifier, err error) {
	if len(path) < 1 || path[0] != '/' {
		return pi, fmt.Errorf("invalid path %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return pi, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return pi, fmt.Errorf("invalid file %q stat %T", path, info.Sys())
	}

	return pathIdentifier{
		dev:   stat.Dev,
		inode: stat.Ino,
	}, nil
}

type soRule struct {
	re           *regexp.Regexp
	registerCB   func(id pathIdentifier, root string, path string) error
	unregisterCB func(id pathIdentifier) error
}

// soWatcher provides a way to tie callback functions to the lifecycle of shared libraries
type soWatcher struct {
	wg             sync.WaitGroup
	done           chan struct{}
	procRoot       string
	rules          []soRule
	loadEvents     *ddebpf.PerfHandler
	processMonitor *monitor.ProcessMonitor
	registry       *soRegistry
}

type soRegistry struct {
	byID  sync.Map // map[pathIdentifier]*soRegistration
	byPID sync.Map // map[uint32]map[pathIdentifier]struct{}

	// if we can't register a uprobe we don't try more than once
	blocklistByID sync.Map // map[pathIdentifier]struct{}
}

func newSOWatcher(perfHandler *ddebpf.PerfHandler, rules ...soRule) *soWatcher {
	return &soWatcher{
		wg:             sync.WaitGroup{},
		done:           make(chan struct{}),
		procRoot:       util.GetProcRoot(),
		rules:          rules,
		loadEvents:     perfHandler,
		processMonitor: monitor.GetProcessMonitor(),
		registry: &soRegistry{
			byID:          sync.Map{},
			byPID:         sync.Map{},
			blocklistByID: sync.Map{},
		},
	}
}

type soRegistration struct {
	uniqueProcessesCount atomic.Int32
	unregisterCB         func(pathIdentifier) error
}

// unregister return true if there are no more reference to this registration
func (r *soRegistration) unregisterPath(pathID pathIdentifier) bool {
	currentUniqueProcessesCount := r.uniqueProcessesCount.Dec()
	if currentUniqueProcessesCount > 0 {
		return false
	}
	if currentUniqueProcessesCount < 0 {
		log.Errorf("unregistered %+v too much (current counter %v)", pathID, currentUniqueProcessesCount)
		return true
	}
	// currentUniqueProcessesCount is 0, thus we should unregister.
	if r.unregisterCB != nil {
		if err := r.unregisterCB(pathID); err != nil {
			// Even if we fail here, we have to return true, as best effort methodology.
			// We cannot handle the failure, and thus we should continue.
			log.Warnf("error while unregistering %s : %s", pathID.String(), err)
		}
	}
	return true
}

func newRegistration(unregister func(pathIdentifier) error) *soRegistration {
	uniqueCounter := atomic.Int32{}
	uniqueCounter.Store(int32(1))
	return &soRegistration{
		unregisterCB:         unregister,
		uniqueProcessesCount: uniqueCounter,
	}
}

func (w *soWatcher) Stop() {
	close(w.done)
	w.wg.Wait()
}

// Start consuming shared-library events
func (w *soWatcher) Start() {
	thisPID, err := util.GetRootNSPID()
	if err != nil {
		log.Warnf("soWatcher Start can't get root namespace pid %s", err)
	}

	_ = util.WithAllProcs(w.procRoot, func(pid int) error {
		if pid == thisPID { // don't scan ourself
			return nil
		}

		// report silently parsing /proc error as this could happen
		// just exit processes
		proc, err := process.NewProcess(int32(pid))
		if err != nil {
			log.Debugf("process %d parsing failed %s", pid, err)
			return nil
		}
		mmaps, err := proc.MemoryMaps(true)
		if err != nil {
			log.Tracef("process %d maps parsing failed %s", pid, err)
			return nil
		}

		root := fmt.Sprintf("%s/%d/root", w.procRoot, pid)
		for _, m := range *mmaps {
			for _, r := range w.rules {
				if r.re.MatchString(m.Path) {
					w.registry.register(root, m.Path, uint32(pid), r)
					break
				}
			}
		}

		return nil
	})

	if err := w.processMonitor.Initialize(); err != nil {
		log.Errorf("can't initialize process monitor %s", err)
		return
	}

	cleanupExit, err := w.processMonitor.SubscribeExit(&monitor.ProcessCallback{
		FilterType: monitor.ANY,
		Callback:   w.registry.unregister,
	})
	if err != nil {
		log.Errorf("can't subscribe to process monitor exit event %s", err)
		return
	}

	w.wg.Add(1)
	go func() {
		processSync := time.NewTicker(time.Minute)

		defer func() {
			processSync.Stop()
			// Removing the registration of our hook.
			cleanupExit()
			// Stopping the process monitor (if we're the last instance)
			w.processMonitor.Stop()
			// Cleaning up all active hooks.
			w.registry.cleanup()
			// marking we're finished.
			w.wg.Done()
		}()

		for {
			select {
			case <-w.done:
				return
			case <-processSync.C:
				processSet := make(map[int32]struct{})
				w.registry.byPID.Range(func(key, _ any) bool {
					pid := key.(uint32)
					processSet[int32(pid)] = struct{}{}
					return true
				})

				deletedPids := monitor.FindDeletedProcesses(processSet)
				for deletedPid := range deletedPids {
					w.registry.unregister(int(deletedPid))
				}
			case event, ok := <-w.loadEvents.DataChannel:
				if !ok {
					return
				}

				lib := toLibPath(event.Data)
				if int(lib.Pid) == thisPID {
					// don't scan ourself
					event.Done()
					continue
				}

				path := toBytes(&lib)
				libPath := string(path)
				procPid := fmt.Sprintf("%s/%d", w.procRoot, lib.Pid)
				root := procPid + "/root"
				// use cwd of the process as root if the path is relative
				if libPath[0] != '/' {
					root = procPid + "/cwd"
					libPath = "/" + libPath
				}

				for _, r := range w.rules {
					if r.re.Match(path) {
						w.registry.register(root, libPath, lib.Pid, r)
						break
					}
				}
				event.Done()
			case <-w.loadEvents.LostChannel:
				// Nothing to do in this case
				break
			}
		}
	}()
}

// cleanup removes all registrations
func (r *soRegistry) cleanup() {
	r.byID.Range(func(key, value any) bool {
		pathID := key.(pathIdentifier)
		registry := value.(*soRegistration)
		registry.unregisterPath(pathID)
		return true
	})
}

// unregister a pid if exists, unregisterCB will be called if his uniqueProcessesCount == 0
func (r *soRegistry) unregister(pid int) {
	paths, found := r.byPID.LoadAndDelete(uint32(pid))
	if !found {
		return
	}

	pathSet := paths.(*sync.Map)
	pathSet.Range(func(key, _ any) bool {
		pathID := key.(pathIdentifier)
		loaded, found := r.byID.Load(pathID)
		if found {
			registry := loaded.(*soRegistration)
			if registry.unregisterPath(pathID) {
				// we need to clean up our entries as there are no more processes using this ELF
				r.byID.Delete(pathID)
			}
		}
		return true
	})
}

// register a ELF library root/libPath as be used by the pid
// Only one registration will be done per ELF (system wide)
func (r *soRegistry) register(root, libPath string, pid uint32, rule soRule) {
	hostLibPath := root + libPath
	pathID, err := newPathIdentifier(hostLibPath)
	if err != nil {
		// short living process can hit here
		// as we receive the openat() syscall info after receiving the EXIT netlink process
		log.Tracef("can't create path identifier %s", err)
		return
	}

	if _, found := r.blocklistByID.Load(pathID); found {
		return
	}

	reg, found := r.byID.LoadOrStore(pathID, newRegistration(rule.unregisterCB))
	if found {
		registry := reg.(*soRegistration)
		pathSetRaw, _ := r.byPID.LoadOrStore(pid, &sync.Map{})
		pathSet := pathSetRaw.(*sync.Map)
		if _, found := pathSet.LoadOrStore(pathID, struct{}{}); !found {
			registry.uniqueProcessesCount.Inc()
		}
		return
	}

	// Only the first can get here.
	if err := rule.registerCB(pathID, root, libPath); err != nil {
		log.Debugf("error registering library (adding to blocklist) %s path %s by pid %d : %s", pathID.String(), hostLibPath, pid, err)
		// we are calling unregisterCB here as some uprobes could be already attached, unregisterCB cleanup those entries
		if rule.unregisterCB != nil {
			if err := rule.unregisterCB(pathID); err != nil {
				log.Debugf("unregisterCB library %s path %s : %s", pathID.String(), hostLibPath, err)
			}
		}
		// save sentinel value, so we don't attempt to re-register shared
		// libraries that are problematic for some reason
		r.blocklistByID.Store(pathID, struct{}{})

		// Deleting the temporary value we've put before.
		r.byID.Delete(pathID)
		return
	}

	pidMapRaw, _ := r.byPID.LoadOrStore(pid, &sync.Map{})
	pidMap := pidMapRaw.(*sync.Map)
	pidMap.Store(pathID, struct{}{})
	log.Debugf("registering library %s path %s by pid %d", pathID.String(), hostLibPath, pid)
}
