package serialcore

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestDecodeHexFormats(t *testing.T) {
	want := []byte{0x55, 0xAA, 0x01, 0x02}
	for _, input := range []string{"55 AA 01 02", "0x55,0xAA,0x01,0x02", "55-AA-01-02", "55aa0102"} {
		got, err := DecodeHex(input)
		if err != nil {
			t.Fatalf("DecodeHex(%q): %v", input, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("DecodeHex(%q) = %x, want %x", input, got, want)
		}
	}
}

func TestDecodeHexRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"", "0", "GG", "55 0x"} {
		if _, err := DecodeHex(input); err == nil {
			t.Fatalf("DecodeHex(%q) unexpectedly succeeded", input)
		}
	}
}

func TestPreparePayloadEndings(t *testing.T) {
	tests := []struct {
		name    string
		request SendRequest
		want    []byte
	}{
		{"text", SendRequest{Data: "TEST", Format: "text", Newline: "none"}, []byte("TEST")},
		{"crlf", SendRequest{Data: "TEST", Format: "text", Newline: "crlf"}, []byte("TEST\r\n")},
		{"hex lf", SendRequest{Data: "55 AA", Format: "hex", Newline: "lf"}, []byte{0x55, 0xAA, '\n'}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := PreparePayload(test.request)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, test.want) {
				t.Fatalf("got %x, want %x", got, test.want)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	valid := SerialConfig{Device: "/dev/ttyUSB0", Baud: 115200, DataBits: 8, StopBits: 1, Parity: "none", Flow: "none"}
	if err := ValidateConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	byID := valid
	byID.Device = "/dev/serial/by-id/usb-device@factory_1-if00-port0"
	if err := ValidateConfig(byID); err != nil {
		t.Fatalf("safe by-id path rejected: %v", err)
	}
	invalid := []SerialConfig{
		{Device: "/dev/../etc/passwd", Baud: 115200, DataBits: 8, StopBits: 1, Parity: "none", Flow: "none"},
		{Device: "/dev/serial/by-id/../passwd", Baud: 115200, DataBits: 8, StopBits: 1, Parity: "none", Flow: "none"},
		{Device: "/dev/ttyUSB0", Baud: 12345, DataBits: 8, StopBits: 1, Parity: "none", Flow: "none"},
		{Device: "/dev/ttyUSB0", Baud: 9600, DataBits: 9, StopBits: 1, Parity: "none", Flow: "none"},
		{Device: "/dev/ttyUSB0", Baud: 9600, DataBits: 8, StopBits: 3, Parity: "none", Flow: "none"},
		{Device: "/dev/ttyUSB0", Baud: 9600, DataBits: 8, StopBits: 1, Parity: "mark", Flow: "none"},
		{Device: "/dev/ttyUSB0", Baud: 9600, DataBits: 8, StopBits: 1, Parity: "none", Flow: "invalid"},
	}
	for i, cfg := range invalid {
		if err := ValidateConfig(cfg); err == nil {
			t.Fatalf("invalid config %d was accepted: %+v", i, cfg)
		}
	}
}

func TestCaptureMatchesAcrossChunks(t *testing.T) {
	var capture Capture
	capture.Start([]byte("YUCOM-EXPECTED-FRAME"))
	if capture.Feed([]byte("noise-YUCOM-EXPEC")) {
		t.Fatal("matched before complete frame")
	}
	if !capture.Feed([]byte("TED-FRAME-tail")) {
		t.Fatal("failed to match a frame split across reads")
	}
}

func TestCaptureReset(t *testing.T) {
	var capture Capture
	capture.Start([]byte("OK"))
	capture.Feed([]byte("OK"))
	capture.Reset()
	if capture.Matched || len(capture.Expected) != 0 || len(capture.Received) != 0 {
		t.Fatal("capture was not reset")
	}
}

func TestLoopbackPayload(t *testing.T) {
	a := MakeLoopbackPayload(512)
	b := MakeLoopbackPayload(512)
	if len(a) != 512 || len(b) != 512 {
		t.Fatal("unexpected payload length")
	}
	if !bytes.HasPrefix(a, []byte("YUCOM_LOOPBACK_")) {
		t.Fatal("payload marker missing")
	}
	if bytes.Equal(a, b) {
		t.Fatal("payload should include a random marker")
	}
}

func TestLoopbackPayloadHandlesShortLengths(t *testing.T) {
	for _, length := range []int{-1, 0, 1, 8, 14, 15, 16, 32} {
		payload := MakeLoopbackPayload(length)
		wantLength := length
		if wantLength < 0 {
			wantLength = 0
		}
		if len(payload) != wantLength {
			t.Fatalf("MakeLoopbackPayload(%d) returned %d bytes", length, len(payload))
		}
		markerLength := len("YUCOM_LOOPBACK_")
		if markerLength > wantLength {
			markerLength = wantLength
		}
		if !bytes.Equal(payload[:markerLength], []byte("YUCOM_LOOPBACK_")[:markerLength]) {
			t.Fatalf("MakeLoopbackPayload(%d) has an invalid marker prefix", length)
		}
	}
}

func TestSkipVirtualConsoles(t *testing.T) {
	for _, device := range []string{"/dev/tty", "/dev/tty0", "/dev/tty63", "/dev/ttyprintk"} {
		if !ShouldSkipEnumeratedDevice(device) {
			t.Fatalf("expected %s to be skipped", device)
		}
	}
	for _, device := range []string{"/dev/ttyS0", "/dev/ttyUSB0", "/dev/ttyGS0"} {
		if ShouldSkipEnumeratedDevice(device) {
			t.Fatalf("expected %s to be listed", device)
		}
	}
}

type partialWriter struct {
	max int
	buf bytes.Buffer
}

func (w *partialWriter) Write(payload []byte) (int, error) {
	if len(payload) > w.max {
		payload = payload[:w.max]
	}
	return w.buf.Write(payload)
}

func TestWriteAllHandlesPartialWrites(t *testing.T) {
	writer := &partialWriter{max: 3}
	payload := []byte("0123456789ABCDEF")
	n, err := WriteAll(writer, payload)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(payload) || !bytes.Equal(writer.buf.Bytes(), payload) {
		t.Fatalf("wrote %d bytes: %q", n, writer.buf.Bytes())
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

func TestWriteAllRejectsNoProgress(t *testing.T) {
	if _, err := WriteAll(zeroWriter{}, []byte("data")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("expected io.ErrShortWrite, got %v", err)
	}
}
