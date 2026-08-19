//go:build windows && !legacy_walk

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/lxn/win"
)

const (
	yucomWindowTitle = "YUCOM 通用串口测试工具"
	yucomWidth       = 2400
	yucomHeight      = 1500
	dwmWindowBorderColor = 34
	dwmColorNone         = 0xfffffffe
)

var windowsDwmSetWindowAttribute = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

// runApp hosts the same HTML/CSS application used by the Linux release inside
// Edge WebView2. This keeps the Windows program native and self-contained while
// preserving the rounded blue-and-white Penpot design exactly.
func runApp() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		showWindowsStartupError("无法启动本地界面服务：" + err.Error())
		return
	}

	app := newSerialApp()
	mux := http.NewServeMux()
	app.routes(mux)
	server := &http.Server{
		Handler:           localOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()

	dataPath := ""
	if cacheRoot, cacheErr := os.UserCacheDir(); cacheErr == nil {
		dataPath = filepath.Join(cacheRoot, "YUCOM", "WebView2")
		_ = os.MkdirAll(dataPath, 0o755)
	}

	view := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  yucomWindowTitle,
			Width:  yucomWidth,
			Height: yucomHeight,
			IconId: 1,
			Center: true,
		},
	})
	if view == nil {
		_ = listener.Close()
		showWindowsStartupError("无法加载 Microsoft Edge WebView2。请安装或修复 WebView2 Runtime 后重试。")
		return
	}

	hwnd := win.HWND(uintptr(view.Window()))
	configureYUCOMWindow(hwnd)
	configureYUCOMMinimumSize(view, hwnd)
	bindYUCOMWindowActions(view, hwnd)

	pageURL := "http://" + listener.Addr().String() + "/?host=windows"
	view.Navigate(pageURL)

	windowDone := make(chan struct{})
	go func() {
		select {
		case <-app.shutdown:
			win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
		case serveErr := <-serverDone:
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
			}
		case <-windowDone:
		}
	}()

	win.ShowWindow(hwnd, win.SW_SHOW)
	view.Run()
	close(windowDone)

	app.closePort()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = server.Shutdown(ctx)
	cancel()
	view.Destroy()
}

func configureYUCOMWindow(hwnd win.HWND) {
	style := uint32(win.GetWindowLong(hwnd, win.GWL_STYLE))
	style &^= win.WS_CAPTION
	style |= win.WS_POPUP | win.WS_THICKFRAME | win.WS_SYSMENU | win.WS_MINIMIZEBOX | win.WS_MAXIMIZEBOX
	win.SetWindowLong(hwnd, win.GWL_STYLE, int32(style))
	borderColor := uint32(dwmColorNone)
	_, _, _ = windowsDwmSetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(dwmWindowBorderColor),
		uintptr(unsafe.Pointer(&borderColor)),
		unsafe.Sizeof(borderColor),
	)

	screenWidth := win.GetSystemMetrics(win.SM_CXSCREEN)
	screenHeight := win.GetSystemMetrics(win.SM_CYSCREEN)
	if screenWidth <= 0 || screenHeight <= 0 {
		screenWidth = yucomWidth
		screenHeight = yucomHeight
	}
	width := int32(yucomWidth)
	height := int32(yucomHeight)
	margin := int32(28)
	if width > screenWidth-margin {
		width = screenWidth - margin
	}
	if height > screenHeight-margin {
		height = screenHeight - margin
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	// Keep the app in a normal centered window. The WebView2 page scales its
	// layout for high-DPI monitors, while the native window remains resizable
	// instead of being forced into full-screen mode at startup.
	win.SetWindowPos(hwnd, 0, x, y, width, height, win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
}

func configureYUCOMMinimumSize(view webview2.WebView, hwnd win.HWND) {
	dpi := int(win.GetDpiForWindow(hwnd))
	if dpi <= 0 {
		dpi = 96
	}
	scale := func(value int) int { return (value*dpi + 48) / 96 }
	// Keep the two-column layout usable while allowing the user to resize the
	// window freely from every edge and corner.
	view.SetSize(scale(1000), scale(650), webview2.HintMin)
}

func bindYUCOMWindowActions(view webview2.WebView, hwnd win.HWND) {
	_ = view.Bind("yucomWindowDrag", func() {
		win.ReleaseCapture()
		win.SendMessage(hwnd, win.WM_NCLBUTTONDOWN, win.HTCAPTION, 0)
	})
	_ = view.Bind("yucomWindowMinimize", func() {
		win.ShowWindow(hwnd, win.SW_MINIMIZE)
	})
	_ = view.Bind("yucomWindowToggleMaximize", func() bool {
		if win.IsZoomed(hwnd) {
			win.ShowWindow(hwnd, win.SW_RESTORE)
			return false
		}
		win.ShowWindow(hwnd, win.SW_MAXIMIZE)
		return true
	})
	_ = view.Bind("yucomWindowClose", func() {
		win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
	})
}

func showWindowsStartupError(message string) {
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("YUCOM 启动失败")
	win.MessageBox(0, text, title, win.MB_OK|win.MB_ICONERROR)
}
