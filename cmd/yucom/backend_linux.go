//go:build linux

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"yucom/internal/serialcore"
)

// CRTSCTS is defined by Linux asm-generic/termbits.h but is not exported by
// Go's syscall package on every architecture (including linux/arm64).
const linuxCRTSCTS uint32 = 0x80000000

func runWebApp() {
	listenAddress := flag.String("listen", "127.0.0.1:0", "HTTP listen address; keep 127.0.0.1 for local-only access")
	noOpen := flag.Bool("no-open", false, "do not open the default browser")
	flag.Parse()

	if !strings.HasPrefix(*listenAddress, "127.0.0.1:") && !strings.HasPrefix(*listenAddress, "localhost:") {
		log.Fatal("拒绝监听非本机地址；请使用 127.0.0.1:端口")
	}

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		log.Fatalf("启动本地页面失败: %v", err)
	}

	app := newSerialApp()
	mux := http.NewServeMux()
	app.routes(mux)
	server := &http.Server{
		Handler:           localOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	pageURL := "http://" + listener.Addr().String()
	fmt.Println("YUCOM 已启动")
	fmt.Println("本机页面:", pageURL)
	fmt.Println("关闭方式: 点击页面右上角“退出程序”，或在本终端按 Ctrl+C")
	if !*noOpen {
		if err := openBrowser(pageURL); err != nil {
			fmt.Println("未能自动打开浏览器，请手动访问:", pageURL)
		}
	}

	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	select {
	case <-app.shutdown:
		app.closePort()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	case err := <-serveDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("页面服务异常退出: %v", err)
		}
	}
}

func (a *serialApp) openPort(cfg serialConfig) error {
	file, err := os.OpenFile(cfg.Device, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("无法打开 %s：%w", cfg.Device, err)
	}
	if err := configureSerial(file.Fd(), cfg); err != nil {
		_ = file.Close()
		return fmt.Errorf("设置串口失败：%w", err)
	}
	if err := syscall.SetNonblock(int(file.Fd()), false); err != nil {
		_ = file.Close()
		return fmt.Errorf("设置串口阻塞模式失败：%w", err)
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

func (a *serialApp) readLoop(file *os.File, generation uint64) {
	buffer := make([]byte, 4096)
	for {
		n, err := file.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			a.mu.Lock()
			if a.generation != generation || a.file != file {
				a.mu.Unlock()
				return
			}
			a.rxCount += uint64(n)
			if len(a.testCapture.Expected) > 0 {
				if a.testCapture.Feed(chunk) && a.testDone != nil {
					close(a.testDone)
				}
			}
			a.mu.Unlock()
			a.publish(serialEvent{Type: "data", Data: base64.StdEncoding.EncodeToString(chunk)})
		}
		if err != nil {
			if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
				continue
			}
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
	var termios syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, uintptr(unsafe.Pointer(&termios))); err != nil {
		return err
	}
	speed, _ := baudConstant(cfg.Baud)
	termios.Iflag = 0
	termios.Oflag = 0
	termios.Lflag = 0
	termios.Cflag = uint32(syscall.CLOCAL | syscall.CREAD | speed)
	switch cfg.DataBits {
	case 5:
		termios.Cflag |= syscall.CS5
	case 6:
		termios.Cflag |= syscall.CS6
	case 7:
		termios.Cflag |= syscall.CS7
	case 8:
		termios.Cflag |= syscall.CS8
	}
	if cfg.StopBits == 2 {
		termios.Cflag |= syscall.CSTOPB
	}
	if cfg.Parity == "even" {
		termios.Cflag |= syscall.PARENB
		termios.Iflag |= syscall.INPCK
	} else if cfg.Parity == "odd" {
		termios.Cflag |= syscall.PARENB | syscall.PARODD
		termios.Iflag |= syscall.INPCK
	}
	if cfg.Flow == "software" {
		termios.Iflag |= syscall.IXON | syscall.IXOFF
	} else if cfg.Flow == "hardware" {
		termios.Cflag |= linuxCRTSCTS
	}
	termios.Cc[syscall.VMIN] = 0
	termios.Cc[syscall.VTIME] = 1
	termios.Ispeed = speed
	termios.Ospeed = speed
	return ioctl(fd, syscall.TCSETS, uintptr(unsafe.Pointer(&termios)))
}

func baudConstant(baud int) (uint32, bool) {
	speeds := map[int]uint32{
		300: syscall.B300, 600: syscall.B600,
		1200: syscall.B1200, 2400: syscall.B2400, 4800: syscall.B4800,
		9600: syscall.B9600, 19200: syscall.B19200, 38400: syscall.B38400,
		57600: syscall.B57600, 115200: syscall.B115200, 230400: syscall.B230400,
		460800: syscall.B460800, 921600: syscall.B921600,
	}
	speed, ok := speeds[baud]
	return speed, ok
}

func modemSignals(fd uintptr) map[string]bool {
	result := map[string]bool{"CTS": false, "DSR": false, "DCD": false, "RI": false, "RTS": false, "DTR": false}
	var bits int
	if err := ioctl(fd, syscall.TIOCMGET, uintptr(unsafe.Pointer(&bits))); err != nil {
		return result
	}
	result["CTS"] = bits&syscall.TIOCM_CTS != 0
	result["DSR"] = bits&syscall.TIOCM_DSR != 0
	result["DCD"] = bits&syscall.TIOCM_CAR != 0
	result["RI"] = bits&syscall.TIOCM_RI != 0
	result["RTS"] = bits&syscall.TIOCM_RTS != 0
	result["DTR"] = bits&syscall.TIOCM_DTR != 0
	return result
}

func ioctl(fd uintptr, request uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

func enumeratePorts() []portInfo {
	var ports []portInfo
	seen := make(map[string]bool)
	patterns := []struct {
		glob  string
		group string
	}{
		{"/dev/serial/by-id/*", "稳定设备标识"},
		{"/dev/ttyUSB*", "USB转串口"},
		{"/dev/ttyACM*", "USB CDC/ACM串口"},
		{"/dev/ttyXRUSB*", "多串口扩展设备"},
		{"/dev/ttyS*", "板载/PCIe串口"},
		{"/dev/ttyAMA*", "ARM板载串口"},
		{"/dev/ttyTHS*", "NVIDIA板载串口"},
		{"/dev/ttymxc*", "NXP板载串口"},
		{"/dev/ttySC*", "SuperH串口"},
		{"/dev/tty*", "其他串口设备"},
	}
	for _, item := range patterns {
		matches, _ := filepath.Glob(item.glob)
		sort.Strings(matches)
		for _, device := range matches {
			if seen[device] || serialcore.ShouldSkipEnumeratedDevice(device) {
				continue
			}
			if _, err := os.Stat(device); err != nil {
				continue
			}
			seen[device] = true
			ports = append(ports, portInfo{Device: device, Group: item.group})
		}
	}
	return ports
}

func validateConfig(cfg serialConfig) error {
	return serialcore.ValidateConfig(cfg)
}

func openBrowser(pageURL string) error {
	return exec.Command("xdg-open", pageURL).Start()
}
