//go:build windows && legacy_walk

package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"yucom/internal/serialcore"
)

var nativeBaudOptions = []string{
	"921600", "460800", "230400", "115200", "57600", "38400", "19200",
	"9600", "4800", "2400", "1200", "600", "300",
}

var nativeDataBitsOptions = []string{"8", "7", "6", "5"}
var nativeStopBitsOptions = []string{"1", "2"}
var nativeParityOptions = []string{"无校验", "奇校验", "偶校验"}
var nativeFlowOptions = []string{"无流控", "软件流控", "硬件流控"}
var nativeNewlineOptions = []string{"无结束符", "CRLF", "CR", "LF"}

func newYUCOMIcon() *walk.Icon {
	icon, err := walk.NewIconFromImage(newYUCOMLogoImage(32))
	if err != nil {
		return nil
	}
	return icon
}

// ensureNativeControlBorder adds a DPI-aware non-client frame around native
// controls. On recent Windows light themes the top highlight can be almost
// white, which makes buttons and combo boxes look as if their top edge is
// missing against a white card.
func ensureNativeControlBorder(widget walk.Widget) {
	if widget == nil {
		return
	}

	hwnd := widget.Handle()
	style := uint32(win.GetWindowLong(hwnd, win.GWL_STYLE))
	if style&win.WS_BORDER == 0 {
		win.SetWindowLong(hwnd, win.GWL_STYLE, int32(style|win.WS_BORDER))
	}
	win.SetWindowPos(
		hwnd,
		0,
		0,
		0,
		0,
		0,
		win.SWP_FRAMECHANGED|win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE,
	)
}

// runApp starts the Windows build as a native Win32 window. It deliberately
// does not start a web server or invoke a browser.
func runApp() {
	app := newSerialApp()
	appIcon := newYUCOMIcon()
	if appIcon != nil {
		defer appIcon.Dispose()
	}
	brandBitmap, brandBitmapErr := walk.NewBitmapFromImage(newYUCOMLogoImage(36))
	if brandBitmapErr != nil {
		brandBitmap = nil
	} else {
		defer brandBitmap.Dispose()
	}

	var mw *walk.MainWindow
	var portCombo, baudCombo, dataBitsCombo, stopBitsCombo, parityCombo, flowCombo, newlineCombo, intervalCombo *walk.ComboBox
	var receiveEdit, sendEdit *walk.TextEdit
	var hexReceive, hexSend, timedSend, pauseReceive *walk.CheckBox
	var dtrCheck, dsrCheck, rtsCheck, ctsCheck, dcdCheck, riCheck *walk.CheckBox
	var openButton, refreshButton, sendButton, frameButton, loopbackButton, clearReceiveButton, clearCounterButton *walk.PushButton
	var connectionDot, connectionLabel, statusLabel, txCounterLabel, rxCounterLabel, modemLabel, resultLabel *walk.Label
	var statusBarItem *walk.StatusBarItem

	var closeOnce sync.Once
	done := make(chan struct{})
	var timedMu sync.Mutex
	var timedStop chan struct{}

	showError := func(title string, err error) {
		if err != nil && mw != nil {
			walk.MsgBox(mw, title, err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		}
	}

	setStatus := func(text string) {
		if statusLabel != nil {
			_ = statusLabel.SetText(text)
		}
		if statusBarItem != nil {
			statusBarItem.SetText(text)
		}
	}

	updateState := func() {
		if mw == nil {
			return
		}
		status := app.status()
		if status.Opened {
			_ = openButton.SetText("断开串口")
			connectionDot.SetTextColor(walk.RGB(22, 163, 74))
			connectionLabel.SetTextColor(walk.RGB(21, 128, 61))
			_ = connectionLabel.SetText("已连接 · " + status.Config.Device)
			setStatus("已打开：" + status.Config.Device)
			_ = modemLabel.SetText("实时更新")
		} else {
			_ = openButton.SetText("连接串口")
			connectionDot.SetTextColor(walk.RGB(245, 158, 11))
			connectionLabel.SetTextColor(walk.RGB(100, 116, 139))
			_ = connectionLabel.SetText("未连接")
			setStatus("未打开串口")
			_ = modemLabel.SetText("待连接")
		}
		portCombo.SetEnabled(!status.Opened)
		baudCombo.SetEnabled(!status.Opened)
		dataBitsCombo.SetEnabled(!status.Opened)
		stopBitsCombo.SetEnabled(!status.Opened)
		parityCombo.SetEnabled(!status.Opened)
		flowCombo.SetEnabled(!status.Opened)
		refreshButton.SetEnabled(!status.Opened)
		sendButton.SetEnabled(status.Opened)
		frameButton.SetEnabled(status.Opened)
		loopbackButton.SetEnabled(status.Opened)
		timedSend.SetEnabled(status.Opened)
		_ = txCounterLabel.SetText(fmt.Sprintf("%d 字节", status.TXCount))
		_ = rxCounterLabel.SetText(fmt.Sprintf("%d 字节", status.RXCount))
		dtrCheck.SetChecked(status.Modem["DTR"])
		dsrCheck.SetChecked(status.Modem["DSR"])
		rtsCheck.SetChecked(status.Modem["RTS"])
		ctsCheck.SetChecked(status.Modem["CTS"])
		dcdCheck.SetChecked(status.Modem["DCD"])
		riCheck.SetChecked(status.Modem["RI"])
	}

	refreshPorts := func() {
		if portCombo == nil {
			return
		}
		ports := enumeratePorts()
		model := make([]string, 0, len(ports))
		for _, port := range ports {
			model = append(model, port.Device)
		}
		if err := portCombo.SetModel(model); err != nil {
			showError("刷新串口失败", err)
			return
		}
		if len(model) > 0 {
			_ = portCombo.SetCurrentIndex(0)
			setStatus(fmt.Sprintf("发现 %d 个串口", len(model)))
		} else {
			setStatus("没有发现可用的 COM 串口")
		}
	}

	readConfig := func() (serialConfig, error) {
		baud, err := strconv.Atoi(strings.TrimSpace(baudCombo.Text()))
		if err != nil {
			return serialConfig{}, fmt.Errorf("波特率无效：%s", baudCombo.Text())
		}
		dataBits, err := strconv.Atoi(dataBitsCombo.Text())
		if err != nil {
			return serialConfig{}, fmt.Errorf("数据位无效：%s", dataBitsCombo.Text())
		}
		stopBits, err := strconv.Atoi(stopBitsCombo.Text())
		if err != nil {
			return serialConfig{}, fmt.Errorf("停止位无效：%s", stopBitsCombo.Text())
		}
		parity := map[string]string{"无校验": "none", "奇校验": "odd", "偶校验": "even"}[parityCombo.Text()]
		flow := map[string]string{"无流控": "none", "软件流控": "software", "硬件流控": "hardware"}[flowCombo.Text()]
		cfg := serialConfig{
			Device:   strings.TrimSpace(portCombo.Text()),
			Baud:     baud,
			DataBits: dataBits,
			StopBits: stopBits,
			Parity:   parity,
			Flow:     flow,
		}
		if err := validateConfig(cfg); err != nil {
			return serialConfig{}, err
		}
		return cfg, nil
	}

	newlineValue := func() string {
		switch newlineCombo.CurrentIndex() {
		case 1:
			return "crlf"
		case 2:
			return "cr"
		case 3:
			return "lf"
		default:
			return "none"
		}
	}

	prepareSendPayload := func() ([]byte, error) {
		format := "text"
		if hexSend.Checked() {
			format = "hex"
		}
		return serialcore.PreparePayload(sendRequest{Data: sendEdit.Text(), Format: format, Newline: newlineValue()})
	}

	stopTimedSend := func() {
		timedMu.Lock()
		stop := timedStop
		timedStop = nil
		timedMu.Unlock()
		if stop != nil {
			close(stop)
		}
		if timedSend != nil && timedSend.Checked() {
			timedSend.SetChecked(false)
		}
	}

	startTimedSend := func() {
		if !timedSend.Checked() {
			stopTimedSend()
			return
		}
		if !app.status().Opened {
			timedSend.SetChecked(false)
			showError("定时发送", fmt.Errorf("请先打开串口"))
			return
		}
		intervalMS, err := strconv.Atoi(intervalCombo.Text())
		if err != nil || intervalMS < 50 || intervalMS > 60000 {
			timedSend.SetChecked(false)
			showError("定时发送", fmt.Errorf("发送周期必须为50～60000毫秒"))
			return
		}
		payload, err := prepareSendPayload()
		if err != nil {
			timedSend.SetChecked(false)
			showError("定时发送", err)
			return
		}

		timedMu.Lock()
		oldStop := timedStop
		timedStop = nil
		timedMu.Unlock()
		if oldStop != nil {
			close(oldStop)
		}
		stop := make(chan struct{})
		timedMu.Lock()
		timedStop = stop
		timedMu.Unlock()
		setStatus(fmt.Sprintf("定时发送已启动：%d ms/次", intervalMS))

		go func(payload []byte, interval time.Duration, stop <-chan struct{}) {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if _, err := app.send(payload); err != nil {
						mw.Synchronize(func() {
							stopTimedSend()
							_ = resultLabel.SetText("定时发送已停止：" + err.Error())
							updateState()
						})
						return
					}
					mw.Synchronize(updateState)
				case <-stop:
					return
				case <-done:
					return
				}
			}
		}(append([]byte(nil), payload...), time.Duration(intervalMS)*time.Millisecond, stop)
	}

	appendReceive := func(data []byte) {
		if len(data) == 0 {
			return
		}
		if pauseReceive != nil && pauseReceive.Checked() {
			return
		}
		if hexReceive.Checked() {
			receiveEdit.AppendText(strings.ToUpper(hex.EncodeToString(data)) + " ")
		} else {
			receiveEdit.AppendText(string(data))
		}
		// Keep the native text control responsive during long-running tests.
		if receiveEdit.TextLength() > 1<<20 {
			text := receiveEdit.Text()
			if len(text) > 1<<19 {
				_ = receiveEdit.SetText(text[len(text)-1<<19:])
			}
		}
	}

	standardFrame := func() string {
		return fmt.Sprintf("YUCOM_TEST_%s_%s\r\n", time.Now().Format("20060102_150405.000"), strings.TrimSpace(portCombo.Text()))
	}

	// The native UI uses a compact diagnostics-workbench layout: one neutral
	// canvas, bordered white cards, a single blue accent, and monospaced data
	// surfaces. Native controls remain familiar and keyboard accessible.
	windowBrush := declarative.SolidColorBrush{Color: walk.RGB(243, 246, 250)}
	cardBrush := declarative.SolidColorBrush{Color: walk.RGB(255, 255, 255)}
	editBrush := declarative.SolidColorBrush{Color: walk.RGB(249, 251, 254)}
	softAccentBrush := declarative.SolidColorBrush{Color: walk.RGB(235, 243, 255)}
	softReceiveBrush := declarative.SolidColorBrush{Color: walk.RGB(240, 253, 244)}
	bodyFont := declarative.Font{Family: "Microsoft YaHei UI", PointSize: 9}
	buttonFont := declarative.Font{Family: "Microsoft YaHei UI", PointSize: 9, Bold: true}
	brandFont := declarative.Font{Family: "Segoe UI", PointSize: 15, Bold: true}
	sectionFont := declarative.Font{Family: "Microsoft YaHei UI", PointSize: 10, Bold: true}
	statFont := declarative.Font{Family: "Segoe UI", PointSize: 11, Bold: true}
	miniFont := declarative.Font{Family: "Microsoft YaHei UI", PointSize: 8}
	monoFont := declarative.Font{Family: "Consolas", PointSize: 10}
	textColor := walk.RGB(15, 23, 42)
	mutedText := walk.RGB(100, 116, 139)
	accentText := walk.RGB(37, 99, 235)
	receiveText := walk.RGB(22, 163, 74)

	ui := declarative.MainWindow{
		AssignTo:   &mw,
		Title:      "YUCOM 通用串口测试工具",
		Icon:       appIcon,
		Size:       declarative.Size{Width: 1180, Height: 760},
		MinSize:    declarative.Size{Width: 980, Height: 680},
		Font:       bodyFont,
		Background: windowBrush,
		Layout:     declarative.VBox{Spacing: 0},
		StatusBarItems: []declarative.StatusBarItem{
			{AssignTo: &statusBarItem, Text: "就绪", Width: 720},
			{Text: "YUCOM 1.2.0", Width: 100},
		},
		Children: []declarative.Widget{
			declarative.Composite{Border: true, Background: cardBrush, MinSize: declarative.Size{Height: 64}, MaxSize: declarative.Size{Height: 64}, Layout: declarative.HBox{Margins: declarative.Margins{Left: 14, Top: 9, Right: 18, Bottom: 9}, Spacing: 10}, Children: []declarative.Widget{
				declarative.ImageView{Image: brandBitmap, MinSize: declarative.Size{Width: 36, Height: 36}, MaxSize: declarative.Size{Width: 36, Height: 36}, Mode: declarative.ImageViewModeIdeal},
				declarative.Composite{Background: cardBrush, Layout: declarative.VBox{Spacing: 1}, Children: []declarative.Widget{
					declarative.Label{Text: "YUCOM", Font: brandFont, TextColor: accentText},
					declarative.Label{Text: "通用串口诊断与回环测试", Font: miniFont, TextColor: mutedText},
				}},
				declarative.HSpacer{},
				declarative.Label{AssignTo: &connectionDot, Text: "●", Font: sectionFont, TextColor: walk.RGB(245, 158, 11)},
				declarative.Label{AssignTo: &connectionLabel, Text: "未连接", Font: sectionFont, TextColor: mutedText},
				declarative.Label{Text: "·", TextColor: mutedText},
				declarative.Label{Text: "本机离线  ·  V1.2.0", Font: miniFont, TextColor: mutedText},
			}},
			declarative.Composite{Background: windowBrush, StretchFactor: 1, Layout: declarative.HBox{Margins: declarative.Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}, Spacing: 12}, Children: []declarative.Widget{
				declarative.Composite{Background: windowBrush, MinSize: declarative.Size{Width: 286}, MaxSize: declarative.Size{Width: 306}, Layout: declarative.VBox{Spacing: 10}, Children: []declarative.Widget{
					declarative.Composite{Border: true, Background: cardBrush, MinSize: declarative.Size{Height: 224}, MaxSize: declarative.Size{Height: 224}, Layout: declarative.VBox{Margins: declarative.Margins{Left: 12, Top: 8, Right: 12, Bottom: 8}, Spacing: 5}, Children: []declarative.Widget{
						declarative.Composite{Background: cardBrush, Layout: declarative.HBox{Spacing: 8}, Children: []declarative.Widget{
							declarative.Label{Text: "连接设置", Font: sectionFont, TextColor: textColor},
							declarative.HSpacer{},
							declarative.Label{Text: "默认 115200 · 8N1", Font: miniFont, TextColor: mutedText},
						}},
						declarative.HSeparator{},
						declarative.Composite{Background: cardBrush, Layout: declarative.HBox{Spacing: 6}, Children: []declarative.Widget{
							declarative.Composite{Border: true, Background: cardBrush, StretchFactor: 1, Layout: declarative.VBox{MarginsZero: true, SpacingZero: true}, Children: []declarative.Widget{
								declarative.HSeparator{},
								declarative.ComboBox{AssignTo: &portCombo, Editable: true, StretchFactor: 1},
							}},
							declarative.Composite{Border: true, Background: cardBrush, MinSize: declarative.Size{Width: 54}, MaxSize: declarative.Size{Width: 54}, Layout: declarative.VBox{MarginsZero: true, SpacingZero: true}, Children: []declarative.Widget{
								declarative.HSeparator{},
								declarative.PushButton{AssignTo: &refreshButton, Text: "刷新", StretchFactor: 1, OnClicked: func() { refreshPorts() }},
							}},
						}},
						declarative.Composite{Border: true, Background: cardBrush, MinSize: declarative.Size{Height: 34}, MaxSize: declarative.Size{Height: 34}, Layout: declarative.VBox{MarginsZero: true, SpacingZero: true}, Children: []declarative.Widget{
							declarative.HSeparator{},
							declarative.PushButton{AssignTo: &openButton, Text: "连接串口", Font: buttonFont, StretchFactor: 1, OnClicked: func() {
								if app.status().Opened {
									stopTimedSend()
									app.closePort()
									updateState()
									return
								}
								cfg, err := readConfig()
								if err != nil {
									showError("连接串口失败", err)
									return
								}
								if err := app.openPort(cfg); err != nil {
									showError("连接串口失败", err)
									return
								}
								updateState()
							}},
						}},
						declarative.Composite{Background: cardBrush, Layout: declarative.Grid{Columns: 2, Spacing: 6}, Children: []declarative.Widget{
							declarative.Label{Text: "波特率", TextColor: mutedText}, declarative.ComboBox{AssignTo: &baudCombo, Model: nativeBaudOptions, CurrentIndex: 3},
							declarative.Label{Text: "数据 / 停止", TextColor: mutedText}, declarative.Composite{Background: cardBrush, Layout: declarative.HBox{Spacing: 6}, Children: []declarative.Widget{
								declarative.ComboBox{AssignTo: &dataBitsCombo, Model: nativeDataBitsOptions, CurrentIndex: 0, StretchFactor: 1},
								declarative.ComboBox{AssignTo: &stopBitsCombo, Model: nativeStopBitsOptions, CurrentIndex: 0, StretchFactor: 1},
							}},
							declarative.Label{Text: "校验 / 流控", TextColor: mutedText}, declarative.Composite{Background: cardBrush, Layout: declarative.HBox{Spacing: 6}, Children: []declarative.Widget{
								declarative.ComboBox{AssignTo: &parityCombo, Model: nativeParityOptions, CurrentIndex: 0, StretchFactor: 1},
								declarative.ComboBox{AssignTo: &flowCombo, Model: nativeFlowOptions, CurrentIndex: 0, StretchFactor: 1},
							}},
						}},
					}},
					declarative.Composite{Border: true, Background: cardBrush, MinSize: declarative.Size{Height: 104}, MaxSize: declarative.Size{Height: 104}, Layout: declarative.VBox{Margins: declarative.Margins{Left: 12, Top: 7, Right: 12, Bottom: 7}, Spacing: 4}, Children: []declarative.Widget{
						declarative.Composite{Background: cardBrush, Layout: declarative.HBox{Spacing: 8}, Children: []declarative.Widget{
							declarative.Label{Text: "线路信号", Font: sectionFont, TextColor: textColor},
							declarative.HSpacer{},
							declarative.Label{AssignTo: &modemLabel, Text: "待连接", Font: miniFont, TextColor: mutedText},
						}},
						declarative.HSeparator{},
						declarative.Composite{Background: cardBrush, Layout: declarative.Grid{Columns: 3, Spacing: 5}, Children: []declarative.Widget{
							declarative.CheckBox{AssignTo: &dtrCheck, Text: "DTR", Enabled: false}, declarative.CheckBox{AssignTo: &dsrCheck, Text: "DSR", Enabled: false}, declarative.CheckBox{AssignTo: &rtsCheck, Text: "RTS", Enabled: false},
							declarative.CheckBox{AssignTo: &ctsCheck, Text: "CTS", Enabled: false}, declarative.CheckBox{AssignTo: &dcdCheck, Text: "DCD", Enabled: false}, declarative.CheckBox{AssignTo: &riCheck, Text: "RING", Enabled: false},
						}},
					}},
					declarative.Composite{Border: true, Background: cardBrush, MinSize: declarative.Size{Height: 122}, MaxSize: declarative.Size{Height: 122}, Layout: declarative.VBox{Margins: declarative.Margins{Left: 12, Top: 7, Right: 12, Bottom: 7}, Spacing: 4}, Children: []declarative.Widget{
						declarative.Label{Text: "流量统计", Font: sectionFont, TextColor: textColor},
						declarative.HSeparator{},
						declarative.Composite{Background: cardBrush, Layout: declarative.HBox{Spacing: 8}, Children: []declarative.Widget{
							declarative.Composite{Background: softAccentBrush, StretchFactor: 1, Layout: declarative.VBox{Margins: declarative.Margins{Left: 9, Top: 7, Right: 9, Bottom: 7}, Spacing: 2}, Children: []declarative.Widget{
								declarative.Label{Text: "TX  发送", Font: miniFont, TextColor: mutedText},
								declarative.Label{AssignTo: &txCounterLabel, Text: "0 字节", Font: statFont, TextColor: accentText},
							}},
							declarative.Composite{Background: softReceiveBrush, StretchFactor: 1, Layout: declarative.VBox{Margins: declarative.Margins{Left: 9, Top: 7, Right: 9, Bottom: 7}, Spacing: 2}, Children: []declarative.Widget{
								declarative.Label{Text: "RX  接收", Font: miniFont, TextColor: mutedText},
								declarative.Label{AssignTo: &rxCounterLabel, Text: "0 字节", Font: statFont, TextColor: receiveText},
							}},
						}},
						declarative.PushButton{AssignTo: &clearCounterButton, Text: "清零计数", OnClicked: func() { app.resetCounters(); updateState() }},
					}},
					declarative.Composite{Background: windowBrush, MinSize: declarative.Size{Height: 48}, MaxSize: declarative.Size{Height: 48}, Layout: declarative.HBox{Spacing: 0}, Children: []declarative.Widget{
						declarative.Composite{Border: true, Background: softAccentBrush, StretchFactor: 1, Layout: declarative.HBox{Margins: declarative.Margins{Left: 12, Top: 7, Right: 12, Bottom: 7}, Spacing: 8}, Children: []declarative.Widget{
							declarative.Label{Text: "当前状态", Font: miniFont, TextColor: mutedText},
							declarative.HSpacer{},
							declarative.Label{AssignTo: &statusLabel, Text: "正在初始化…", Font: sectionFont, TextColor: accentText},
						}},
					}},
					declarative.VSpacer{},
				}},
				declarative.Composite{Background: windowBrush, StretchFactor: 1, Layout: declarative.VBox{Spacing: 10}, Children: []declarative.Widget{
					declarative.Composite{Border: true, Background: cardBrush, StretchFactor: 2, Layout: declarative.VBox{Spacing: 0}, Children: []declarative.Widget{
						declarative.Composite{Background: cardBrush, MinSize: declarative.Size{Height: 42}, MaxSize: declarative.Size{Height: 42}, Layout: declarative.HBox{Margins: declarative.Margins{Left: 12, Top: 7, Right: 10, Bottom: 7}, Spacing: 10}, Children: []declarative.Widget{
							declarative.Label{Text: "↓", Font: sectionFont, TextColor: receiveText},
							declarative.Label{Text: "接收监视器", Font: sectionFont, TextColor: textColor},
							declarative.Label{Text: "RX", Font: miniFont, TextColor: receiveText},
							declarative.HSpacer{},
							declarative.CheckBox{AssignTo: &hexReceive, Text: "HEX 显示"},
							declarative.CheckBox{AssignTo: &pauseReceive, Text: "暂停显示"},
							declarative.PushButton{AssignTo: &clearReceiveButton, Text: "清空", MinSize: declarative.Size{Width: 62}, OnClicked: func() { _ = receiveEdit.SetText("") }},
						}},
						declarative.HSeparator{},
						declarative.TextEdit{AssignTo: &receiveEdit, ReadOnly: true, VScroll: true, HScroll: true, StretchFactor: 1, Background: editBrush, Font: monoFont, TextColor: textColor},
					}},
					declarative.Composite{Border: true, Background: cardBrush, StretchFactor: 1, Layout: declarative.VBox{Spacing: 0}, Children: []declarative.Widget{
						declarative.Composite{Background: cardBrush, MinSize: declarative.Size{Height: 42}, MaxSize: declarative.Size{Height: 42}, Layout: declarative.HBox{Margins: declarative.Margins{Left: 12, Top: 7, Right: 10, Bottom: 7}, Spacing: 10}, Children: []declarative.Widget{
							declarative.Label{Text: "↑", Font: sectionFont, TextColor: accentText},
							declarative.Label{Text: "发送编辑器", Font: sectionFont, TextColor: textColor},
							declarative.Label{Text: "TX", Font: miniFont, TextColor: accentText},
							declarative.HSpacer{},
							declarative.CheckBox{AssignTo: &hexSend, Text: "HEX 发送"},
						}},
						declarative.HSeparator{},
						declarative.TextEdit{AssignTo: &sendEdit, VScroll: true, HScroll: true, Text: "YUCOM serial test", StretchFactor: 1, Background: editBrush, Font: monoFont, TextColor: textColor},
						declarative.Composite{Background: cardBrush, Layout: declarative.HBox{Margins: declarative.Margins{Left: 10, Top: 7, Right: 10, Bottom: 7}, Spacing: 8}, Children: []declarative.Widget{
							declarative.Label{Text: "结束符", Font: miniFont, TextColor: mutedText},
							declarative.ComboBox{AssignTo: &newlineCombo, Model: nativeNewlineOptions, CurrentIndex: 0, MinSize: declarative.Size{Width: 90}},
							declarative.Label{Text: "周期", Font: miniFont, TextColor: mutedText},
							declarative.ComboBox{AssignTo: &intervalCombo, Editable: true, Model: []string{"1000", "500", "200", "100", "50"}, CurrentIndex: 0, MinSize: declarative.Size{Width: 72}},
							declarative.Label{Text: "ms", Font: miniFont, TextColor: mutedText},
							declarative.CheckBox{AssignTo: &timedSend, Text: "定时发送", Enabled: false, OnCheckedChanged: func() { startTimedSend() }},
							declarative.HSpacer{},
							declarative.PushButton{AssignTo: &sendButton, Text: "发送", Font: buttonFont, Enabled: false, MinSize: declarative.Size{Width: 96}, OnClicked: func() {
								payload, err := prepareSendPayload()
								if err != nil {
									showError("发送失败", err)
									return
								}
								if _, err := app.send(payload); err != nil {
									showError("发送失败", err)
									return
								}
								updateState()
							}},
						}},
						declarative.Composite{Background: cardBrush, Layout: declarative.HBox{Margins: declarative.Margins{Left: 10, Top: 0, Right: 10, Bottom: 7}, Spacing: 8}, Children: []declarative.Widget{
							declarative.PushButton{AssignTo: &frameButton, Text: "发送标准测试帧", Enabled: false, OnClicked: func() {
								payload, err := serialcore.PreparePayload(sendRequest{Data: standardFrame(), Format: "text", Newline: "none"})
								if err != nil {
									showError("测试帧失败", err)
									return
								}
								if _, err := app.send(payload); err != nil {
									showError("测试帧发送失败", err)
									return
								}
								setStatus("标准测试帧已发送")
							}},
							declarative.PushButton{AssignTo: &loopbackButton, Text: "512 字节回环自检", Enabled: false, OnClicked: func() {
								if !app.status().Opened {
									showError("回环自检", fmt.Errorf("请先打开串口，并将发送端与接收端短接"))
									return
								}
								loopbackButton.SetEnabled(false)
								_ = resultLabel.SetText("回环测试进行中，请保持接线不变…")
								go func() {
									result, err := app.loopbackTest(512, 2500*time.Millisecond)
									mw.Synchronize(func() {
										loopbackButton.SetEnabled(true)
										if err != nil {
											_ = resultLabel.SetText("回环失败：" + err.Error())
											return
										}
										_ = resultLabel.SetText(fmt.Sprintf("%s（发送%d字节，收到%d字节，耗时%dms）", result.Message, result.SentBytes, result.ReceivedBytes, result.ElapsedMS))
									})
								}()
							}},
							declarative.HSpacer{},
							declarative.Label{AssignTo: &resultLabel, Text: "就绪：可发送测试帧或进行硬件回环自检", Font: miniFont, TextColor: mutedText},
						}},
					}},
				}},
			}},
		},
	}

	if err := ui.Create(); err != nil {
		walk.MsgBox(nil, "YUCOM 启动失败", err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}

	for _, widget := range []walk.Widget{
		portCombo, baudCombo, dataBitsCombo, stopBitsCombo, parityCombo, flowCombo,
		newlineCombo, intervalCombo,
		receiveEdit, sendEdit,
		openButton, refreshButton, sendButton, frameButton, loopbackButton,
		clearReceiveButton, clearCounterButton,
	} {
		ensureNativeControlBorder(widget)
	}

	// Subscribe before the message loop starts so the first received bytes are
	// displayed in the native window as soon as the port is opened.
	events := make(chan serialEvent, 128)
	app.mu.Lock()
	app.subs[events] = struct{}{}
	app.mu.Unlock()
	go func() {
		defer func() {
			app.mu.Lock()
			delete(app.subs, events)
			app.mu.Unlock()
		}()
		for {
			select {
			case event := <-events:
				mw.Synchronize(func() {
					switch event.Type {
					case "data":
						data, err := base64.StdEncoding.DecodeString(event.Data)
						if err == nil {
							appendReceive(data)
						}
					case "state":
						setStatus(event.Text)
					case "error":
						setStatus(event.Text)
						_ = resultLabel.SetText(event.Text)
					}
					updateState()
				})
			case <-done:
				return
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mw.Synchronize(updateState)
			case <-done:
				return
			}
		}
	}()

	refreshPorts()
	updateState()
	mw.Run()
	closeOnce.Do(func() {
		stopTimedSend()
		close(done)
		app.closePort()
	})
}
