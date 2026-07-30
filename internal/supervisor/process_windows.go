//go:build windows

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type platformProcessConfig struct {
	Path   string
	Args   []string
	Env    map[string]string
	Dir    string
	Stdout io.Writer
	Stderr io.Writer
}

type windowsProcess struct {
	pid int

	mu            sync.Mutex
	processHandle windows.Handle
	threadHandle  windows.Handle
	jobHandle     windows.Handle
	pipes         []*windowsOutputPipe
	waitOnce      sync.Once
	waitDone      chan struct{}
	waitErr       error
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation windows.JOBOBJECT_BASIC_LIMIT_INFORMATION
	IoInfo                windows.IO_COUNTERS
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

const createSuspended = 0x00000004

var (
	modkernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procIsProcessInJob           = modkernel32.NewProc("IsProcessInJob")
	procSetInformationJobObject  = modkernel32.NewProc("SetInformationJobObject")
	procTerminateJobObject       = modkernel32.NewProc("TerminateJobObject")
	procGenerateConsoleCtrlEvent = modkernel32.NewProc("GenerateConsoleCtrlEvent")
	assignProcessToJobObject     = windows.AssignProcessToJobObject
)

func startPlatformProcess(ctx context.Context, config platformProcessConfig) (*windowsProcess, error) {
	if config.Path == "" {
		return nil, errors.New("start process: missing executable path")
	}
	cmdline := windows.StringToUTF16Ptr(syscall.EscapeArg(config.Path) + commandLineArgs(config.Args))
	app, err := windows.UTF16PtrFromString(config.Path)
	if err != nil {
		return nil, fmt.Errorf("start process: executable path: %w", err)
	}
	envBlock, err := createEnvironmentBlock(mergedEnvironment(config.Env))
	if err != nil {
		return nil, fmt.Errorf("start process: environment block: %w", err)
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	cleanupJob := true
	defer func() {
		if cleanupJob {
			_ = windows.CloseHandle(job)
		}
	}()
	if err := setJobObjectKillOnClose(job); err != nil {
		return nil, err
	}

	stdoutPipe, err := newWindowsOutputPipe(config.Stdout)
	if err != nil {
		return nil, fmt.Errorf("start process: stdout pipe: %w", err)
	}
	defer func() { stdoutPipe.closeOnStartFailure() }()
	stderrPipe, err := newWindowsOutputPipe(config.Stderr)
	if err != nil {
		return nil, fmt.Errorf("start process: stderr pipe: %w", err)
	}
	defer func() { stderrPipe.closeOnStartFailure() }()

	var startup windows.StartupInfo
	var processInfo windows.ProcessInformation
	startup.Cb = uint32(unsafe.Sizeof(startup))
	creationFlags := uint32(createSuspended | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_PROCESS_GROUP)
	inheritHandles := false
	if stdoutPipe != nil || stderrPipe != nil {
		startup.Flags |= windows.STARTF_USESTDHANDLES
		startup.StdOutput = windows.Stdout
		startup.StdErr = windows.Stderr
		if stdoutPipe != nil {
			startup.StdOutput = stdoutPipe.childWrite
			inheritHandles = true
		}
		if stderrPipe != nil {
			startup.StdErr = stderrPipe.childWrite
			inheritHandles = true
		}
	}
	var dirPtr *uint16
	if config.Dir != "" {
		dirPtr, err = windows.UTF16PtrFromString(config.Dir)
		if err != nil {
			return nil, fmt.Errorf("start process: working directory: %w", err)
		}
	}
	if err := windows.CreateProcess(app, cmdline, nil, nil, inheritHandles, creationFlags, &envBlock[0], dirPtr, &startup, &processInfo); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}
	proc := &windowsProcess{
		pid:           int(processInfo.ProcessId),
		processHandle: processInfo.Process,
		threadHandle:  processInfo.Thread,
		jobHandle:     job,
		pipes:         compactWindowsOutputPipes(stdoutPipe, stderrPipe),
		waitDone:      make(chan struct{}),
	}
	stdoutPipe = nil
	stderrPipe = nil
	cleanupJob = false
	assigned := false
	resumed := false
	defer func() {
		if !assigned {
			_ = windows.TerminateProcess(processInfo.Process, 1)
		} else if !resumed {
			_ = proc.Kill()
		}
		if !resumed || !assigned {
			_ = proc.Close()
		}
	}()

	if err := assignProcessToJobObject(job, processInfo.Process); err != nil {
		return nil, fmt.Errorf("assign process %d to job object: %w", proc.pid, err)
	}
	assigned = true
	if _, err := windows.ResumeThread(processInfo.Thread); err != nil {
		return nil, fmt.Errorf("resume process %d: %w", proc.pid, err)
	}
	resumed = true
	proc.startOutputCopy()
	proc.startWait()

	select {
	case <-ctx.Done():
		_ = proc.Kill()
		_ = proc.Close()
		return nil, fmt.Errorf("start process: %w", ctx.Err())
	default:
	}
	return proc, nil
}

func (p *windowsProcess) PID() int {
	if p == nil {
		return 0
	}
	return p.pid
}

func (p *windowsProcess) Wait(ctx context.Context) error {
	if p == nil {
		return os.ErrInvalid
	}
	p.startWait()
	select {
	case <-p.waitDone:
		return p.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *windowsProcess) Stop(ctx context.Context) error {
	if p == nil {
		return os.ErrInvalid
	}
	if processHandle := p.currentProcessHandle(); processHandle != 0 {
		_, _, _ = procGenerateConsoleCtrlEvent.Call(windows.CTRL_BREAK_EVENT, uintptr(p.pid))
	}
	select {
	case <-p.waitDone:
		return p.waitErr
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
		return p.Kill()
	}
}

func (p *windowsProcess) Kill() error {
	if p == nil {
		return os.ErrInvalid
	}
	job := p.currentJobHandle()
	if job == 0 {
		return os.ErrProcessDone
	}
	if err := terminateJobObject(job, 1); err != nil && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return fmt.Errorf("terminate job for process %d: %w", p.pid, err)
	}
	return nil
}

func (p *windowsProcess) Close() error {
	if p == nil {
		return os.ErrInvalid
	}
	if p.currentJobHandle() != 0 {
		select {
		case <-p.waitDone:
		default:
			_ = p.Kill()
			select {
			case <-p.waitDone:
			case <-time.After(3 * time.Second):
			}
		}
	}

	p.mu.Lock()
	processHandle := p.processHandle
	threadHandle := p.threadHandle
	jobHandle := p.jobHandle
	pipes := p.pipes
	p.processHandle = 0
	p.threadHandle = 0
	p.jobHandle = 0
	p.pipes = nil
	p.mu.Unlock()

	var closeErr error
	for _, pipe := range pipes {
		if err := pipe.close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	for _, handle := range []windows.Handle{threadHandle, processHandle, jobHandle} {
		if handle == 0 {
			continue
		}
		if err := windows.CloseHandle(handle); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (p *windowsProcess) startOutputCopy() {
	p.mu.Lock()
	pipes := append([]*windowsOutputPipe(nil), p.pipes...)
	p.mu.Unlock()
	for _, pipe := range pipes {
		pipe.startCopy()
	}
}

func (p *windowsProcess) startWait() {
	p.waitOnce.Do(func() {
		go func() {
			handle, err := p.duplicateProcessHandle()
			if err != nil {
				p.waitErr = err
				close(p.waitDone)
				return
			}
			if handle == 0 {
				p.waitErr = os.ErrProcessDone
				close(p.waitDone)
				return
			}
			defer windows.CloseHandle(handle)
			_, err = windows.WaitForSingleObject(handle, windows.INFINITE)
			p.waitErr = err
			close(p.waitDone)
		}()
	})
}

func (p *windowsProcess) duplicateProcessHandle() (windows.Handle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.processHandle == 0 {
		return 0, nil
	}
	currentProcess, err := windows.GetCurrentProcess()
	if err != nil {
		return 0, err
	}
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(currentProcess, p.processHandle, currentProcess, &duplicate, windows.SYNCHRONIZE, false, 0); err != nil {
		return 0, err
	}
	return duplicate, nil
}

func (p *windowsProcess) currentProcessHandle() windows.Handle {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.processHandle
}

func (p *windowsProcess) currentJobHandle() windows.Handle {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.jobHandle
}

func setJobObjectKillOnClose(job windows.Handle) error {
	info := jobObjectExtendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if err := setInformationJobObject(job, windows.JobObjectExtendedLimitInformation, unsafe.Pointer(&info), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("configure job object: %w", err)
	}
	return nil
}

func setInformationJobObject(job windows.Handle, infoClass uint32, info unsafe.Pointer, infoLen uint32) error {
	r1, _, err := procSetInformationJobObject.Call(uintptr(job), uintptr(infoClass), uintptr(info), uintptr(infoLen))
	if r1 == 0 {
		if err != syscall.Errno(0) {
			return err
		}
		return windows.GetLastError()
	}
	return nil
}

type windowsOutputPipe struct {
	reader     *os.File
	childWrite windows.Handle
	writer     io.Writer
	once       sync.Once
	done       chan struct{}
}

func newWindowsOutputPipe(writer io.Writer) (*windowsOutputPipe, error) {
	if writer == nil {
		return nil, nil
	}
	var readHandle windows.Handle
	var writeHandle windows.Handle
	var sa windows.SecurityAttributes
	sa.Length = uint32(unsafe.Sizeof(sa))
	sa.InheritHandle = 1
	if err := windows.CreatePipe(&readHandle, &writeHandle, &sa, 0); err != nil {
		return nil, err
	}
	if err := windows.SetHandleInformation(readHandle, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		_ = windows.CloseHandle(readHandle)
		_ = windows.CloseHandle(writeHandle)
		return nil, err
	}
	return &windowsOutputPipe{reader: os.NewFile(uintptr(readHandle), "gowa-manager-output-pipe"), childWrite: writeHandle, writer: writer, done: make(chan struct{})}, nil
}

func compactWindowsOutputPipes(pipes ...*windowsOutputPipe) []*windowsOutputPipe {
	out := make([]*windowsOutputPipe, 0, len(pipes))
	for _, pipe := range pipes {
		if pipe != nil {
			out = append(out, pipe)
		}
	}
	return out
}

func (p *windowsOutputPipe) startCopy() {
	if p == nil {
		return
	}
	_ = windows.CloseHandle(p.childWrite)
	p.childWrite = 0
	go func() {
		_, _ = io.Copy(p.writer, p.reader)
		close(p.done)
	}()
}

func (p *windowsOutputPipe) closeOnStartFailure() {
	if p == nil {
		return
	}
	if p.childWrite != 0 {
		_ = windows.CloseHandle(p.childWrite)
		p.childWrite = 0
	}
	if p.reader != nil {
		_ = p.reader.Close()
	}
}

func (p *windowsOutputPipe) close() error {
	if p == nil {
		return nil
	}
	var err error
	p.once.Do(func() {
		if p.childWrite != 0 {
			if closeErr := windows.CloseHandle(p.childWrite); closeErr != nil && err == nil {
				err = closeErr
			}
			p.childWrite = 0
		}
		if p.reader != nil {
			if closeErr := p.reader.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}
		select {
		case <-p.done:
		case <-time.After(time.Second):
		}
	})
	return err
}

func terminateJobObject(job windows.Handle, exitCode uint32) error {
	r1, _, err := procTerminateJobObject.Call(uintptr(job), uintptr(exitCode))
	if r1 == 0 {
		if err != syscall.Errno(0) {
			return err
		}
		return windows.GetLastError()
	}
	return nil
}

func commandLineArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	escaped := make([]string, 0, len(args))
	for _, arg := range args {
		escaped = append(escaped, syscall.EscapeArg(arg))
	}
	return " " + strings.Join(escaped, " ")
}

func mergedEnvironment(env map[string]string) []string {
	return mergeEnvironment(os.Environ(), env)
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		folded := strings.ToUpper(key)
		values[folded] = entry
	}
	for key, value := range overrides {
		folded := strings.ToUpper(key)
		values[folded] = key + "=" + value
	}
	keys := make([]string, 0, len(values))
	for folded := range values {
		keys = append(keys, folded)
	}
	sort.Strings(keys)
	merged := make([]string, 0, len(keys))
	for _, folded := range keys {
		merged = append(merged, values[folded])
	}
	return merged
}

func createEnvironmentBlock(env []string) ([]uint16, error) {
	sort.Strings(env)
	block := make([]uint16, 0)
	for _, entry := range env {
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, err
		}
		block = append(block, encoded...)
	}
	block = append(block, 0)
	return block, nil
}
