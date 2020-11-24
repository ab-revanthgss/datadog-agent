// +build linux

package procutil

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/DataDog/gopsutil/host"
	"github.com/DataDog/gopsutil/process"
)

func TestGetActivePIDs(t *testing.T) {
	os.Setenv("HOST_PROC", "resources/test_procfs/proc")
	defer os.Unsetenv("HOST_PROC")

	probe := NewProcessProbe()
	defer probe.Close()

	actual, err := probe.getActivePIDs()
	assert.NoError(t, err)

	expect, err := process.Pids()
	assert.NoError(t, err)

	assert.ElementsMatch(t, expect, actual)
}

func TestTrimAndSplitBytes(t *testing.T) {
	for _, tc := range []struct {
		input  []byte
		expect []string
	}{
		{
			input:  []byte{115, 115, 104, 100, 58, 32, 115, 117, 110, 110, 121, 46, 107, 108, 97, 105, 114, 64, 112, 116, 115, 47, 48},
			expect: []string{string([]byte{115, 115, 104, 100, 58, 32, 115, 117, 110, 110, 121, 46, 107, 108, 97, 105, 114, 64, 112, 116, 115, 47, 48})},
		},
		{
			input:  []byte{40, 115, 100, 45, 112, 97, 109, 41, 0, 0, 0},
			expect: []string{string([]byte{40, 115, 100, 45, 112, 97, 109, 41})},
		},
		{
			input:  []byte{115, 115, 104, 100, 58, 32, 118, 97, 103, 114, 97, 110, 116, 64, 112, 116, 115, 47, 48, 0, 0},
			expect: []string{string([]byte{115, 115, 104, 100, 58, 32, 118, 97, 103, 114, 97, 110, 116, 64, 112, 116, 115, 47, 48})},
		},
		{
			input: []byte{47, 117, 115, 114, 47, 98, 105, 110, 47, 100, 111, 99, 107, 101, 114, 100, 0, 45, 72, 0, 102, 100, 58, 47, 47, 0},
			expect: []string{
				string([]byte{47, 117, 115, 114, 47, 98, 105, 110, 47, 100, 111, 99, 107, 101, 114, 100}),
				string([]byte{45, 72}),
				string([]byte{102, 100, 58, 47, 47}),
			},
		},
		{
			input: []byte{47, 117, 115, 114, 47, 98, 105, 110, 47, 100, 111, 99, 107, 101, 114, 100, 0, 45, 72, 0, 102, 100, 58, 47, 47},
			expect: []string{
				string([]byte{47, 117, 115, 114, 47, 98, 105, 110, 47, 100, 111, 99, 107, 101, 114, 100}),
				string([]byte{45, 72}),
				string([]byte{102, 100, 58, 47, 47}),
			},
		},
		{
			input: []byte{47, 117, 115, 114, 47, 98, 105, 110, 47, 100, 111, 99, 107, 101, 114, 100, 0, 0, 0, 45, 72, 0, 102, 100, 58, 47, 47},
			expect: []string{
				string([]byte{47, 117, 115, 114, 47, 98, 105, 110, 47, 100, 111, 99, 107, 101, 114, 100}),
				string([]byte{45, 72}),
				string([]byte{102, 100, 58, 47, 47}),
			},
		},
		{
			input:  []byte{0, 0, 47, 115, 98, 105, 110, 47, 105, 110, 105, 116, 0},
			expect: []string{string([]byte{47, 115, 98, 105, 110, 47, 105, 110, 105, 116})},
		},
	} {
		actual := trimAndSplitBytes(tc.input)
		assert.ElementsMatch(t, tc.expect, actual)
	}
}

func TestGetCmdline(t *testing.T) {
	hostProc := "resources/test_procfs/proc"
	os.Setenv("HOST_PROC", hostProc)
	defer os.Unsetenv("HOST_PROC")

	probe := NewProcessProbe()
	defer probe.Close()

	pids, err := probe.getActivePIDs()
	assert.NoError(t, err)

	for _, pid := range pids {
		actual := strings.Join(probe.getCmdline(filepath.Join(hostProc, strconv.Itoa(int(pid)))), " ")
		expProc, err := process.NewProcess(pid)
		assert.NoError(t, err)

		expect, err := expProc.Cmdline()
		assert.NoError(t, err)

		assert.Equal(t, expect, actual)
	}
}

func TestProcessesByPID(t *testing.T) {
	hostProc := "resources/test_procfs/proc/"
	os.Setenv("HOST_PROC", hostProc)
	defer os.Unsetenv("HOST_PROC")

	probe := NewProcessProbe()
	defer probe.Close()

	pids, err := probe.getActivePIDs()
	assert.NoError(t, err)

	procByPID, err := probe.ProcessesByPID(time.Now())
	assert.NoError(t, err)

	// make sure the process that has no command line doesn't get included in the output
	for _, pid := range pids {
		cmd := strings.Join(probe.getCmdline(filepath.Join(hostProc, strconv.Itoa(int(pid)))), " ")
		if cmd == "" {
			assert.NotContains(t, procByPID, pid)
		} else {
			assert.Contains(t, procByPID, pid)
		}
	}
}

func TestMultipleProbes(t *testing.T) {
	os.Setenv("HOST_PROC", "resources/test_procfs/proc/")
	defer os.Unsetenv("HOST_PROC")

	probe1 := NewProcessProbe()
	defer probe1.Close()

	probe2 := NewProcessProbe()
	defer probe2.Close()

	now := time.Now()

	procByPID1, err := probe1.ProcessesByPID(now)
	assert.NoError(t, err)
	procByPID2, err := probe2.ProcessesByPID(now)
	assert.NoError(t, err)
	for i := 0; i < 10; i++ {
		currProcByPID1, err := probe1.ProcessesByPID(now)
		assert.NoError(t, err)
		currProcByPID2, err := probe2.ProcessesByPID(now)
		assert.NoError(t, err)
		assert.EqualValues(t, currProcByPID1, currProcByPID2)
		assert.EqualValues(t, currProcByPID1, procByPID1)
		assert.EqualValues(t, currProcByPID2, procByPID2)
		procByPID1 = currProcByPID1
		procByPID2 = currProcByPID2
	}
}

func TestProcfsChange(t *testing.T) {
	os.Setenv("HOST_PROC", "resources/test_procfs/proc/")
	defer os.Unsetenv("HOST_PROC")

	probe := NewProcessProbe()
	defer probe.Close()

	now := time.Now()

	procByPID, err := probe.ProcessesByPID(now)
	assert.NoError(t, err)

	// update the procfs file structure to add a pid, make sure next time it reads in the updates
	err = os.Rename("resources/10389", "resources/test_procfs/proc/10389")
	assert.NoError(t, err)
	defer func() {
		err = os.Rename("resources/test_procfs/proc/10389", "resources/10389")
		assert.NoError(t, err)
	}()
	newProcByPID1, err := probe.ProcessesByPID(now)
	assert.NoError(t, err)
	assert.Contains(t, newProcByPID1, int32(10389))
	assert.NotContains(t, procByPID, int32(10389))

	// remove a pid from procfs, make sure it's gone from the result
	err = os.Rename("resources/test_procfs/proc/29613", "resources/29613")
	assert.NoError(t, err)
	defer func() {
		err = os.Rename("resources/29613", "resources/test_procfs/proc/29613")
		assert.NoError(t, err)
	}()
	newProcByPID2, err := probe.ProcessesByPID(now)
	assert.NoError(t, err)
	assert.NotContains(t, newProcByPID2, int32(29613))
	assert.Contains(t, procByPID, int32(29613))
}

func TestParseStatusLine(t *testing.T) {
	probe := NewProcessProbe()
	defer probe.Close()

	for _, tc := range []struct {
		line     []byte
		expected *statusInfo
	}{
		{
			line: []byte("Name:\tpostgres"),
			expected: &statusInfo{
				name:        "postgres",
				memInfo:     &MemoryInfoStat{},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
		{
			line: []byte("Name:\t\t\t\t\tpostgres"),
			expected: &statusInfo{
				name:        "postgres",
				memInfo:     &MemoryInfoStat{},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
		{
			line: []byte("State:\tS (sleeping)"),
			expected: &statusInfo{
				status:      "S",
				memInfo:     &MemoryInfoStat{},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
		{
			line: []byte("Uid:\t112\t112\t112\t112"),
			expected: &statusInfo{
				uids:        []int32{112, 112, 112, 112},
				memInfo:     &MemoryInfoStat{},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
		{
			line: []byte("bad status line"), // bad status
			expected: &statusInfo{
				memInfo:     &MemoryInfoStat{},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
		{
			line: []byte("Name:\t"), // edge case for parsing failure
			expected: &statusInfo{
				memInfo:     &MemoryInfoStat{},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
		{
			line: []byte("NSpid:\t123"),
			expected: &statusInfo{
				nspid:       123,
				memInfo:     &MemoryInfoStat{},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
		{
			line: []byte("VmRSS:\t712 kB"),
			expected: &statusInfo{
				memInfo:     &MemoryInfoStat{RSS: 712 * 1024},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
		{
			line: []byte("VmRSS:\t  712 kB"),
			expected: &statusInfo{
				memInfo:     &MemoryInfoStat{RSS: 712 * 1024},
				ctxSwitches: &NumCtxSwitchesStat{},
			},
		},
	} {
		result := &statusInfo{memInfo: &MemoryInfoStat{}, ctxSwitches: &NumCtxSwitchesStat{}}
		probe.parseStatusLine(tc.line, result)
		assert.EqualValues(t, tc.expected, result)
	}
}

func TestParseStatus(t *testing.T) {
	hostProc := "resources/test_procfs/proc/"
	os.Setenv("HOST_PROC", hostProc)
	defer os.Unsetenv("HOST_PROC")

	probe := NewProcessProbe()
	defer probe.Close()

	pids, err := probe.getActivePIDs()
	assert.NoError(t, err)

	for _, pid := range pids {
		actual := probe.parseStatus(filepath.Join(hostProc, strconv.Itoa(int(pid))))
		expProc, err := process.NewProcess(pid)
		assert.NoError(t, err)

		expName, err := expProc.Name()
		assert.NoError(t, err)
		expStatus, err := expProc.Status()
		assert.NoError(t, err)
		expUIDs, err := expProc.Uids()
		assert.NoError(t, err)
		expGIDs, err := expProc.Gids()
		assert.NoError(t, err)
		expThreads, err := expProc.NumThreads()
		assert.NoError(t, err)
		expMemInfo, err := expProc.MemoryInfo()
		assert.NoError(t, err)
		expCtxSwitches, err := expProc.NumCtxSwitches()
		assert.NoError(t, err)

		assert.Equal(t, expName, actual.name)
		assert.Equal(t, expStatus, actual.status)
		assert.EqualValues(t, expUIDs, actual.uids)
		assert.EqualValues(t, expGIDs, actual.gids)
		assert.Equal(t, expThreads, actual.numThreads)
		assert.Equal(t, expProc.NsPid, actual.nspid)

		assert.Equal(t, expMemInfo.RSS, actual.memInfo.RSS)
		assert.Equal(t, expMemInfo.VMS, actual.memInfo.VMS)
		assert.Equal(t, expMemInfo.Swap, actual.memInfo.Swap)

		assert.Equal(t, expCtxSwitches.Voluntary, actual.ctxSwitches.Voluntary)
		assert.Equal(t, expCtxSwitches.Involuntary, actual.ctxSwitches.Involuntary)
	}
}

func TestParseIOLine(t *testing.T) {
	probe := NewProcessProbe()
	defer probe.Close()

	for _, tc := range []struct {
		line     []byte
		expected *IOCountersStat
	}{
		{
			line:     []byte("syscr: 467721"),
			expected: &IOCountersStat{ReadCount: 467721},
		},
		{
			line:     []byte("syscw: 4842687"),
			expected: &IOCountersStat{WriteCount: 4842687},
		},
		{
			line:     []byte("read_bytes: 65536"),
			expected: &IOCountersStat{ReadBytes: 65536},
		},
		{
			line:     []byte("write_bytes: 4096"),
			expected: &IOCountersStat{WriteBytes: 4096},
		},
	} {
		result := &IOCountersStat{}
		probe.parseIOLine(tc.line, result)
		assert.EqualValues(t, tc.expected, result)
	}
}

func TestParseIO(t *testing.T) {
	hostProc := "resources/test_procfs/proc/"
	os.Setenv("HOST_PROC", hostProc)
	defer os.Unsetenv("HOST_PROC")

	probe := NewProcessProbe()
	defer probe.Close()

	pids, err := probe.getActivePIDs()
	assert.NoError(t, err)

	for _, pid := range pids {
		actual := probe.parseIO(filepath.Join(hostProc, strconv.Itoa(int(pid))))
		expProc, err := process.NewProcess(pid)
		assert.NoError(t, err)
		expIO, err := expProc.IOCounters()
		assert.NoError(t, err)
		assert.Equal(t, expIO.ReadCount, actual.ReadCount)
		assert.Equal(t, expIO.ReadBytes, actual.ReadBytes)
		assert.Equal(t, expIO.WriteCount, actual.WriteCount)
		assert.Equal(t, expIO.WriteBytes, actual.WriteBytes)
	}
}

func TestParseStatContent(t *testing.T) {
	probe := NewProcessProbe()
	defer probe.Close()

	// hard code the bootTime so we get consistent calculation for createTime
	probe.bootTime = 1606181252
	now := time.Now()

	for _, tc := range []struct {
		line     []byte
		expected *statInfo
	}{
		// standard content
		{
			line: []byte("1 (systemd) S 0 1 1 0 -1 4194560 425768 306165945 70 4299 4890 2184 563120 375308 20 0 1 0 15 189849600 1541 18446744073709551615 94223912931328 94223914360080 140733806473072 140733806469312 140053573122579 0 671173123 4096 1260 1 0 0 17 0 0 0 155 0 0 94223914368000 942\n23914514184 94223918080000 140733806477086 140733806477133 140733806477133 140733806477283 0"),
			expected: &statInfo{
				ppid:       0,
				createTime: 1606181252000,
				cpuStat: &CPUTimesStat{
					CPU:       "cpu",
					User:      48.9,
					System:    21.84,
					Timestamp: now.Unix(),
				},
			},
		},
		// command line has brackets around
		{
			line: []byte("1 ((sd-pam)) S 0 1 1 0 -1 4194560 425768 306165945 70 4299 4890 2184 563120 375308 20 0 1 0 15 189849600 1541 18446744073709551615 94223912931328 94223914360080 140733806473072 140733806469312 140053573122579 0 671173123 4096 1260 1 0 0 17 0 0 0 155 0 0 94223914368000 942\n23914514184 94223918080000 140733806477086 140733806477133 140733806477133 140733806477283 0"),
			expected: &statInfo{
				ppid:       0,
				createTime: 1606181252000,
				cpuStat: &CPUTimesStat{
					CPU:       "cpu",
					User:      48.9,
					System:    21.84,
					Timestamp: now.Unix(),
				},
			},
		},
		// fields are separated by multiple white spaces
		{
			line: []byte("5  (kworker/0:0H)   S 2 0 0 0 -1   69238880 0 0  0 0  0 0 0 0 0  -20 1 0 17 0 0 18446744073709551615 0 0 0 0 0 0 0 2147483647 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0"),
			expected: &statInfo{
				ppid:       2,
				createTime: 1606181252000,
				cpuStat: &CPUTimesStat{
					CPU:       "cpu",
					User:      0,
					System:    0,
					Timestamp: now.Unix(),
				},
			},
		},
	} {

		actual := probe.parseStatContent(tc.line, &statInfo{cpuStat: &CPUTimesStat{}}, int32(1), now)
		// nice value is fetched at the run time so we just assign the actual value for the sake for comparison
		tc.expected.nice = actual.nice
		assert.EqualValues(t, tc.expected, actual)
	}
}

func TestParseStatTestFS(t *testing.T) {
	os.Setenv("HOST_PROC", "resources/test_procfs/proc/")
	defer os.Unsetenv("HOST_PROC")

	testParseStat(t)
}

func TestParseStatLocalFS(t *testing.T) {
	// this test is flaky as the underlying procfs could change during
	// the comparison of procutil and gopsutil,
	// but we could use it to test locally
	t.Skip("flaky test in CI")
	testParseStat(t)
}

func testParseStat(t *testing.T) {
	probe := NewProcessProbe()
	defer probe.Close()

	pids, err := probe.getActivePIDs()
	assert.NoError(t, err)

	for _, pid := range pids {
		actual := probe.parseStat(filepath.Join(probe.procRootLoc, strconv.Itoa(int(pid))), pid, time.Now())
		expProc, err := process.NewProcess(pid)
		assert.NoError(t, err)
		expCreate, err := expProc.CreateTime()
		assert.NoError(t, err)
		expPpid, err := expProc.Ppid()
		assert.NoError(t, err)
		exptimes, err := expProc.Times()
		assert.NoError(t, err)
		expNice, err := expProc.Nice()
		assert.NoError(t, err)

		assert.Equal(t, expCreate, actual.createTime)
		assert.Equal(t, expPpid, actual.ppid)
		assert.Equal(t, exptimes.User, actual.cpuStat.User)
		assert.Equal(t, exptimes.System, actual.cpuStat.System)
		assert.Equal(t, expNice, actual.nice, pid)
	}
}

func TestBootTime(t *testing.T) {
	bootT, err := bootTime("resources/test_procfs/proc/")
	assert.NoError(t, err)
	assert.Equal(t, uint64(1606127264), bootT)
}

func TestBootTimeLocalFS(t *testing.T) {
	// this test doesn't work when running with other tests,
	// because bootTime is cached in gopsutil as module level variable
	// but we could use it to test locally
	t.Skip("flaky test in CI")

	probe := NewProcessProbe()
	defer probe.Close()
	expectT, err := host.BootTime()
	assert.NoError(t, err)
	assert.Equal(t, expectT, probe.bootTime)
}
