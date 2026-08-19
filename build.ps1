$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -LiteralPath $projectRoot

gofmt -w .\cmd\yucom\main_common.go .\cmd\yucom\logo_common.go .\cmd\yucom\backend_linux.go .\cmd\yucom\webapp_linux.go .\cmd\yucom\native_linux.go .\cmd\yucom\main_windows.go .\cmd\yucom\native_windows.go .\internal\serialcore\core.go .\internal\serialcore\core_test.go .\scripts\generate_windows_resource.go

go run .\scripts\generate_windows_resource.go -png .\design\YUCOM-App-Icon.png
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

New-Item -ItemType Directory -Path '.\dist' -Force | Out-Null

$env:CGO_ENABLED = '0'
$env:GOOS = ''
$env:GOARCH = ''
go test -count=1 ./internal/...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$env:CGO_ENABLED = '0'
$env:GOOS = 'linux'
$env:GOARCH = 'arm64'

go vet ./cmd/yucom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go build -trimpath -ldflags '-s -w' -o '.\dist\YUCOM-Linux-arm64' ./cmd/yucom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$env:GOARCH = 'amd64'
go vet ./cmd/yucom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go build -trimpath -ldflags '-s -w' -o '.\dist\YUCOM-Linux-amd64' ./cmd/yucom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
go vet ./cmd/yucom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go build -trimpath -ldflags '-s -w -H=windowsgui' -o '.\dist\YUCOM-Windows-amd64.exe' ./cmd/yucom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$env:GOARCH = 'arm64'
go vet ./cmd/yucom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go build -trimpath -ldflags '-s -w -H=windowsgui' -o '.\dist\YUCOM-Windows-arm64.exe' ./cmd/yucom
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host 'YUCOM Linux arm64/amd64 and Windows amd64/arm64 builds completed.'
