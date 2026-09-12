//go:build windows

package sandwicher

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// AttachToOwnedJob puts the CLI (and every child it spawns: ffmpeg, ffprobe)
// into a Kill-on-close Job Object. Effects:
//  1. Process managers can attribute the whole chain to the app.
//  2. If the owning Electron UI dies for any reason (crash, force quit,
//     process kill), the kernel closes the job handle and reaps ffmpeg too —
//     no orphaned encoders left burning CPU on a network drive.
func AttachToOwnedJob() error {
	// Named so process-tree tools can attribute the chain.
	jobName := fmt.Sprintf("subsandwicher-job-%d", windows.GetCurrentProcessId())
	jobNamePtr, err := windows.UTF16PtrFromString(jobName)
	if err != nil {
		return fmt.Errorf("job name utf16: %w", err)
	}
	job, err := windows.CreateJobObject(nil, jobNamePtr)
	if err != nil {
		return fmt.Errorf("CreateJobObject: %w", err)
	}
	info := windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
		LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
	}
	ext := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: info,
	}
	if _, err := windows.SetInformationJobObject(windows.Handle(job),
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&ext)),
		uint32(unsafe.Sizeof(ext))); err != nil {
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}
	cur, err := windows.GetCurrentProcess()
	if err != nil {
		return fmt.Errorf("GetCurrentProcess: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, cur); err != nil {
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return nil
}
