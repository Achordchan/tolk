//go:build windows

package winsel

import (
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shcore   = windows.NewLazySystemDLL("shcore.dll")
	version  = windows.NewLazySystemDLL("version.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")

	pGetForegroundWindow     = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadPID      = user32.NewProc("GetWindowThreadProcessId")
	pGetGUIThreadInfo        = user32.NewProc("GetGUIThreadInfo")
	pClientToScreen          = user32.NewProc("ClientToScreen")
	pGetCursorPos            = user32.NewProc("GetCursorPos")
	pGetAsyncKeyState        = user32.NewProc("GetAsyncKeyState")
	pSendInput               = user32.NewProc("SendInput")
	pOpenClipboard           = user32.NewProc("OpenClipboard")
	pCloseClipboard          = user32.NewProc("CloseClipboard")
	pEmptyClipboard          = user32.NewProc("EmptyClipboard")
	pGetClipboardData        = user32.NewProc("GetClipboardData")
	pSetClipboardData        = user32.NewProc("SetClipboardData")
	pEnumClipboardFormats    = user32.NewProc("EnumClipboardFormats")
	pRegisterClipboardFormat = user32.NewProc("RegisterClipboardFormatW")
	pGetClipboardSeq         = user32.NewProc("GetClipboardSequenceNumber")
	pIsClipboardFormatAvail  = user32.NewProc("IsClipboardFormatAvailable")
	pFindWindow              = user32.NewProc("FindWindowW")
	pGetWindowLongPtr        = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtr        = user32.NewProc("SetWindowLongPtrW")
	pSetWindowPos            = user32.NewProc("SetWindowPos")
	pShowWindow              = user32.NewProc("ShowWindow")
	pGetWindowRect           = user32.NewProc("GetWindowRect")
	pIsWindowVisible         = user32.NewProc("IsWindowVisible")
	pIsWindow                = user32.NewProc("IsWindow")
	pMonitorFromPoint        = user32.NewProc("MonitorFromPoint")
	pGetMonitorInfo          = user32.NewProc("GetMonitorInfoW")
	pGetDpiForWindow         = user32.NewProc("GetDpiForWindow")
	pSetWindowsHookEx        = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx     = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx          = user32.NewProc("CallNextHookEx")
	pGetMessage              = user32.NewProc("GetMessageW")
	pPostThreadMessage       = user32.NewProc("PostThreadMessageW")
	pGetDoubleClickTime      = user32.NewProc("GetDoubleClickTime")
	pGetAncestor             = user32.NewProc("GetAncestor")
	pCreateWindowEx          = user32.NewProc("CreateWindowExW")
	pDispatchMessage         = user32.NewProc("DispatchMessageW")

	pCopyEnhMetaFile   = gdi32.NewProc("CopyEnhMetaFileW")
	pDeleteEnhMetaFile = gdi32.NewProc("DeleteEnhMetaFile")

	pGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	pGlobalFree   = kernel32.NewProc("GlobalFree")
	pGlobalLock   = kernel32.NewProc("GlobalLock")
	pGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	pGlobalSize   = kernel32.NewProc("GlobalSize")
	pGetThreadID  = kernel32.NewProc("GetCurrentThreadId")

	pGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")

	pGetFileVersionInfoSize = version.NewProc("GetFileVersionInfoSizeW")
	pGetFileVersionInfo     = version.NewProc("GetFileVersionInfoW")
	pVerQueryValue          = version.NewProc("VerQueryValueW")

	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoUninitialize   = ole32.NewProc("CoUninitialize")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")

	pSysFreeString        = oleaut32.NewProc("SysFreeString")
	pVariantClear         = oleaut32.NewProc("VariantClear")
	pSafeArrayGetLBound   = oleaut32.NewProc("SafeArrayGetLBound")
	pSafeArrayGetUBound   = oleaut32.NewProc("SafeArrayGetUBound")
	pSafeArrayAccessData  = oleaut32.NewProc("SafeArrayAccessData")
	pSafeArrayUnaccessDat = oleaut32.NewProc("SafeArrayUnaccessData")
	pSafeArrayDestroy     = oleaut32.NewProc("SafeArrayDestroy")
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// keep holds the most recent pointers handed to Win32 as plain integers.
// Storing a pointer here makes what it points to live on the heap, where the
// garbage collector neither moves it (a goroutine stack can move whenever a
// function is called) nor frees it until 64 more pointers have come through.
var (
	keep  [64]unsafe.Pointer
	keepN atomic.Uint32
)

// ptr is the address of *p for a Win32 call.
func ptr[T any](p *T) uintptr {
	atomic.StorePointer(&keep[keepN.Add(1)%uint32(len(keep))], unsafe.Pointer(p))
	return uintptr(unsafe.Pointer(p))
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

// at turns an address Win32 or COM gave us into a pointer. go vet cannot tell
// that these addresses are not Go memory, hence the indirection.
func at(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }
