//go:build windows

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = modkernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = modkernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = modkernel32.NewProc("AssignProcessToJobObject")
	procOpenProcess              = modkernel32.NewProc("OpenProcess")
	procCloseHandle              = modkernel32.NewProc("CloseHandle")
)

const (
	jobObjectExtendedLimitInformationClass = 9
	jobObjectLimitKillOnJobClose           = 0x2000
	jobObjectLimitJobMemory                = 0x0200
)

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation  jobObjectBasicLimitInformation
	IoInfo                 ioCounters
	ProcessMemoryLimit     uintptr
	JobMemoryLimit         uintptr
	PeakProcessMemoryLimit uintptr
	PeakJobMemoryLimit     uintptr
}

func createJobObject(memoryMB int64) (syscall.Handle, error) {
	h, _, err := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return 0, fmt.Errorf("CreateJobObjectW failed: %w", err)
	}
	job := syscall.Handle(h)

	var info jobObjectExtendedLimitInformation
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	if memoryMB > 0 {
		info.BasicLimitInformation.LimitFlags |= jobObjectLimitJobMemory
		info.JobMemoryLimit = uintptr(memoryMB * 1024 * 1024)
	}

	ret, _, err := procSetInformationJobObject.Call(
		uintptr(job),
		jobObjectExtendedLimitInformationClass,
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Sizeof(info)),
	)
	if ret == 0 {
		_, _, _ = procCloseHandle.Call(uintptr(job))
		return 0, fmt.Errorf("SetInformationJobObject failed: %w", err)
	}
	return job, nil
}

func killProcessTreeWindows(pid int) {
	if pid <= 0 {
		return
	}
	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	_ = killCmd.Run()
}

func (r *execRunner) runPlatform(ctx context.Context, cfg Config, workingDir string, stdoutWriter, stderrWriter io.Writer) (*Result, error) {
	res := &Result{
		IsolationMode: "best-effort",
		IsolationWarn: BestEffortWarning,
	}

	// Prominent warning to stderr for best-effort isolation on Windows
	_, _ = io.WriteString(stderrWriter, BestEffortWarning+"\n")

	env := r.buildEnv(cfg, true)

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = workingDir
	cmd.Env = env
	cmd.Stdin = cfg.Stdin
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	job, err := createJobObject(cfg.Limits.MemoryMB)
	if err == nil && job != 0 {
		defer func() {
			_, _, _ = procCloseHandle.Call(uintptr(job))
		}()
	}

	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killProcessTreeWindows(cmd.Process.Pid)
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Start(); err != nil {
		return res, fmt.Errorf("failed to start process: %w", err)
	}

	if job != 0 && cmd.Process != nil {
		const processSetQuota = 0x0100
		const processTerminate = 0x0001
		hProc, _, _ := procOpenProcess.Call(processSetQuota|processTerminate, 0, uintptr(cmd.Process.Pid))
		if hProc != 0 {
			_, _, _ = procAssignProcessToJobObject.Call(uintptr(job), hProc)
			_, _, _ = procCloseHandle.Call(hProc)
		}
	}

	waitErr := cmd.Wait()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		res.ExitCode = -1
		return res, ErrTimeout
	}

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, waitErr
	}

	res.ExitCode = 0
	return res, nil
}
