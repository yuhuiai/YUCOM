//go:build ignore

// generate_windows_resource creates icon-only Windows COFF resource objects
// for Go builds. It intentionally uses only the standard library so release
// builds do not depend on third-party resource generators.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

const (
	rtIcon       = 3
	rtGroupIcon  = 14
	rtManifest   = 24
	languageENUS = 0x0409
)

const appManifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="1.2.0.0" processorArchitecture="*" name="YUCOM.SerialTool" type="win32"/>
  <description>YUCOM Serial Diagnostic Tool</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security>
  </trustInfo>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2, PerMonitor</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>
`

type target struct {
	machine        uint16
	relocationType uint16
}

func main() {
	pngPath := flag.String("png", "design/YUCOM-App-Icon.png", "256x256 PNG icon")
	amd64Path := flag.String("amd64", "cmd/yucom/rsrc_windows_amd64.syso", "amd64 output")
	arm64Path := flag.String("arm64", "cmd/yucom/rsrc_windows_arm64.syso", "arm64 output")
	flag.Parse()

	png, err := os.ReadFile(*pngPath)
	if err != nil {
		fatal(err)
	}
	if len(png) < 24 || !bytes.Equal(png[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		fatal(fmt.Errorf("%s is not a PNG image", *pngPath))
	}
	width := binary.BigEndian.Uint32(png[16:20])
	height := binary.BigEndian.Uint32(png[20:24])
	if width != 256 || height != 256 {
		fatal(fmt.Errorf("icon PNG must be 256x256, got %dx%d", width, height))
	}

	outputs := []struct {
		path   string
		target target
	}{
		{*amd64Path, target{machine: 0x8664, relocationType: 0x0003}}, // IMAGE_REL_AMD64_ADDR32NB
		{*arm64Path, target{machine: 0xaa64, relocationType: 0x0002}}, // IMAGE_REL_ARM64_ADDR32NB
	}
	for _, output := range outputs {
		if err := os.WriteFile(output.path, makeResourceObject(png, output.target), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("generated %s\n", output.path)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func makeResourceObject(iconPNG []byte, architecture target) []byte {
	const (
		fileHeaderSize      = 20
		sectionHeaderSize   = 40
		rawOffset           = fileHeaderSize + sectionHeaderSize
		rootOffset          = 0
		iconTypeOffset      = 40
		iconLangOffset      = 64
		groupTypeOffset     = 88
		groupLangOffset     = 112
		manifestTypeOffset  = 136
		manifestLangOffset  = 160
		iconEntryOffset     = 184
		groupEntryOffset    = 200
		manifestEntryOffset = 216
		dataOffset          = 232
	)

	groupIcon := new(bytes.Buffer)
	write(groupIcon, uint16(0))
	write(groupIcon, uint16(1))
	write(groupIcon, uint16(1))
	groupIcon.Write([]byte{0, 0, 0, 0}) // 256x256, true color, reserved
	write(groupIcon, uint16(1))         // planes
	write(groupIcon, uint16(32))        // bit depth
	write(groupIcon, uint32(len(iconPNG)))
	write(groupIcon, uint16(1)) // RT_ICON resource id

	section := new(bytes.Buffer)
	writeDirectory(section, 0, 3)
	writeDirectoryEntry(section, rtIcon, iconTypeOffset, true)
	writeDirectoryEntry(section, rtGroupIcon, groupTypeOffset, true)
	writeDirectoryEntry(section, rtManifest, manifestTypeOffset, true)

	writeDirectory(section, 0, 1)
	writeDirectoryEntry(section, 1, iconLangOffset, true)
	writeDirectory(section, 0, 1)
	writeDirectoryEntry(section, languageENUS, iconEntryOffset, false)

	writeDirectory(section, 0, 1)
	writeDirectoryEntry(section, 1, groupLangOffset, true)
	writeDirectory(section, 0, 1)
	writeDirectoryEntry(section, languageENUS, groupEntryOffset, false)
	writeDirectory(section, 0, 1)
	writeDirectoryEntry(section, 1, manifestLangOffset, true)
	writeDirectory(section, 0, 1)
	writeDirectoryEntry(section, languageENUS, manifestEntryOffset, false)

	iconDataOffset := dataOffset
	groupDataOffset := align4(iconDataOffset + len(iconPNG))
	manifestDataOffset := align4(groupDataOffset + groupIcon.Len())
	writeDataEntry(section, iconDataOffset, len(iconPNG))
	writeDataEntry(section, groupDataOffset, groupIcon.Len())
	writeDataEntry(section, manifestDataOffset, len(appManifest))
	padTo(section, iconDataOffset)
	section.Write(iconPNG)
	padTo(section, groupDataOffset)
	section.Write(groupIcon.Bytes())
	padTo(section, manifestDataOffset)
	section.WriteString(appManifest)
	padTo(section, align4(section.Len()))
	sectionBytes := section.Bytes()

	relocationOffset := rawOffset + len(sectionBytes)
	symbolOffset := relocationOffset + 3*10
	object := new(bytes.Buffer)

	// COFF file header.
	write(object, architecture.machine)
	write(object, uint16(1))
	write(object, uint32(0))
	write(object, uint32(symbolOffset))
	write(object, uint32(1))
	write(object, uint16(0))
	write(object, uint16(0))

	// .rsrc section header.
	object.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0})
	write(object, uint32(0))
	write(object, uint32(0))
	write(object, uint32(len(sectionBytes)))
	write(object, uint32(rawOffset))
	write(object, uint32(relocationOffset))
	write(object, uint32(0))
	write(object, uint16(3))
	write(object, uint16(0))
	write(object, uint32(0x40000040)) // initialized, readable data

	object.Write(sectionBytes)
	writeRelocation(object, iconEntryOffset, architecture.relocationType)
	writeRelocation(object, groupEntryOffset, architecture.relocationType)
	writeRelocation(object, manifestEntryOffset, architecture.relocationType)

	// Static .rsrc section symbol plus an empty string table.
	object.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0})
	write(object, uint32(0))
	write(object, uint16(1))
	write(object, uint16(0))
	object.WriteByte(3) // IMAGE_SYM_CLASS_STATIC
	object.WriteByte(0)
	write(object, uint32(4))
	_ = rootOffset // documents that resource offsets are section-relative
	return object.Bytes()
}

func writeDirectory(buffer *bytes.Buffer, named, ids uint16) {
	write(buffer, uint32(0))
	write(buffer, uint32(0))
	write(buffer, uint16(0))
	write(buffer, uint16(0))
	write(buffer, named)
	write(buffer, ids)
}

func writeDirectoryEntry(buffer *bytes.Buffer, id, offset int, directory bool) {
	write(buffer, uint32(id))
	value := uint32(offset)
	if directory {
		value |= 0x80000000
	}
	write(buffer, value)
}

func writeDataEntry(buffer *bytes.Buffer, offset, size int) {
	write(buffer, uint32(offset))
	write(buffer, uint32(size))
	write(buffer, uint32(0))
	write(buffer, uint32(0))
}

func writeRelocation(buffer *bytes.Buffer, address int, relocationType uint16) {
	write(buffer, uint32(address))
	write(buffer, uint32(0))
	write(buffer, relocationType)
}

func align4(value int) int { return (value + 3) &^ 3 }

func padTo(buffer *bytes.Buffer, length int) {
	for buffer.Len() < length {
		buffer.WriteByte(0)
	}
}

func write(buffer *bytes.Buffer, value any) {
	if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
		panic(err)
	}
}
