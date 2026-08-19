//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"yucom/internal/serialcore"
)

const (
	windowsGenericRead       = 0x80000000
	windowsGenericWrite      = 0x40000000
	windowsOpenExisting      = 3
	windowsFileAttributeNorm = 0x80
	windowsInvalidHandle     = ^uintptr(0)

	windowsDTRControlEnable = 1
	windowsRTSControlEnable = 1

	windowsMSCTSOn  = 0x0010
	windowsMSDSROn  = 0x0020
	windowsMSRingOn = 0x0040
	windowsMSRLSDOn = 0x0080
)

var windowsKernel32 = syscall.NewLazyDLL("kernel32.dll")
var (
	windowsCreateFileW        = windowsKernel32.NewProc("CreateFileW")
	windowsGetCommState       = windowsKernel32.NewProc("GetCommState")
	windowsSetCommState       = windowsKernel32.NewProc("SetCommState")
	windowsSetCommTimeouts    = windowsKernel32.NewProc("SetCommTimeouts")
	windowsReadFile           = windowsKernel32.NewProc("ReadFile")
	windowsGetCommModemStatus = windowsKernel32.NewProc("GetCommModemStatus")
	windowsQueryDosDeviceW    = windowsKernel32.NewProc("QueryDosDeviceW")
)

type windowsDCB struct {
	DCBlength uint32
	BaudRate  uint32
	Flags     uint32
	Reserved  uint16
	XonLim    uint16
	XoffLim   uint16
	ByteSize  byte
	Parity    byte
	StopBits  byte
	XonChar   int8
	XoffChar  int8
	ErrorChar int8
	EofChar   int8
	EvtChar   int8
	Reserved1 uint16
}

type windowsCommTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

func openPortHandle(device string) (*os.File, error) {
	path, err := syscall.UTF16PtrFromString("\\\\.\\" + device)
	if err != nil {
		return nil, err
	}
	result, _, callErr := windowsCreateFileW.Call(
		uintptr(unsafe.Pointer(path)), windowsGenericRead|windowsGenericWrite,
		0, 0, windowsOpenExisting, windowsFileAttributeNorm, 0,
	)
	if result == windowsInvalidHandle {
		return nil, callErr
	}
	return os.NewFile(result, device), nil
}

func (a *serialApp) openPort(cfg serialConfig) error {
	file, err := openPortHandle(cfg.Device)
	if err != nil {
		return fmt.Errorf("无法打开 %s：%w", cfg.Device, err)
	}
	if err := configureSerial(file.Fd(), cfg); err != nil {
		_ = file.Close()
		return fmt.Errorf("设置串口失败：%w", err)
	}
	a.mu.Lock()
	oldFile := a.file
	a.generation++
	generation := a.generation
	a.file = file
	a.config = cfg
	a.lastError = ""
	a.startedAt = time.Now()
	a.mu.Unlock()
	if oldFile != nil {
		_ = oldFile.Close()
	}
	a.publish(serialEvent{Type: "state", Text: "串口已打开：" + cfg.Device})
	go a.readLoop(file, generation)
	return nil
}

func (a *serialApp) closePort() {
	a.mu.Lock()
	file := a.file
	device := a.config.Device
	a.file = nil
	a.generation++
	a.startedAt = time.Time{}
	if a.testDone != nil && !a.testCapture.Matched {
		close(a.testDone)
		a.testDone = nil
	}
	a.mu.Unlock()
	if file != nil {
		_ = file.Close()
		a.publish(serialEvent{Type: "state", Text: "串口已关闭：" + device})
	}
}

func windowsRead(file *os.File, buffer []byte) (int, error) {
	var read uint32
	result, _, callErr := windowsReadFile.Call(
		file.Fd(), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&read)), 0,
	)
	if result == 0 {
		return int(read), callErr
	}
	return int(read), nil
}

func (a *serialApp) readLoop(file *os.File, generation uint64) {
	buffer := make([]byte, 4096)
	for {
		n, err := windowsRead(file, buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			a.mu.Lock()
			if a.generation != generation || a.file != file {
				a.mu.Unlock()
				return
			}
			a.rxCount += uint64(n)
			if len(a.testCapture.Expected) > 0 && a.testCapture.Feed(chunk) && a.testDone != nil {
				close(a.testDone)
			}
			a.mu.Unlock()
			a.publish(serialEvent{Type: "data", Data: base64.StdEncoding.EncodeToString(chunk)})
		}
		if err != nil {
			a.mu.Lock()
			current := a.generation == generation && a.file == file
			if current {
				a.lastError = err.Error()
				a.file = nil
				a.generation++
				if a.testDone != nil && !a.testCapture.Matched {
					close(a.testDone)
					a.testDone = nil
				}
			}
			a.mu.Unlock()
			if current {
				a.publish(serialEvent{Type: "error", Text: "串口读取停止：" + err.Error()})
			}
			return
		}
	}
}

func configureSerial(fd uintptr, cfg serialConfig) error {
	var state windowsDCB
	state.DCBlength = uint32(unsafe.Sizeof(state))
	if result, _, err := windowsGetCommState.Call(fd, uintptr(unsafe.Pointer(&state))); result == 0 {
		return err
	}
	state.BaudRate = uint32(cfg.Baud)
	state.Flags = 1
	if cfg.Parity != "none" {
		state.Flags |= 1 << 1
	}
	if cfg.Flow == "software" {
		state.Flags |= (1 << 9) | (1 << 10)
	} else if cfg.Flow == "hardware" {
		state.Flags |= 1 << 4
		state.Flags |= uint32(windowsRTSControlEnable) << 13
		state.Flags |= uint32(windowsDTRControlEnable) << 5
	}
	state.ByteSize = byte(cfg.DataBits)
	switch cfg.Parity {
	case "odd":
		state.Parity = 1
	case "even":
		state.Parity = 2
	default:
		state.Parity = 0
	}
	if cfg.StopBits == 2 {
		state.StopBits = 2
	}
	if result, _, err := windowsSetCommState.Call(fd, uintptr(unsafe.Pointer(&state))); result == 0 {
		return err
	}
	timeouts := windowsCommTimeouts{ReadTotalTimeoutConstant: 100}
	if result, _, err := windowsSetCommTimeouts.Call(fd, uintptr(unsafe.Pointer(&timeouts))); result == 0 {
		return err
	}
	return nil
}

func modemSignals(fd uintptr) map[string]bool {
	result := map[string]bool{"CTS": false, "DSR": false, "DCD": false, "RI": false, "RTS": false, "DTR": false}
	var bits uint32
	if resultCode, _, _ := windowsGetCommModemStatus.Call(fd, uintptr(unsafe.Pointer(&bits))); resultCode == 0 {
		return result
	}
	result["CTS"] = bits&windowsMSCTSOn != 0
	result["DSR"] = bits&windowsMSDSROn != 0
	result["DCD"] = bits&windowsMSRLSDOn != 0
	result["RI"] = bits&windowsMSRingOn != 0
	return result
}

func queryDosDevice(device string) (string, error) {
	name, err := syscall.UTF16PtrFromString(device)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, 4096)
	result, _, callErr := windowsQueryDosDeviceW.Call(
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)),
	)
	if result == 0 {
		return "", callErr
	}
	return syscall.UTF16ToString(buffer[:result]), nil
}

func enumeratePorts() []portInfo {
	ports := make([]portInfo, 0, 16)
	for number := 1; number <= 256; number++ {
		device := "COM" + strconv.Itoa(number)
		if target, err := queryDosDevice(device); err == nil {
			group := "Windows串口"
			if strings.Contains(strings.ToLower(target), "usb") {
				group = "USB转串口"
			}
			ports = append(ports, portInfo{Device: device, Group: group})
		}
	}
	return ports
}

func validateConfig(cfg serialConfig) error {
	device := strings.ToUpper(strings.TrimSpace(cfg.Device))
	if !strings.HasPrefix(device, "COM") {
		return errors.New("Windows串口名称无效，应为 COM1、COM2 等")
	}
	number, err := strconv.Atoi(strings.TrimPrefix(device, "COM"))
	if err != nil || number < 1 || number > 256 || device != cfg.Device {
		return errors.New("Windows串口名称无效，应为 COM1、COM2 等")
	}
	return serialcore.ValidateConfigValues(cfg)
}
