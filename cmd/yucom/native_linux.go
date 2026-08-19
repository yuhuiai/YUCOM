//go:build linux && nativegui

package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"yucom/internal/serialcore"
)

var linuxNativeBaudOptions = []string{"921600", "460800", "230400", "115200", "57600", "38400", "19200", "9600", "4800", "2400", "1200", "600", "300"}
var linuxNativeDataBitsOptions = []string{"8", "7", "6", "5"}
var linuxNativeStopBitsOptions = []string{"1", "2"}
var linuxNativeParityOptions = []string{"无校验", "奇校验", "偶校验"}
var linuxNativeFlowOptions = []string{"无流控", "软件流控", "硬件流控"}
var linuxNativeNewlineOptions = []string{"无结束符", "CRLF", "CR", "LF"}

func linuxNativeAdd(box *gtk.Box, widget gtk.IWidget, expand bool) {
	box.PackStart(widget, expand, true, 0)
}

func linuxNativeLabel(text string) *gtk.Label {
	label, err := gtk.LabelNew(text)
	if err != nil {
		panic(err)
	}
	label.SetHAlign(gtk.ALIGN_START)
	return label
}

func linuxNativeCombo(values []string, active int) *gtk.ComboBoxText {
	combo, err := gtk.ComboBoxTextNew()
	if err != nil {
		panic(err)
	}
	for _, value := range values {
		combo.AppendText(value)
	}
	combo.SetActive(active)
	return combo
}

func linuxNativeText(buffer *gtk.TextBuffer) string {
	text, err := buffer.GetText(buffer.GetStartIter(), buffer.GetEndIter(), true)
	if err != nil {
		return ""
	}
	return text
}

func linuxNativeShowError(window *gtk.Window, title string, err error) {
	if err == nil {
		return
	}
	dialog := gtk.MessageDialogNew(window, gtk.DIALOG_MODAL, gtk.MESSAGE_ERROR, gtk.BUTTONS_OK, "%s", err.Error())
	dialog.SetTitle(title)
	dialog.Run()
	dialog.Destroy()
}

func linuxNativeApplyTheme() {
	css, err := gtk.CssProviderNew()
	if err != nil {
		return
	}
	if err := css.LoadFromData(`
* {
  font-family: "Noto Sans CJK SC", "Microsoft YaHei", sans-serif;
  font-size: 9.5pt;
}
window {
  background-color: #f3f6fb;
}
#topbar {
  background-color: #ffffff;
  border-bottom: 1px solid #e2e8f0;
  padding: 9px 14px;
}
#brand-title {
  color: #0f172a;
  font-size: 16pt;
  font-weight: 700;
}
#brand-subtitle {
  color: #64748b;
  font-size: 8pt;
}
#connection-state {
  color: #9a3412;
  background-color: #fff7ed;
  border: 1px solid #fed7aa;
  border-radius: 16px;
  padding: 6px 12px;
}
#connection-state.online {
  color: #166534;
  background-color: #dcfce7;
  border-color: #bbf7d0;
}
frame {
  border: 1px solid #d8e2ee;
  border-radius: 14px;
  background-color: #ffffff;
  padding: 8px;
}
frame > label {
  color: #172033;
  font-weight: 700;
  padding: 0 5px;
}
#receive-frame > label {
  color: #15803d;
}
#send-frame > label {
  color: #1d4ed8;
}
button {
  color: #334155;
  background: #ffffff;
  border: 1px solid #cbd5e1;
  border-radius: 9px;
  padding: 6px 11px;
}
button:hover {
  background-color: #f8fafc;
  border-color: #9eb1c8;
}
#primary-button, #send-button {
  color: #ffffff;
  background-color: #2563eb;
  border-color: #2563eb;
  font-weight: 700;
}
#primary-button:hover, #send-button:hover {
  background: #1d4ed8;
}
#primary-button.disconnect {
  background-color: #dc2626;
  border-color: #b91c1c;
}
entry, combobox, textview, textview.view {
  color: #0f172a;
  background-color: #f8fafc;
  border-color: #cbd5e1;
  border-radius: 8px;
}
textview, textview.view {
  color: #0f2748;
  font-family: "DejaVu Sans Mono", monospace;
  font-size: 10pt;
}
#receive-view, #receive-view text {
  background-color: #f8fafc;
}
#status-card {
  color: #2563eb;
  background-color: #eff6ff;
  border: 1px solid #bfdbfe;
  border-radius: 9px;
  padding: 8px 10px;
}
checkbutton:checked {
  color: #2563eb;
}
`); err != nil {
		return
	}
	screen, err := gdk.ScreenGetDefault()
	if err != nil || screen == nil {
		return
	}
	gtk.AddProviderForScreen(screen, css, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
}

func runApp() {
	// GTK requires all widget operations and the main loop to stay on one OS
	// thread. Serial reads remain on background goroutines and post UI work via
	// glib.IdleAdd.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	gtk.Init(nil)
	linuxNativeApplyTheme()
	app := newSerialApp()

	window, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		panic(err)
	}
	window.SetTitle("YUCOM 通用串口测试工具")
	window.SetDefaultSize(1180, 780)
	window.SetBorderWidth(0)

	outer, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	if err != nil {
		panic(err)
	}
	window.Add(outer)

	logoRGBA := newYUCOMLogoImage(40)
	logoPixbuf, logoErr := gdk.PixbufNewFromData(logoRGBA.Pix, gdk.COLORSPACE_RGB, true, 8, 40, 40, logoRGBA.Stride)
	if logoErr == nil {
		window.SetIcon(logoPixbuf)
	}
	header, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 10)
	header.SetName("topbar")
	if logoErr == nil {
		logoImage, imageErr := gtk.ImageNewFromPixbuf(logoPixbuf)
		if imageErr == nil {
			header.PackStart(logoImage, false, false, 0)
		}
	}
	brandBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	brandTitle := linuxNativeLabel("YUCOM")
	brandTitle.SetName("brand-title")
	brandSubtitle := linuxNativeLabel("通用串口诊断与回环测试")
	brandSubtitle.SetName("brand-subtitle")
	brandBox.PackStart(brandTitle, false, false, 0)
	brandBox.PackStart(brandSubtitle, false, false, 0)
	header.PackStart(brandBox, false, false, 0)
	headerSpacer := linuxNativeLabel("")
	header.PackStart(headerSpacer, true, true, 0)
	connectionLabel := linuxNativeLabel("●  未连接")
	connectionLabel.SetName("connection-state")
	connectionStyle, _ := connectionLabel.GetStyleContext()
	header.PackStart(connectionLabel, false, false, 0)
	outer.PackStart(header, false, true, 0)

	root, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 12)
	root.SetBorderWidth(12)
	outer.PackStart(root, true, true, 0)

	portCombo := linuxNativeCombo(nil, -1)
	portCombo.SetSizeRequest(170, -1)
	baudCombo := linuxNativeCombo(linuxNativeBaudOptions, 3)
	dataBitsCombo := linuxNativeCombo(linuxNativeDataBitsOptions, 0)
	stopBitsCombo := linuxNativeCombo(linuxNativeStopBitsOptions, 0)
	parityCombo := linuxNativeCombo(linuxNativeParityOptions, 0)
	flowCombo := linuxNativeCombo(linuxNativeFlowOptions, 0)
	newlineCombo := linuxNativeCombo(linuxNativeNewlineOptions, 0)
	newlineCombo.SetSizeRequest(95, -1)
	intervalEntry, err := gtk.EntryNew()
	if err != nil {
		panic(err)
	}
	intervalEntry.SetText("1000")
	intervalEntry.SetSizeRequest(80, -1)

	statusLabel := linuxNativeLabel("正在初始化…")
	statusLabel.SetName("status-card")
	modemLabel := linuxNativeLabel("CTS=false  DSR=false  DCD=false  RI=false")
	counterLabel := linuxNativeLabel("发送：0 字节    接收：0 字节")
	resultLabel := linuxNativeLabel("回环测试结果将在这里显示")
	resultLabel.SetLineWrap(true)

	hexReceive, err := gtk.CheckButtonNewWithLabel("HEX显示")
	if err != nil {
		panic(err)
	}
	hexSend, err := gtk.CheckButtonNewWithLabel("HEX发送")
	if err != nil {
		panic(err)
	}
	timedSend, err := gtk.CheckButtonNewWithLabel("定时发送")
	if err != nil {
		panic(err)
	}
	pauseReceive, err := gtk.CheckButtonNewWithLabel("暂停显示")
	if err != nil {
		panic(err)
	}

	openButton, err := gtk.ButtonNewWithLabel("打开串口")
	if err != nil {
		panic(err)
	}
	openButton.SetName("primary-button")
	openButtonStyle, _ := openButton.GetStyleContext()
	refreshButton, err := gtk.ButtonNewWithLabel("刷新")
	if err != nil {
		panic(err)
	}
	sendButton, err := gtk.ButtonNewWithLabel("单次发送")
	if err != nil {
		panic(err)
	}
	sendButton.SetName("send-button")
	frameButton, err := gtk.ButtonNewWithLabel("发送标准测试帧")
	if err != nil {
		panic(err)
	}
	loopbackButton, err := gtk.ButtonNewWithLabel("512字节回环自检")
	if err != nil {
		panic(err)
	}
	clearButton, err := gtk.ButtonNewWithLabel("清空接收区")
	if err != nil {
		panic(err)
	}
	clearCounterButton, err := gtk.ButtonNewWithLabel("清空计数")
	if err != nil {
		panic(err)
	}

	parameterFrame, err := gtk.FrameNew("连接设置")
	if err != nil {
		panic(err)
	}
	parameterBox, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
	if err != nil {
		panic(err)
	}
	parameterBox.SetBorderWidth(8)
	parameterFrame.Add(parameterBox)
	deviceLabel := linuxNativeLabel("串口设备")
	parameterBox.PackStart(deviceLabel, false, true, 0)
	deviceRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	linuxNativeAdd(deviceRow, portCombo, true)
	linuxNativeAdd(deviceRow, refreshButton, false)
	parameterBox.PackStart(deviceRow, false, true, 0)
	parameterBox.PackStart(openButton, false, true, 0)
	parameterGrid, _ := gtk.GridNew()
	parameterGrid.SetRowSpacing(6)
	parameterGrid.SetColumnSpacing(8)
	parameterGrid.Attach(linuxNativeLabel("波特率"), 0, 0, 1, 1)
	parameterGrid.Attach(baudCombo, 1, 0, 1, 1)
	parameterGrid.Attach(linuxNativeLabel("数据 / 停止"), 0, 1, 1, 1)
	dataStopRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	linuxNativeAdd(dataStopRow, dataBitsCombo, true)
	linuxNativeAdd(dataStopRow, stopBitsCombo, true)
	parameterGrid.Attach(dataStopRow, 1, 1, 1, 1)
	parameterGrid.Attach(linuxNativeLabel("校验 / 流控"), 0, 2, 1, 1)
	parityFlowRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 6)
	linuxNativeAdd(parityFlowRow, parityCombo, true)
	linuxNativeAdd(parityFlowRow, flowCombo, true)
	parameterGrid.Attach(parityFlowRow, 1, 2, 1, 1)
	parameterBox.PackStart(parameterGrid, false, true, 0)

	modemFrame, _ := gtk.FrameNew("线路信号")
	modemBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 4)
	modemBox.SetBorderWidth(8)
	modemBox.PackStart(modemLabel, false, true, 0)
	modemFrame.Add(modemBox)

	countFrame, _ := gtk.FrameNew("流量统计")
	countBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 5)
	countBox.SetBorderWidth(8)
	countBox.PackStart(counterLabel, false, true, 0)
	countBox.PackStart(clearCounterButton, false, true, 0)
	countFrame.Add(countBox)

	receiveView, err := gtk.TextViewNew()
	if err != nil {
		panic(err)
	}
	receiveView.SetEditable(false)
	receiveView.SetCursorVisible(false)
	receiveView.SetMonospace(true)
	receiveView.SetWrapMode(gtk.WRAP_NONE)
	receiveView.SetName("receive-view")
	receiveBuffer, _ := receiveView.GetBuffer()
	receiveScroll, _ := gtk.ScrolledWindowNew(nil, nil)
	receiveScroll.SetPolicy(gtk.POLICY_AUTOMATIC, gtk.POLICY_AUTOMATIC)
	receiveScroll.Add(receiveView)

	sendView, err := gtk.TextViewNew()
	if err != nil {
		panic(err)
	}
	sendView.SetMonospace(true)
	sendView.SetWrapMode(gtk.WRAP_NONE)
	sendBuffer, _ := sendView.GetBuffer()
	sendBuffer.SetText("YUCOM serial test")
	sendScroll, _ := gtk.ScrolledWindowNew(nil, nil)
	sendScroll.SetPolicy(gtk.POLICY_AUTOMATIC, gtk.POLICY_AUTOMATIC)
	sendScroll.Add(sendView)

	receiveFrame, _ := gtk.FrameNew("↓  接收监视器   RX")
	receiveFrame.SetName("receive-frame")
	receiveBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
	receiveBox.SetBorderWidth(8)
	receiveToolbar, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	linuxNativeAdd(receiveToolbar, hexReceive, false)
	linuxNativeAdd(receiveToolbar, pauseReceive, false)
	linuxNativeAdd(receiveToolbar, clearButton, false)
	receiveBox.PackStart(receiveToolbar, false, true, 0)
	receiveBox.PackStart(receiveScroll, true, true, 0)
	receiveFrame.Add(receiveBox)

	sendFrame, _ := gtk.FrameNew("↑  发送编辑器   TX")
	sendFrame.SetName("send-frame")
	sendBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 6)
	sendBox.SetBorderWidth(8)
	sendBox.PackStart(sendScroll, true, true, 0)
	sendToolbar, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	linuxNativeAdd(sendToolbar, hexSend, false)
	linuxNativeAdd(sendToolbar, linuxNativeLabel("结束符"), false)
	linuxNativeAdd(sendToolbar, newlineCombo, false)
	linuxNativeAdd(sendToolbar, sendButton, false)
	linuxNativeAdd(sendToolbar, frameButton, false)
	linuxNativeAdd(sendToolbar, loopbackButton, false)
	linuxNativeAdd(sendToolbar, linuxNativeLabel("周期(ms)"), false)
	linuxNativeAdd(sendToolbar, intervalEntry, false)
	linuxNativeAdd(sendToolbar, timedSend, false)
	sendBox.PackStart(sendToolbar, false, true, 0)
	sendBox.PackStart(resultLabel, false, true, 0)
	sendFrame.Add(sendBox)

	paned, err := gtk.PanedNew(gtk.ORIENTATION_VERTICAL)
	if err != nil {
		panic(err)
	}
	paned.Pack1(receiveFrame, true, false)
	paned.Pack2(sendFrame, true, false)
	paned.SetPosition(470)

	leftPanel, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	leftPanel.SetSizeRequest(310, -1)
	leftPanel.PackStart(parameterFrame, false, true, 0)
	leftPanel.PackStart(modemFrame, false, true, 0)
	leftPanel.PackStart(countFrame, false, true, 0)
	leftPanel.PackStart(statusLabel, false, true, 0)
	rightPanel, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	rightPanel.PackStart(paned, true, true, 0)
	root.PackStart(leftPanel, false, true, 0)
	root.PackStart(rightPanel, true, true, 0)

	var timedMu sync.Mutex
	var timedStop chan struct{}
	stopTimed := func() {
		timedMu.Lock()
		stop := timedStop
		timedStop = nil
		timedMu.Unlock()
		if stop != nil {
			close(stop)
		}
		if timedSend.GetActive() {
			timedSend.SetActive(false)
		}
	}

	newlineValue := func() string {
		switch newlineCombo.GetActive() {
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
	preparePayload := func() ([]byte, error) {
		format := "text"
		if hexSend.GetActive() {
			format = "hex"
		}
		return serialcore.PreparePayload(sendRequest{Data: linuxNativeText(sendBuffer), Format: format, Newline: newlineValue()})
	}
	standardFrame := func() []byte {
		payload, _ := serialcore.PreparePayload(sendRequest{Data: fmt.Sprintf("YUCOM_TEST_%s_%s\r\n", time.Now().Format("20060102_150405.000"), portCombo.GetActiveText()), Format: "text", Newline: "none"})
		return payload
	}

	updateState := func() {
		status := app.status()
		if status.Opened {
			openButton.SetLabel("关闭串口")
			connectionLabel.SetText("●  已连接 · " + status.Config.Device)
			connectionStyle.AddClass("online")
			openButtonStyle.AddClass("disconnect")
			statusLabel.SetText("已打开：" + status.Config.Device)
		} else {
			openButton.SetLabel("打开串口")
			connectionLabel.SetText("●  未连接")
			connectionStyle.RemoveClass("online")
			openButtonStyle.RemoveClass("disconnect")
			statusLabel.SetText("未打开串口")
		}
		portCombo.SetSensitive(!status.Opened)
		baudCombo.SetSensitive(!status.Opened)
		dataBitsCombo.SetSensitive(!status.Opened)
		stopBitsCombo.SetSensitive(!status.Opened)
		parityCombo.SetSensitive(!status.Opened)
		flowCombo.SetSensitive(!status.Opened)
		refreshButton.SetSensitive(!status.Opened)
		sendButton.SetSensitive(status.Opened)
		frameButton.SetSensitive(status.Opened)
		loopbackButton.SetSensitive(status.Opened)
		timedSend.SetSensitive(status.Opened)
		counterLabel.SetText(fmt.Sprintf("发送：%d 字节    接收：%d 字节", status.TXCount, status.RXCount))
		modemLabel.SetText(fmt.Sprintf("CTS=%t  DSR=%t  DCD=%t  RI=%t", status.Modem["CTS"], status.Modem["DSR"], status.Modem["DCD"], status.Modem["RI"]))
	}

	refreshPorts := func() {
		portCombo.RemoveAll()
		ports := enumeratePorts()
		for _, port := range ports {
			portCombo.AppendText(port.Device)
		}
		if len(ports) > 0 {
			portCombo.SetActive(0)
			statusLabel.SetText(fmt.Sprintf("发现 %d 个串口", len(ports)))
		} else {
			statusLabel.SetText("没有发现可用串口")
		}
	}

	var uiClosed bool
	done := make(chan struct{})
	events := make(chan serialEvent, 128)
	app.mu.Lock()
	app.subs[events] = struct{}{}
	app.mu.Unlock()
	window.Connect("destroy", func() {
		if uiClosed {
			return
		}
		uiClosed = true
		stopTimed()
		close(done)
		app.closePort()
		app.mu.Lock()
		delete(app.subs, events)
		app.mu.Unlock()
		gtk.MainQuit()
	})

	refreshButton.Connect("clicked", refreshPorts)
	openButton.Connect("clicked", func() {
		if app.status().Opened {
			stopTimed()
			app.closePort()
			updateState()
			return
		}
		baud, err := strconv.Atoi(baudCombo.GetActiveText())
		if err != nil {
			linuxNativeShowError(window, "打开串口失败", fmt.Errorf("波特率无效"))
			return
		}
		dataBits, _ := strconv.Atoi(dataBitsCombo.GetActiveText())
		stopBits, _ := strconv.Atoi(stopBitsCombo.GetActiveText())
		cfg := serialConfig{
			Device:   portCombo.GetActiveText(),
			Baud:     baud,
			DataBits: dataBits,
			StopBits: stopBits,
			Parity:   map[string]string{"无校验": "none", "奇校验": "odd", "偶校验": "even"}[parityCombo.GetActiveText()],
			Flow:     map[string]string{"无流控": "none", "软件流控": "software", "硬件流控": "hardware"}[flowCombo.GetActiveText()],
		}
		if err := validateConfig(cfg); err != nil {
			linuxNativeShowError(window, "打开串口失败", err)
			return
		}
		if err := app.openPort(cfg); err != nil {
			linuxNativeShowError(window, "打开串口失败", err)
			return
		}
		updateState()
	})

	sendButton.Connect("clicked", func() {
		payload, err := preparePayload()
		if err != nil {
			linuxNativeShowError(window, "发送失败", err)
			return
		}
		if _, err := app.send(payload); err != nil {
			linuxNativeShowError(window, "发送失败", err)
			return
		}
		updateState()
	})
	frameButton.Connect("clicked", func() {
		if _, err := app.send(standardFrame()); err != nil {
			linuxNativeShowError(window, "测试帧发送失败", err)
			return
		}
		statusLabel.SetText("标准测试帧已发送")
	})
	clearButton.Connect("clicked", func() { receiveBuffer.SetText("") })
	clearCounterButton.Connect("clicked", func() { app.resetCounters(); updateState() })

	timedSend.Connect("toggled", func() {
		if !timedSend.GetActive() {
			stopTimed()
			return
		}
		if !app.status().Opened {
			timedSend.SetActive(false)
			linuxNativeShowError(window, "定时发送", fmt.Errorf("请先打开串口"))
			return
		}
		intervalText, _ := intervalEntry.GetText()
		intervalMS, err := strconv.Atoi(strings.TrimSpace(intervalText))
		if err != nil || intervalMS < 50 || intervalMS > 60000 {
			timedSend.SetActive(false)
			linuxNativeShowError(window, "定时发送", fmt.Errorf("发送周期必须为50～60000毫秒"))
			return
		}
		payload, err := preparePayload()
		if err != nil {
			timedSend.SetActive(false)
			linuxNativeShowError(window, "定时发送", err)
			return
		}
		timedMu.Lock()
		oldStop := timedStop
		timedStop = make(chan struct{})
		stop := timedStop
		timedMu.Unlock()
		if oldStop != nil {
			close(oldStop)
		}
		statusLabel.SetText(fmt.Sprintf("定时发送已启动：%d ms/次", intervalMS))
		go func(payload []byte, interval time.Duration, stop <-chan struct{}) {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if _, err := app.send(payload); err != nil {
						glib.IdleAdd(func() bool {
							stopTimed()
							resultLabel.SetText("定时发送已停止：" + err.Error())
							updateState()
							return false
						})
						return
					}
					glib.IdleAdd(func() bool { updateState(); return false })
				case <-stop:
					return
				case <-done:
					return
				}
			}
		}(append([]byte(nil), payload...), time.Duration(intervalMS)*time.Millisecond, stop)
	})

	loopbackButton.Connect("clicked", func() {
		if !app.status().Opened {
			linuxNativeShowError(window, "回环自检", fmt.Errorf("请先打开串口，并将发送端与接收端短接"))
			return
		}
		loopbackButton.SetSensitive(false)
		resultLabel.SetText("回环测试进行中，请保持接线不变…")
		go func() {
			result, err := app.loopbackTest(512, 2500*time.Millisecond)
			glib.IdleAdd(func() bool {
				loopbackButton.SetSensitive(true)
				if err != nil {
					resultLabel.SetText("回环失败：" + err.Error())
				} else {
					resultLabel.SetText(fmt.Sprintf("%s（发送%d字节，收到%d字节，耗时%dms）", result.Message, result.SentBytes, result.ReceivedBytes, result.ElapsedMS))
				}
				return false
			})
		}()
	})

	go func() {
		defer func() {
			app.mu.Lock()
			delete(app.subs, events)
			app.mu.Unlock()
		}()
		for {
			select {
			case event := <-events:
				ev := event
				glib.IdleAdd(func() bool {
					if uiClosed {
						return false
					}
					switch ev.Type {
					case "data":
						data, err := base64.StdEncoding.DecodeString(ev.Data)
						if err == nil {
							if pauseReceive.GetActive() {
								break
							}
							if hexReceive.GetActive() {
								receiveBuffer.InsertAtCursor(strings.ToUpper(hex.EncodeToString(data)) + " ")
							} else {
								receiveBuffer.InsertAtCursor(string(data))
							}
						}
					case "state":
						statusLabel.SetText(ev.Text)
					case "error":
						statusLabel.SetText(ev.Text)
						resultLabel.SetText(ev.Text)
					}
					updateState()
					return false
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
				glib.IdleAdd(func() bool {
					if uiClosed {
						return false
					}
					updateState()
					return false
				})
			case <-done:
				return
			}
		}
	}()

	refreshPorts()
	updateState()
	window.ShowAll()
	gtk.Main()
	runtime.KeepAlive(logoRGBA)
}
