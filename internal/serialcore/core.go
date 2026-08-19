package serialcore

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

var serialDevicePattern = regexp.MustCompile(`^/dev/(tty[A-Za-z0-9._-]+|serial/by-id/[^/]+)$`)
var virtualConsolePattern = regexp.MustCompile(`^/dev/tty[0-9]+$`)

type SerialConfig struct {
	Device   string `json:"device"`
	Baud     int    `json:"baud"`
	DataBits int    `json:"dataBits"`
	StopBits int    `json:"stopBits"`
	Parity   string `json:"parity"`
	Flow     string `json:"flow"`
}

type SendRequest struct {
	Data    string `json:"data"`
	Format  string `json:"format"`
	Newline string `json:"newline"`
}

type Capture struct {
	Expected []byte
	Received []byte
	Matched  bool
}

func SupportedBaud(baud int) bool {
	switch baud {
	case 300, 600, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600:
		return true
	default:
		return false
	}
}

func ValidateConfig(cfg SerialConfig) error {
	if !serialDevicePattern.MatchString(cfg.Device) {
		return errors.New("串口路径无效，只允许 /dev/tty... 或 /dev/serial/by-id/... 设备")
	}
	return ValidateConfigValues(cfg)
}

func ValidateConfigValues(cfg SerialConfig) error {
	if !SupportedBaud(cfg.Baud) {
		return fmt.Errorf("不支持的波特率：%d", cfg.Baud)
	}
	if cfg.DataBits < 5 || cfg.DataBits > 8 {
		return errors.New("数据位必须为5、6、7或8")
	}
	if cfg.StopBits != 1 && cfg.StopBits != 2 {
		return errors.New("停止位必须为1或2")
	}
	if cfg.Parity != "none" && cfg.Parity != "odd" && cfg.Parity != "even" {
		return errors.New("校验位必须为无、奇校验或偶校验")
	}
	if cfg.Flow != "none" && cfg.Flow != "software" && cfg.Flow != "hardware" {
		return errors.New("流控参数无效")
	}
	return nil
}

func ShouldSkipEnumeratedDevice(device string) bool {
	return device == "/dev/tty" || device == "/dev/ttyprintk" || virtualConsolePattern.MatchString(device)
}

func PreparePayload(req SendRequest) ([]byte, error) {
	var payload []byte
	var err error
	switch req.Format {
	case "text", "":
		payload = []byte(req.Data)
	case "hex":
		payload, err = DecodeHex(req.Data)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("发送格式只能是文本或HEX")
	}
	switch req.Newline {
	case "", "none":
	case "crlf":
		payload = append(payload, '\r', '\n')
	case "cr":
		payload = append(payload, '\r')
	case "lf":
		payload = append(payload, '\n')
	default:
		return nil, errors.New("结束符参数无效")
	}
	return payload, nil
}

func DecodeHex(input string) ([]byte, error) {
	tokens := strings.FieldsFunc(strings.TrimSpace(input), func(r rune) bool {
		switch r {
		case ' ', '\t', '\r', '\n', ',', '-':
			return true
		default:
			return false
		}
	})
	if len(tokens) == 0 {
		return nil, errors.New("HEX发送内容不能为空")
	}
	var cleaned strings.Builder
	for _, token := range tokens {
		if strings.HasPrefix(token, "0x") || strings.HasPrefix(token, "0X") {
			token = token[2:]
		}
		if token == "" || len(token)%2 != 0 {
			return nil, errors.New("每段HEX字符数量必须为非零偶数，例如：55 AA 01 02")
		}
		if _, err := hex.DecodeString(token); err != nil {
			return nil, errors.New("HEX内容无效，只能包含0-9、A-F和分隔符")
		}
		cleaned.WriteString(token)
	}
	payload, err := hex.DecodeString(cleaned.String())
	if err != nil {
		return nil, errors.New("HEX内容无效，只能包含0-9、A-F和分隔空格")
	}
	return payload, nil
}

func MakeLoopbackPayload(length int) []byte {
	if length <= 0 {
		return []byte{}
	}
	payload := make([]byte, length)
	header := []byte("YUCOM_LOOPBACK_")
	randomOffset := copy(payload, header)
	randomLength := 8
	if randomOffset+randomLength > len(payload) {
		randomLength = len(payload) - randomOffset
	}
	if randomLength > 0 {
		if _, err := rand.Read(payload[randomOffset : randomOffset+randomLength]); err != nil {
			now := time.Now().UnixNano()
			for i := 0; i < randomLength; i++ {
				payload[randomOffset+i] = byte(now >> (i * 8))
			}
		}
	}
	for i := randomOffset + randomLength; i < len(payload); i++ {
		payload[i] = byte(i)
	}
	return payload
}

func (c *Capture) Start(expected []byte) {
	c.Expected = append(c.Expected[:0], expected...)
	c.Received = c.Received[:0]
	c.Matched = false
}

func (c *Capture) Feed(chunk []byte) bool {
	if len(c.Expected) == 0 || c.Matched {
		return false
	}
	c.Received = append(c.Received, chunk...)
	maxCapture := len(c.Expected)*4 + 4096
	if len(c.Received) > maxCapture {
		c.Received = append(c.Received[:0], c.Received[len(c.Received)-maxCapture:]...)
	}
	if bytes.Contains(c.Received, c.Expected) {
		c.Matched = true
		return true
	}
	return false
}

func (c *Capture) Reset() {
	c.Expected = nil
	c.Received = nil
	c.Matched = false
}

func WriteAll(writer io.Writer, payload []byte) (int, error) {
	total := 0
	for total < len(payload) {
		n, err := writer.Write(payload[total:])
		if n < 0 || n > len(payload)-total {
			return total, errors.New("串口驱动返回了无效的写入长度")
		}
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
